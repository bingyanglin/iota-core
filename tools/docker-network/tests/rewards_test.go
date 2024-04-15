//go:build dockertests

package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	iotago "github.com/iotaledger/iota.go/v4"
)

// Test_ValidatorRewards tests the rewards for a validator.
// 1. Create 2 accounts with staking feature.
// 2. Issue candidacy payloads for the accounts and wait until the accounts is in the committee.
// 3. One of the account issues 3 validation blocks per slot, the other account issues 1 validation block per slot until claiming slot is reached.
// 4. Claim rewards and check if the mana increased as expected, the account that issued less validation blocks should have less mana.
func Test_ValidatorRewards(t *testing.T) {
	d := NewDockerTestFramework(t,
		WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 4),
			iotago.WithLivenessOptions(10, 10, 2, 4, 8),
			iotago.WithStakingOptions(3, 10, 10),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	err := d.Run()
	require.NoError(t, err)

	d.WaitUntilNetworkReady()

	ctx := context.Background()
	clt := d.wallet.DefaultClient()
	status := d.NodeStatus("V1")
	currentEpoch := clt.CommittedAPI().TimeProvider().EpochFromSlot(status.LatestAcceptedBlockSlot)
	slotsDuration := clt.CommittedAPI().ProtocolParameters().SlotDurationInSeconds()

	// Set end epoch so the staking feature can be removed as soon as possible.
	endEpoch := currentEpoch + clt.CommittedAPI().ProtocolParameters().StakingUnbondingPeriod() + 1
	// The earliest epoch in which we can remove the staking feature and claim rewards.
	claimingSlot := clt.CommittedAPI().TimeProvider().EpochStart(endEpoch + 1)

	// create accounts and continue issuing candidacy payload for account in the background
	account := d.CreateAccount(WithStakingFeature(100, 1, currentEpoch, endEpoch))
	initialMana := account.Output.StoredMana()
	issueCandidacyPayloadInBackground(d, account.ID, clt.CommittedAPI().TimeProvider().CurrentSlot(), claimingSlot,
		slotsDuration)

	lazyAccount := d.CreateAccount(WithStakingFeature(100, 1, currentEpoch, endEpoch))
	lazyInitialMana := lazyAccount.Output.StoredMana()
	issueCandidacyPayloadInBackground(d, lazyAccount.ID, clt.CommittedAPI().TimeProvider().CurrentSlot(), claimingSlot,
		slotsDuration)

	// make sure the account is in the committee, so it can issue validation blocks
	accountAddrBech32 := account.Address.Bech32(clt.CommittedAPI().ProtocolParameters().Bech32HRP())
	lazyAccountAddrBech32 := lazyAccount.Address.Bech32(clt.CommittedAPI().ProtocolParameters().Bech32HRP())
	d.AssertCommittee(currentEpoch+2, append(d.AccountsFromNodes(d.Nodes("V1", "V3", "V2", "V4")...), accountAddrBech32, lazyAccountAddrBech32))

	// issue validation blocks to have performance
	currentSlot := clt.CommittedAPI().TimeProvider().CurrentSlot()
	slotToWait := claimingSlot - currentSlot
	secToWait := time.Duration(slotToWait) * time.Duration(slotsDuration) * time.Second
	fmt.Println("Wait for ", secToWait, "until expected slot: ", claimingSlot)

	var wg sync.WaitGroup
	issueValidationBlockInBackground(&wg, d, account.ID, currentSlot, claimingSlot, 5)
	issueValidationBlockInBackground(&wg, d, lazyAccount.ID, currentSlot, claimingSlot, 1)

	wg.Wait()

	// claim rewards that put to the account output
	d.AwaitCommitment(claimingSlot)
	account = d.ClaimRewardsForValidator(ctx, account)
	lazyAccount = d.ClaimRewardsForValidator(ctx, lazyAccount)

	// check if the mana increased as expected
	outputFromAPI, err := clt.OutputByID(ctx, account.OutputID)
	require.NoError(t, err)
	require.Greater(t, outputFromAPI.StoredMana(), initialMana)
	require.Equal(t, account.Output.StoredMana(), outputFromAPI.StoredMana())

	lazyOutputFromAPI, err := clt.OutputByID(ctx, lazyAccount.OutputID)
	require.NoError(t, err)
	require.Greater(t, lazyOutputFromAPI.StoredMana(), lazyInitialMana)
	require.Equal(t, lazyAccount.Output.StoredMana(), lazyOutputFromAPI.StoredMana())

	// account that issued more validation blocks should have more mana
	require.Greater(t, account.Output.StoredMana(), lazyAccount.Output.StoredMana())
}

