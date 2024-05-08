//go:build dockertests

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/iota-core/tools/docker-network/tests/dockertestframework"
	iotago "github.com/iotaledger/iota.go/v4"
)

func Test_SyncFromSnapshot(t *testing.T) {
	d := dockertestframework.NewDockerTestFramework(t,
		dockertestframework.WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(0, time.Now().Unix(), 10, 3),
			iotago.WithLivenessOptions(10, 10, 2, 4, 5),
			iotago.WithCongestionControlOptions(1, 1, 1, 400_000, 250_000, 50_000_000, 1000, 100),
			iotago.WithRewardsOptions(8, 10, 2, 384),
			iotago.WithTargetCommitteeSize(4),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8080")

	err := d.Run()
	require.NoError(t, err)

	d.WaitUntilNetworkReady()

	ctx := context.Background()
	delegatorWallet, accountData := d.CreateAccountFromFaucet()
	clt := delegatorWallet.Client

	// delegate funds to V2
	delegationOutputData := d.DelegateToValidator(delegatorWallet, d.Node("V2").AccountAddress(t))
	d.AwaitCommitment(delegationOutputData.ID.CreationSlot())

	// check if V2 received the delegator stake
	v2Resp, err := clt.Validator(ctx, d.Node("V2").AccountAddress(t))
	require.NoError(t, err)
	require.Greater(t, v2Resp.PoolStake, v2Resp.ValidatorStake)

	//nolint:forcetypeassert
	currentEpoch := clt.CommittedAPI().TimeProvider().EpochFromSlot(delegatorWallet.CurrentSlot())
	expectedEpoch := delegationOutputData.Output.(*iotago.DelegationOutput).StartEpoch + 2
	for range expectedEpoch - currentEpoch {
		d.AwaitEpochFinalized()
	}

	// claim rewards that put to an basic output
	rewardsOutputID := d.ClaimRewardsForDelegator(ctx, delegatorWallet, delegationOutputData)

	// check if the mana increased as expected
	node5Clt := d.Client("node5")
	outputFromAPI, err := node5Clt.OutputByID(ctx, rewardsOutputID)
	require.NoError(t, err)

	rewardsOutput := delegatorWallet.Output(rewardsOutputID)
	require.Equal(t, rewardsOutput.Output.StoredMana(), outputFromAPI.StoredMana())

	d.AwaitEpochFinalized()

	// create snapshot
	nodeClientV1 := d.Client("V1")
	managementClient, err := nodeClientV1.Management(getContextWithTimeout(5 * time.Second))
	require.NoError(t, err)

	response, err := managementClient.CreateSnapshot(getContextWithTimeout(5 * time.Second))
	require.NoError(t, err)

	// Deletes the database of node5 and restarts it with the just created snapshot.
	d.ResetNode("node5", response.FilePath)

	d.AwaitEpochFinalized()
	d.AwaitEpochFinalized()

	// check if the account and rewardsOutput are available
	node5Clt = d.Client("node5")
	_, err = node5Clt.Validator(ctx, accountData.Address)
	require.NoError(t, err)

	outputFromAPI, err = node5Clt.OutputByID(ctx, rewardsOutputID)
	require.NoError(t, err)
}
