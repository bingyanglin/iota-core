//go:build dockertests

package dockertestframework

import (
	"context"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (d *DockerTestFramework) ClaimRewardsForValidator(ctx context.Context, validatorWallet *mock.Wallet) {
	validatorAccountData := validatorWallet.BlockIssuer.AccountData
	outputData := &mock.OutputData{
		ID:           validatorAccountData.OutputID,
		Address:      validatorAccountData.Address,
		AddressIndex: validatorAccountData.AddressIndex,
		Output:       validatorAccountData.Output,
	}
	signedTx := validatorWallet.ClaimValidatorRewards("", outputData)

	validatorWallet.CreateAndSubmitBasicBlock(ctx, "claim_rewards_validator", mock.WithPayload(signedTx))
	d.AwaitTransactionPayloadAccepted(ctx, signedTx.Transaction.MustID())

	// update account data of validator
	validatorWallet.SetBlockIssuer(&mock.AccountData{
		ID:           validatorWallet.BlockIssuer.AccountData.ID,
		Address:      validatorWallet.BlockIssuer.AccountData.Address,
		AddressIndex: validatorWallet.BlockIssuer.AccountData.AddressIndex,
		OutputID:     iotago.OutputIDFromTransactionIDAndIndex(signedTx.Transaction.MustID(), 0),
		Output:       signedTx.Transaction.Outputs[0].(*iotago.AccountOutput),
	})
}

func (d *DockerTestFramework) ClaimRewardsForDelegator(ctx context.Context, wallet *mock.Wallet, delegationOutputData *mock.OutputData) iotago.OutputID {
	signedTx := wallet.ClaimDelegatorRewards("", delegationOutputData)

	wallet.CreateAndSubmitBasicBlock(ctx, "claim_rewards_delegator", mock.WithPayload(signedTx))
	d.AwaitTransactionPayloadAccepted(ctx, signedTx.Transaction.MustID())

	return iotago.OutputIDFromTransactionIDAndIndex(signedTx.Transaction.MustID(), 0)
}

func (d *DockerTestFramework) DelayedClaimingTransition(ctx context.Context, wallet *mock.Wallet, delegationOutputData *mock.OutputData) *mock.OutputData {
	signedTx := wallet.DelayedClaimingTransition("delayed_claim_tx", delegationOutputData)

	wallet.CreateAndSubmitBasicBlock(ctx, "delayed_claim", mock.WithPayload(signedTx))
	d.AwaitTransactionPayloadAccepted(ctx, signedTx.Transaction.MustID())

	return &mock.OutputData{
		ID:           iotago.OutputIDFromTransactionIDAndIndex(signedTx.Transaction.MustID(), 0),
		Address:      wallet.Address(),
		AddressIndex: 0,
		Output:       signedTx.Transaction.Outputs[0].(*iotago.DelegationOutput),
	}
}

// DelegateToValidator requests faucet funds and delegate the UTXO output to the validator.
func (d *DockerTestFramework) DelegateToValidator(fromWallet *mock.Wallet, accountAddress *iotago.AccountAddress) *mock.OutputData {
	// requesting faucet funds as delegation input
	ctx := context.TODO()
	fundsOutputData := d.RequestFaucetFunds(ctx, fromWallet, iotago.AddressEd25519)

	signedTx := fromWallet.CreateDelegationFromInput(
		"delegation_tx",
		fundsOutputData,
		mock.WithDelegatedValidatorAddress(accountAddress),
		mock.WithDelegationStartEpoch(GetDelegationStartEpoch(fromWallet.Client.LatestAPI(), fromWallet.GetNewBlockIssuanceResponse().LatestCommitment.Slot)),
	)

	fromWallet.CreateAndSubmitBasicBlock(ctx, "delegation", mock.WithPayload(signedTx))
	d.AwaitTransactionPayloadAccepted(ctx, signedTx.Transaction.MustID())

	delegationOutput, ok := signedTx.Transaction.Outputs[0].(*iotago.DelegationOutput)
	require.True(d.Testing, ok)

	return &mock.OutputData{
		ID:           iotago.OutputIDFromTransactionIDAndIndex(signedTx.Transaction.MustID(), 0),
		Address:      fromWallet.Address(),
		AddressIndex: 0,
		Output:       delegationOutput,
	}

}

func GetDelegationStartEpoch(api iotago.API, commitmentSlot iotago.SlotIndex) iotago.EpochIndex {
	pastBoundedSlot := commitmentSlot + api.ProtocolParameters().MaxCommittableAge()
	pastBoundedEpoch := api.TimeProvider().EpochFromSlot(pastBoundedSlot)
	pastBoundedEpochEnd := api.TimeProvider().EpochEnd(pastBoundedEpoch)
	registrationSlot := pastBoundedEpochEnd - api.ProtocolParameters().EpochNearingThreshold()

	if pastBoundedSlot <= registrationSlot {
		return pastBoundedEpoch + 1
	}

	return pastBoundedEpoch + 2
}

func GetDelegationEndEpoch(api iotago.API, slot, latestCommitmentSlot iotago.SlotIndex) iotago.EpochIndex {
	futureBoundedSlot := latestCommitmentSlot + api.ProtocolParameters().MinCommittableAge()
	futureBoundedEpoch := api.TimeProvider().EpochFromSlot(futureBoundedSlot)

	registrationSlot := api.TimeProvider().EpochEnd(api.TimeProvider().EpochFromSlot(slot)) - api.ProtocolParameters().EpochNearingThreshold()

	if futureBoundedSlot <= registrationSlot {
		return futureBoundedEpoch
	}

	return futureBoundedEpoch + 1
}