// Test_DelegatorRewards tests the rewards for a delegator.
// 1. Create an account and delegate funds to a validator.
// 2. Wait long enough so there's rewards can be claimed.
// 3. Claim rewards and check if the mana increased as expected.
func Test_DelegatorRewards(t *testing.T) {
	d := NewDockerTestFramework(t,
		WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 3),
			iotago.WithLivenessOptions(10, 10, 2, 4, 5),
			iotago.WithStakingOptions(3, 10, 10),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	err := d.Run()
	require.NoError(t, err)

	d.WaitUntilNetworkReady()

	ctx := context.Background()
	clt := d.wallet.DefaultClient()

	account := d.CreateAccount()

	// delegate funds to V2
	delegationOutputID, delegationOutput := d.DelegateToValidator(account.ID, d.Node("V2").AccountAddress(t))
	d.AwaitCommitment(delegationOutputID.CreationSlot())

	// check if V2 received the delegator stake
	v2Resp, err := clt.Validator(ctx, d.Node("V2").AccountAddress(t))
	require.NoError(t, err)
	require.Greater(t, v2Resp.PoolStake, v2Resp.ValidatorStake)

	// wait until next epoch so the rewards can be claimed
	expectedSlot := clt.CommittedAPI().TimeProvider().EpochStart(delegationOutput.StartEpoch + 2)
	slotToWait := expectedSlot - clt.CommittedAPI().TimeProvider().CurrentSlot()
	secToWait := time.Duration(slotToWait) * time.Duration(clt.CommittedAPI().ProtocolParameters().SlotDurationInSeconds()) * time.Second
	fmt.Println("Wait for ", secToWait, "until expected slot: ", expectedSlot)
	time.Sleep(secToWait)

	// claim rewards that put to an basic output
	rewardsOutputID := d.ClaimRewardsForDelegator(ctx, account, delegationOutputID)

	// check if the mana increased as expected
	outputFromAPI, err := clt.OutputByID(ctx, rewardsOutputID)
	require.NoError(t, err)

	rewardsOutput := d.wallet.Output(rewardsOutputID)
	require.Equal(t, rewardsOutput.Output.StoredMana(), outputFromAPI.StoredMana())
}

// Test_DelayedClaimingRewards tests the delayed claiming rewards for a delegator.
// 1. Create an account and delegate funds to a validator.
// 2. Delay claiming rewards for the delegation and check if the delegated stake is removed from the validator.
// 3. Claim rewards and check to destroy the delegation output.
func Test_DelayedClaimingRewards(t *testing.T) {
	d := NewDockerTestFramework(t,
		WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 4),
			iotago.WithLivenessOptions(10, 10, 2, 4, 8),
			iotago.WithStakingOptions(3, 10, 10),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	err := d.Run()
	require.NoError(t, err)

	d.WaitUntilNetworkReady()

	ctx := context.Background()
	clt := d.wallet.DefaultClient()

	account := d.CreateAccount()

	{
		// delegate funds to V2
		delegationOutputID, _ := d.DelegateToValidator(account.ID, d.Node("V2").AccountAddress(t))
		d.AwaitCommitment(delegationOutputID.CreationSlot())

		// check if V2 received the delegator stake
		v2Resp, err := clt.Validator(ctx, d.Node("V2").AccountAddress(t))
		require.NoError(t, err)
		require.Greater(t, v2Resp.PoolStake, v2Resp.ValidatorStake)

		// delay claiming rewards
		delegationOutputID1, delegationEndEpoch := d.DelayedClaimingTransition(ctx, account, delegationOutputID)
		d.AwaitCommitment(delegationOutputID1.CreationSlot())

		// the delegated stake should be removed from the validator, so the pool stake should equal to the validator stake
		v2Resp, err = clt.Validator(ctx, d.Node("V2").AccountAddress(t))
		require.NoError(t, err)
		require.Equal(t, v2Resp.PoolStake, v2Resp.ValidatorStake)

		// wait until next epoch to destroy the delegation
		expectedSlot := clt.CommittedAPI().TimeProvider().EpochStart(delegationEndEpoch)
		slotToWait := expectedSlot - clt.CommittedAPI().TimeProvider().CurrentSlot()
		secToWait := time.Duration(slotToWait) * time.Duration(clt.CommittedAPI().ProtocolParameters().SlotDurationInSeconds()) * time.Second
		fmt.Println("Wait for ", secToWait, "until expected slot: ", expectedSlot)
		time.Sleep(secToWait)
		d.ClaimRewardsForDelegator(ctx, account, delegationOutputID1)
	}

	{
		// delegate funds to V2
		delegationOutputID, _ := d.DelegateToValidator(account.ID, d.Node("V2").AccountAddress(t))

		// delay claiming rewards in the same slot of delegation
		delegationOutputID1, _ := d.DelayedClaimingTransition(ctx, account, delegationOutputID)
		d.AwaitCommitment(delegationOutputID1.CreationSlot())

		// the delegated stake should be 0, thus poolStake should be equal to validatorStake
		v2Resp, err := clt.Validator(ctx, d.Node("V2").AccountAddress(t))
		require.NoError(t, err)
		require.Equal(t, v2Resp.PoolStake, v2Resp.ValidatorStake)

		// wait until next epoch to destroy the delegation
		d.ClaimRewardsForDelegator(ctx, account, delegationOutputID1)
	}
}

func issueCandidacyPayloadInBackground(d *DockerTestFramework, accountID iotago.AccountID, startSlot, endSlot iotago.SlotIndex, slotDuration uint8) {
	go func() {
		fmt.Println("Issuing candidacy payloads for account", accountID, "in the background...")
		defer fmt.Println("Issuing candidacy payloads for account", accountID, "in the background......done")

		for i := startSlot; i < endSlot; i++ {
			d.IssueCandidacyPayloadFromAccount(accountID)
			time.Sleep(time.Duration(slotDuration) * time.Second)
		}
	}()
}

func issueValidationBlockInBackground(wg *sync.WaitGroup, d *DockerTestFramework, accountID iotago.AccountID, startSlot, endSlot iotago.SlotIndex, blocksPerSlot int) {
	wg.Add(1)

	go func() {
		defer wg.Done()
		fmt.Println("Issuing validation block for account", accountID, "in the background...")
		defer fmt.Println("Issuing validation block for account", accountID, "in the background......done")
		clt := d.wallet.DefaultClient()

		for i := startSlot; i < endSlot; i++ {
			// wait until the slot is reached
			for {
				if clt.CommittedAPI().TimeProvider().CurrentSlot() == i {
					break
				}
				time.Sleep(2 * time.Second)
			}

			for range blocksPerSlot {
				d.SubmitValidationBlock(accountID)
				time.Sleep(1 * time.Second)
			}
		}
	}()
}
