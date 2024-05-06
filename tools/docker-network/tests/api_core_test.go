//go:build dockertests

package tests

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/ds/types"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	"github.com/iotaledger/iota-core/tools/docker-network/tests/dockertestframework"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

type coreAPIAssets map[iotago.SlotIndex]*coreAPISlotAssets

func (a coreAPIAssets) setupAssetsForSlot(slot iotago.SlotIndex) {
	_, ok := a[slot]
	if !ok {
		a[slot] = newAssetsPerSlot()
	}
}

func (a coreAPIAssets) assertCommitments(t *testing.T) {
	for _, asset := range a {
		asset.assertCommitments(t)
	}
}

func (a coreAPIAssets) assertBICs(t *testing.T) {
	for _, asset := range a {
		asset.assertBICs(t)
	}
}

func (a coreAPIAssets) forEachBlock(t *testing.T, f func(*testing.T, *iotago.Block)) {
	for _, asset := range a {
		for _, block := range asset.dataBlocks {
			f(t, block)
		}
		for _, block := range asset.valueBlocks {
			f(t, block)
		}
	}
}

func (a coreAPIAssets) forEachTransaction(t *testing.T, f func(*testing.T, *iotago.SignedTransaction, iotago.BlockID)) {
	for _, asset := range a {
		for i, tx := range asset.transactions {
			blockID := asset.valueBlocks[i].MustID()
			f(t, tx, blockID)
		}
	}
}

func (a coreAPIAssets) forEachReattachment(t *testing.T, f func(*testing.T, iotago.BlockID)) {
	for _, asset := range a {
		for _, reattachment := range asset.reattachments {
			f(t, reattachment)
		}
	}
}

func (a coreAPIAssets) forEachOutput(t *testing.T, f func(*testing.T, iotago.OutputID, iotago.Output)) {
	for _, asset := range a {
		for outID, out := range asset.basicOutputs {
			f(t, outID, out)
		}
		for outID, out := range asset.faucetOutputs {
			f(t, outID, out)
		}
		for outID, out := range asset.delegationOutputs {
			f(t, outID, out)
		}
	}
}

func (a coreAPIAssets) forEachSlot(t *testing.T, f func(*testing.T, iotago.SlotIndex, map[string]iotago.CommitmentID)) {
	for slot, slotAssets := range a {
		f(t, slot, slotAssets.commitmentPerNode)
	}
}

func (a coreAPIAssets) forEachCommitment(t *testing.T, f func(*testing.T, map[string]iotago.CommitmentID)) {
	for _, asset := range a {
		f(t, asset.commitmentPerNode)
	}
}

func (a coreAPIAssets) forEachAccountAddress(t *testing.T, f func(t *testing.T, accountAddress *iotago.AccountAddress, commitmentPerNode map[string]iotago.CommitmentID, bicPerNode map[string]iotago.BlockIssuanceCredits)) {
	for _, asset := range a {
		if asset.accountAddress == nil {
			// no account created in this slot
			continue
		}
		f(t, asset.accountAddress, asset.commitmentPerNode, asset.bicPerNode)
	}
}

func (a coreAPIAssets) assertUTXOOutputIDsInSlot(t *testing.T, slot iotago.SlotIndex, createdOutputs iotago.OutputIDs, spentOutputs iotago.OutputIDs) {
	created := make(map[iotago.OutputID]types.Empty)
	spent := make(map[iotago.OutputID]types.Empty)
	for _, outputID := range createdOutputs {
		created[outputID] = types.Void
	}

	for _, outputID := range spentOutputs {
		spent[outputID] = types.Void
	}

	for outID := range a[slot].basicOutputs {
		_, ok := created[outID]
		require.True(t, ok, "Output ID not found in created outputs: %s, for slot %d", outID, slot)
	}

	for outID := range a[slot].faucetOutputs {
		_, ok := spent[outID]
		require.True(t, ok, "Output ID not found in spent outputs: %s, for slot %d", outID, slot)
	}
}

func (a coreAPIAssets) assertUTXOOutputsInSlot(t *testing.T, slot iotago.SlotIndex, created []*api.OutputWithID, spent []*api.OutputWithID) {
	createdMap := make(map[iotago.OutputID]iotago.Output)
	spentMap := make(map[iotago.OutputID]iotago.Output)
	for _, output := range created {
		createdMap[output.OutputID] = output.Output
	}
	for _, output := range spent {
		spentMap[output.OutputID] = output.Output
	}

	for outID, out := range a[slot].basicOutputs {
		_, ok := createdMap[outID]
		require.True(t, ok, "Output ID not found in created outputs: %s, for slot %d", outID, slot)
		require.Equal(t, out, createdMap[outID], "Output not equal for ID: %s, for slot %d", outID, slot)
	}

	for outID, out := range a[slot].faucetOutputs {
		_, ok := spentMap[outID]
		require.True(t, ok, "Output ID not found in spent outputs: %s, for slot %d", outID, slot)
		require.Equal(t, out, spentMap[outID], "Output not equal for ID: %s, for slot %d", outID, slot)
	}
}

type coreAPISlotAssets struct {
	accountAddress    *iotago.AccountAddress
	dataBlocks        []*iotago.Block
	valueBlocks       []*iotago.Block
	transactions      []*iotago.SignedTransaction
	reattachments     []iotago.BlockID
	basicOutputs      map[iotago.OutputID]iotago.Output
	faucetOutputs     map[iotago.OutputID]iotago.Output
	delegationOutputs map[iotago.OutputID]iotago.Output

	commitmentPerNode map[string]iotago.CommitmentID
	bicPerNode        map[string]iotago.BlockIssuanceCredits
}

func (a *coreAPISlotAssets) assertCommitments(t *testing.T) {
	prevCommitment := a.commitmentPerNode["V1"]
	for _, commitmentID := range a.commitmentPerNode {
		if prevCommitment == iotago.EmptyCommitmentID {
			require.Fail(t, "commitment is empty")
		}

		require.Equal(t, commitmentID, prevCommitment)
		prevCommitment = commitmentID
	}
}

func (a *coreAPISlotAssets) assertBICs(t *testing.T) {
	prevBIC := a.bicPerNode["V1"]
	for _, bic := range a.bicPerNode {
		require.Equal(t, bic, prevBIC)
		prevBIC = bic
	}
}

func newAssetsPerSlot() *coreAPISlotAssets {
	return &coreAPISlotAssets{
		dataBlocks:        make([]*iotago.Block, 0),
		valueBlocks:       make([]*iotago.Block, 0),
		transactions:      make([]*iotago.SignedTransaction, 0),
		reattachments:     make([]iotago.BlockID, 0),
		basicOutputs:      make(map[iotago.OutputID]iotago.Output),
		faucetOutputs:     make(map[iotago.OutputID]iotago.Output),
		delegationOutputs: make(map[iotago.OutputID]iotago.Output),
		commitmentPerNode: make(map[string]iotago.CommitmentID),
		bicPerNode:        make(map[string]iotago.BlockIssuanceCredits),
	}
}

func prepareAssets(d *dockertestframework.DockerTestFramework, totalAssetsNum int) (coreAPIAssets, iotago.SlotIndex) {
	assets := make(coreAPIAssets)
	ctx := context.Background()

	latestSlot := iotago.SlotIndex(0)

	for i := 0; i < totalAssetsNum; i++ {
		// account
		wallet, account := d.CreateAccountFromFaucet()
		assets.setupAssetsForSlot(account.OutputID.Slot())
		assets[account.OutputID.Slot()].accountAddress = account.Address

		// data block
		block := d.CreateTaggedDataBlock(wallet, []byte("tag"))
		blockSlot := lo.PanicOnErr(block.ID()).Slot()
		assets.setupAssetsForSlot(blockSlot)
		assets[blockSlot].dataBlocks = append(assets[blockSlot].dataBlocks, block)
		d.SubmitBlock(ctx, block)

		// transaction
		valueBlock, signedTx, faucetOutput := d.CreateBasicOutputBlock(wallet)
		valueBlockSlot := valueBlock.MustID().Slot()
		assets.setupAssetsForSlot(valueBlockSlot)
		// transaction and outputs are stored with the earliest included block
		assets[valueBlockSlot].valueBlocks = append(assets[valueBlockSlot].valueBlocks, valueBlock)
		assets[valueBlockSlot].transactions = append(assets[valueBlockSlot].transactions, signedTx)
		basicOutputID := iotago.OutputIDFromTransactionIDAndIndex(signedTx.Transaction.MustID(), 0)
		assets[valueBlockSlot].basicOutputs[basicOutputID] = signedTx.Transaction.Outputs[0]
		assets[valueBlockSlot].faucetOutputs[faucetOutput.ID] = faucetOutput.Output
		d.SubmitBlock(ctx, valueBlock)
		d.AwaitTransactionPayloadAccepted(ctx, signedTx.Transaction.MustID())

		// issue reattachment after the first one is already included
		secondAttachment, err := wallet.CreateAndSubmitBasicBlock(ctx, "second_attachment", mock.WithPayload(signedTx))
		require.NoError(d.Testing, err)
		assets[valueBlockSlot].reattachments = append(assets[valueBlockSlot].reattachments, secondAttachment.ID())

		// delegation
		//nolint:forcetypeassert
		delegationOutputData := d.DelegateToValidator(wallet, d.Node("V1").AccountAddress(d.Testing))
		assets.setupAssetsForSlot(delegationOutputData.ID.CreationSlot())
		assets[delegationOutputData.ID.CreationSlot()].delegationOutputs[delegationOutputData.ID] = delegationOutputData.Output.(*iotago.DelegationOutput)

		latestSlot = lo.Max[iotago.SlotIndex](latestSlot, blockSlot, valueBlockSlot, delegationOutputData.ID.CreationSlot(), secondAttachment.ID().Slot())

		fmt.Printf("Assets for slot %d\n: dataBlock: %s block: %s\ntx: %s\nbasic output: %s, faucet output: %s\n delegation output: %s\n",
			valueBlockSlot, block.MustID().String(), valueBlock.MustID().String(), signedTx.MustID().String(),
			basicOutputID.String(), faucetOutput.ID.String(), delegationOutputData.ID.String())
	}

	return assets, latestSlot
}

// Test_ValidatorsAPI tests if the validators API returns the expected validators.
// 1. Run docker network.
// 2. Create 50 new accounts with staking feature.
// 3. Wait until next epoch then issue candidacy payload for each account.
// 4. Check if all 54 validators are returned from the validators API with pageSize 10, the pagination of api is also tested.
// 5. Wait until next epoch then check again if the results remain.
func Test_ValidatorsAPI(t *testing.T) {
	d := dockertestframework.NewDockerTestFramework(t,
		dockertestframework.WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 4),
			iotago.WithLivenessOptions(10, 10, 2, 4, 8),
			iotago.WithRewardsOptions(8, 10, 2, 384),
			iotago.WithTargetCommitteeSize(4),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	runErr := d.Run()
	require.NoError(t, runErr)

	d.WaitUntilNetworkReady()
	hrp := d.DefaultWallet().Client.CommittedAPI().ProtocolParameters().Bech32HRP()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
	})

	// Create registered validators
	var wg sync.WaitGroup
	clt := d.DefaultWallet().Client
	status := d.NodeStatus("V1")
	currentEpoch := clt.CommittedAPI().TimeProvider().EpochFromSlot(status.LatestAcceptedBlockSlot)
	expectedValidators := d.AccountsFromNodes(d.Nodes()...)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// create implicit accounts for every validator
			wallet, implicitAccountOutputData := d.CreateImplicitAccount(ctx)

			// create account with staking feature for every validator
			accountData := d.CreateAccountFromImplicitAccount(wallet,
				implicitAccountOutputData,
				wallet.GetNewBlockIssuanceResponse(),
				dockertestframework.WithStakingFeature(100, 1, currentEpoch),
			)

			expectedValidators = append(expectedValidators, accountData.Address.Bech32(hrp))

			// issue candidacy payload in the next epoch (currentEpoch + 1), in order to issue it before epochNearingThreshold
			d.AwaitCommitment(clt.CommittedAPI().TimeProvider().EpochEnd(currentEpoch))
			blkID := d.IssueCandidacyPayloadFromAccount(wallet)
			fmt.Println("Candidacy payload:", blkID.ToHex(), blkID.Slot())
			d.AwaitCommitment(blkID.Slot())
		}()
	}
	wg.Wait()

	// get all validators of currentEpoch+1 with pageSize 10
	actualValidators := getAllValidatorsOnEpoch(t, clt, 0, 10)
	require.ElementsMatch(t, expectedValidators, actualValidators)

	// wait until currentEpoch+3 and check the results again
	targetSlot := clt.CommittedAPI().TimeProvider().EpochEnd(currentEpoch + 2)
	d.AwaitCommitment(targetSlot)
	actualValidators = getAllValidatorsOnEpoch(t, clt, currentEpoch+1, 10)
	require.ElementsMatch(t, expectedValidators, actualValidators)
}

func Test_CoreAPI(t *testing.T) {
	d := dockertestframework.NewDockerTestFramework(t,
		dockertestframework.WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 4),
			iotago.WithLivenessOptions(10, 10, 2, 4, 8),
			iotago.WithRewardsOptions(8, 10, 2, 384),
			iotago.WithTargetCommitteeSize(4),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	runErr := d.Run()
	require.NoError(t, runErr)

	d.WaitUntilNetworkReady()

	assetsPerSlot, lastSlot := prepareAssets(d, 5)

	fmt.Println("Await finalisation of slot", lastSlot)
	d.AwaitFinalization(lastSlot)

	tests := []struct {
		name     string
		testFunc func(t *testing.T, nodeAlias string)
	}{
		{
			name: "Test_Info",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, err := d.Client(nodeAlias).Info(context.Background())
				require.NoError(t, err)
				require.NotNil(t, resp)
			},
		},
		{
			name: "Test_BlockByBlockID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachBlock(t, func(t *testing.T, block *iotago.Block) {
					respBlock, err := d.Client(nodeAlias).BlockByBlockID(context.Background(), block.MustID())
					require.NoError(t, err)
					require.NotNil(t, respBlock)
					require.Equal(t, block.MustID(), respBlock.MustID(), "BlockID of retrieved block does not match: %s != %s", block.MustID(), respBlock.MustID())
				})
			},
		},
		{
			name: "Test_BlockMetadataByBlockID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachBlock(t, func(t *testing.T, block *iotago.Block) {
					resp, err := d.Client(nodeAlias).BlockMetadataByBlockID(context.Background(), block.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.Equal(t, block.MustID(), resp.BlockID, "BlockID of retrieved block does not match: %s != %s", block.MustID(), resp.BlockID)
					require.Equal(t, api.BlockStateFinalized, resp.BlockState)
				})

				assetsPerSlot.forEachReattachment(t, func(t *testing.T, blockID iotago.BlockID) {
					resp, err := d.Client(nodeAlias).BlockMetadataByBlockID(context.Background(), blockID)
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.Equal(t, blockID, resp.BlockID, "BlockID of retrieved block does not match: %s != %s", blockID, resp.BlockID)
					require.Equal(t, api.BlockStateFinalized, resp.BlockState)
				})
			},
		},
		{
			name: "Test_BlockWithMetadata",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachBlock(t, func(t *testing.T, block *iotago.Block) {
					resp, err := d.Client(nodeAlias).BlockWithMetadataByBlockID(context.Background(), block.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.Equal(t, block.MustID(), resp.Block.MustID(), "BlockID of retrieved block does not match: %s != %s", block.MustID(), resp.Block.MustID())
					require.Equal(t, api.BlockStateFinalized, resp.Metadata.BlockState)
				})
			},
		},
		{
			name: "Test_BlockIssuance",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, err := d.Client(nodeAlias).BlockIssuance(context.Background())
				require.NoError(t, err)
				require.NotNil(t, resp)

				require.GreaterOrEqual(t, len(resp.StrongParents), 1, "There should be at least 1 strong parent provided")
			},
		},
		{
			name: "Test_CommitmentBySlot",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachSlot(t, func(t *testing.T, slot iotago.SlotIndex, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentBySlot(context.Background(), slot)
					require.NoError(t, err)
					require.NotNil(t, resp)
					commitmentsPerNode[nodeAlias] = resp.MustID()
				})
			},
		},
		{
			name: "Test_CommitmentByID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachCommitment(t, func(t *testing.T, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentByID(context.Background(), commitmentsPerNode[nodeAlias])
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.Equal(t, commitmentsPerNode[nodeAlias], resp.MustID(), "Commitment does not match commitment got for the same slot from the same node: %s != %s", commitmentsPerNode[nodeAlias], resp.MustID())
				})
			},
		},
		{
			name: "Test_CommitmentUTXOChangesByID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachCommitment(t, func(t *testing.T, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentUTXOChangesByID(context.Background(), commitmentsPerNode[nodeAlias])
					require.NoError(t, err)
					require.NotNil(t, resp)
					assetsPerSlot.assertUTXOOutputIDsInSlot(t, commitmentsPerNode[nodeAlias].Slot(), resp.CreatedOutputs, resp.ConsumedOutputs)
					require.Equal(t, commitmentsPerNode[nodeAlias], resp.CommitmentID, "CommitmentID of retrieved UTXO changes does not match: %s != %s", commitmentsPerNode[nodeAlias], resp.CommitmentID)
				})
			},
		},
		{
			"Test_CommitmentUTXOChangesFullByID",
			func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachCommitment(t, func(t *testing.T, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentUTXOChangesFullByID(context.Background(), commitmentsPerNode[nodeAlias])
					require.NoError(t, err)
					require.NotNil(t, resp)
					assetsPerSlot.assertUTXOOutputsInSlot(t, commitmentsPerNode[nodeAlias].Slot(), resp.CreatedOutputs, resp.ConsumedOutputs)
					require.Equal(t, commitmentsPerNode[nodeAlias], resp.CommitmentID, "CommitmentID of retrieved UTXO changes does not match: %s != %s", commitmentsPerNode[nodeAlias], resp.CommitmentID)
				})
			},
		},
		{
			name: "Test_CommitmentUTXOChangesBySlot",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachCommitment(t, func(t *testing.T, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentUTXOChangesBySlot(context.Background(), commitmentsPerNode[nodeAlias].Slot())
					require.NoError(t, err)
					require.NotNil(t, resp)
					assetsPerSlot.assertUTXOOutputIDsInSlot(t, commitmentsPerNode[nodeAlias].Slot(), resp.CreatedOutputs, resp.ConsumedOutputs)
					require.Equal(t, commitmentsPerNode[nodeAlias], resp.CommitmentID, "CommitmentID of retrieved UTXO changes does not match: %s != %s", commitmentsPerNode[nodeAlias], resp.CommitmentID)
				})
			},
		},
		{
			name: "Test_CommitmentUTXOChangesFullBySlot",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachCommitment(t, func(t *testing.T, commitmentsPerNode map[string]iotago.CommitmentID) {
					resp, err := d.Client(nodeAlias).CommitmentUTXOChangesFullBySlot(context.Background(), commitmentsPerNode[nodeAlias].Slot())
					require.NoError(t, err)
					require.NotNil(t, resp)
					assetsPerSlot.assertUTXOOutputsInSlot(t, commitmentsPerNode[nodeAlias].Slot(), resp.CreatedOutputs, resp.ConsumedOutputs)
					require.Equal(t, commitmentsPerNode[nodeAlias], resp.CommitmentID, "CommitmentID of retrieved UTXO changes does not match: %s != %s", commitmentsPerNode[nodeAlias], resp.CommitmentID)
				})
			},
		},
		{
			name: "Test_OutputByID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachOutput(t, func(t *testing.T, outputID iotago.OutputID, output iotago.Output) {
					resp, err := d.Client(nodeAlias).OutputByID(context.Background(), outputID)
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.EqualValues(t, output, resp, "Output created is different than retrieved from the API: %s != %s", output, resp)
				})
			},
		},
		{
			name: "Test_OutputMetadata",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachOutput(t, func(t *testing.T, outputID iotago.OutputID, output iotago.Output) {
					resp, err := d.Client(nodeAlias).OutputMetadataByID(context.Background(), outputID)
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.EqualValues(t, outputID, resp.OutputID, "OutputID of retrieved output does not match: %s != %s", outputID, resp.OutputID)
					require.EqualValues(t, outputID.TransactionID(), resp.Included.TransactionID, "TransactionID of retrieved output does not match: %s != %s", outputID.TransactionID(), resp.Included.TransactionID)
				})
			},
		},
		{
			name: "Test_OutputWithMetadata",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachOutput(t, func(t *testing.T, outputID iotago.OutputID, output iotago.Output) {
					out, outMetadata, err := d.Client(nodeAlias).OutputWithMetadataByID(context.Background(), outputID)
					require.NoError(t, err)
					require.NotNil(t, outMetadata)
					require.NotNil(t, out)
					require.EqualValues(t, outputID, outMetadata.OutputID, "OutputID of retrieved output does not match: %s != %s", outputID, outMetadata.OutputID)
					require.EqualValues(t, outputID.TransactionID(), outMetadata.Included.TransactionID, "TransactionID of retrieved output does not match: %s != %s", outputID.TransactionID(), outMetadata.Included.TransactionID)
					require.EqualValues(t, output, out, "OutputID of retrieved output does not match: %s != %s", output, out)
				})
			},
		},
		{
			name: "Test_TransactionByID",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachTransaction(t, func(t *testing.T, transaction *iotago.SignedTransaction, firstAttachmentID iotago.BlockID) {
					txID := transaction.Transaction.MustID()
					resp, err := d.Client(nodeAlias).TransactionByID(context.Background(), txID)
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.EqualValues(t, txID, resp.MustID())
				})
			},
		},
		{
			name: "Test_TransactionsIncludedBlock",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachTransaction(t, func(t *testing.T, transaction *iotago.SignedTransaction, firstAttachmentID iotago.BlockID) {
					resp, err := d.Client(nodeAlias).TransactionIncludedBlock(context.Background(), transaction.Transaction.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.EqualValues(t, firstAttachmentID, resp.MustID())
				})
			},
		},
		{
			name: "Test_TransactionsIncludedBlockMetadata",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachTransaction(t, func(t *testing.T, transaction *iotago.SignedTransaction, firstAttachmentID iotago.BlockID) {
					resp, err := d.Client(nodeAlias).TransactionIncludedBlockMetadata(context.Background(), transaction.Transaction.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.EqualValues(t, api.BlockStateFinalized, resp.BlockState)
					require.EqualValues(t, firstAttachmentID, resp.BlockID, "Inclusion BlockID of retrieved transaction does not match: %s != %s", firstAttachmentID, resp.BlockID)
				})
			},
		},
		{
			name: "Test_TransactionsMetadata",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachTransaction(t, func(t *testing.T, transaction *iotago.SignedTransaction, firstAttachmentID iotago.BlockID) {
					resp, err := d.Client(nodeAlias).TransactionMetadata(context.Background(), transaction.Transaction.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.Equal(t, api.TransactionStateFinalized, resp.TransactionState)
					require.EqualValues(t, resp.EarliestAttachmentSlot, firstAttachmentID.Slot())
				})
			},
		},
		{
			name: "Test_Congestion",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachAccountAddress(t, func(
					t *testing.T,
					accountAddress *iotago.AccountAddress,
					commitmentPerNode map[string]iotago.CommitmentID,
					bicPerNoode map[string]iotago.BlockIssuanceCredits,
				) {
					resp, err := d.Client(nodeAlias).Congestion(context.Background(), accountAddress, 0)
					require.NoError(t, err)
					require.NotNil(t, resp)

					// node allows to get account only for the slot newer than lastCommittedSlot - MCA, we need fresh commitment
					infoRes, err := d.Client(nodeAlias).Info(context.Background())
					require.NoError(t, err)
					commitment, err := d.Client(nodeAlias).CommitmentBySlot(context.Background(), infoRes.Status.LatestCommitmentID.Slot())
					require.NoError(t, err)

					resp, err = d.Client(nodeAlias).Congestion(context.Background(), accountAddress, 0, commitment.MustID())
					require.NoError(t, err)
					require.NotNil(t, resp)
					// later we check if all nodes have returned the same BIC value for this account
					bicPerNoode[nodeAlias] = resp.BlockIssuanceCredits
				})
			},
		},
		{
			name: "Test_Validators",
			testFunc: func(t *testing.T, nodeAlias string) {
				pageSize := uint64(3)
				resp, err := d.Client(nodeAlias).Validators(context.Background(), pageSize)
				require.NoError(t, err)
				require.NotNil(t, resp)
				//TODO after finishing validators endpoint and including registered validators
				//require.Equal(t, int(pageSize), len(resp.Validators), "There should be exactly %d validators returned on the first page", pageSize)

				resp, err = d.Client(nodeAlias).Validators(context.Background(), pageSize, resp.Cursor)
				require.NoError(t, err)
				require.NotNil(t, resp)
				//TODO after finishing validators endpoint and including registered validators
				//require.Equal(t, 1, len(resp.Validators), "There should be only one validator returned on the last page")
			},
		},
		{
			name: "Test_ValidatorsAll",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, all, err := d.Client(nodeAlias).ValidatorsAll(context.Background())
				require.NoError(t, err)
				require.True(t, all)
				require.Equal(t, 4, len(resp.Validators))
			},
		},
		{
			name: "Test_Rewards",
			testFunc: func(t *testing.T, nodeAlias string) {
				assetsPerSlot.forEachOutput(t, func(t *testing.T, outputID iotago.OutputID, output iotago.Output) {
					if output.Type() != iotago.OutputDelegation {
						return
					}

					resp, err := d.Client(nodeAlias).Rewards(context.Background(), outputID)
					require.NoError(t, err)
					require.NotNil(t, resp)
					// rewards are zero, because we do not wait for the epoch end
					require.EqualValues(t, 0, resp.Rewards)
				})
			},
		},
		{
			name: "Test_Committee",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, err := d.Client(nodeAlias).Committee(context.Background())
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.EqualValues(t, 4, len(resp.Committee))
			},
		},
		{
			name: "Test_CommitteeWithEpoch",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, err := d.Client(nodeAlias).Committee(context.Background(), 0)
				require.NoError(t, err)
				require.Equal(t, iotago.EpochIndex(0), resp.Epoch)
				require.Equal(t, 4, len(resp.Committee))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d.RequestFromNodes(test.testFunc)
		})
	}

	// check if the same values were returned by all nodes for the same slot
	assetsPerSlot.assertCommitments(t)
	assetsPerSlot.assertBICs(t)
}

func Test_CoreAPI_BadRequests(t *testing.T) {
	d := dockertestframework.NewDockerTestFramework(t,
		dockertestframework.WithProtocolParametersOptions(
			iotago.WithTimeProviderOptions(5, time.Now().Unix(), 10, 4),
			iotago.WithLivenessOptions(10, 10, 2, 4, 8),
			iotago.WithRewardsOptions(8, 10, 2, 384),
			iotago.WithTargetCommitteeSize(4),
		))
	defer d.Stop()

	d.AddValidatorNode("V1", "docker-network-inx-validator-1-1", "http://localhost:8050", "rms1pzg8cqhfxqhq7pt37y8cs4v5u4kcc48lquy2k73ehsdhf5ukhya3y5rx2w6")
	d.AddValidatorNode("V2", "docker-network-inx-validator-2-1", "http://localhost:8060", "rms1pqm4xk8e9ny5w5rxjkvtp249tfhlwvcshyr3pc0665jvp7g3hc875k538hl")
	d.AddValidatorNode("V3", "docker-network-inx-validator-3-1", "http://localhost:8070", "rms1pp4wuuz0y42caz48vv876qfpmffswsvg40zz8v79sy8cp0jfxm4kunflcgt")
	d.AddValidatorNode("V4", "docker-network-inx-validator-4-1", "http://localhost:8040", "rms1pr8cxs3dzu9xh4cduff4dd4cxdthpjkpwmz2244f75m0urslrsvtsshrrjw")
	d.AddNode("node5", "docker-network-node-5-1", "http://localhost:8090")

	runErr := d.Run()
	require.NoError(t, runErr)

	d.WaitUntilNetworkReady()

	tests := []struct {
		name     string
		testFunc func(t *testing.T, nodeAlias string)
	}{
		{
			name: "Test_BlockByBlockID_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				blockID := tpkg.RandBlockID()
				respBlock, err := d.Client(nodeAlias).BlockByBlockID(context.Background(), blockID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, respBlock)
			},
		},
		{
			name: "Test_BlockMetadataByBlockID_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				blockID := tpkg.RandBlockID()
				resp, err := d.Client(nodeAlias).BlockMetadataByBlockID(context.Background(), blockID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_BlockWithMetadata_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				blockID := tpkg.RandBlockID()
				resp, err := d.Client(nodeAlias).BlockWithMetadataByBlockID(context.Background(), blockID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_CommitmentBySlot_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				slot := iotago.SlotIndex(1000_000_000)
				resp, err := d.Client(nodeAlias).CommitmentBySlot(context.Background(), slot)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_CommitmentByID_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				committmentID := tpkg.RandCommitmentID()
				resp, err := d.Client(nodeAlias).CommitmentByID(context.Background(), committmentID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_CommitmentUTXOChangesByID_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				committmentID := tpkg.RandCommitmentID()
				resp, err := d.Client(nodeAlias).CommitmentUTXOChangesByID(context.Background(), committmentID)
				require.Error(t, err)
				// commitmentID is valid, but the UTXO changes does not exist in the storage
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			"Test_CommitmentUTXOChangesFullByID_Failure",
			func(t *testing.T, nodeAlias string) {
				committmentID := tpkg.RandCommitmentID()

				resp, err := d.Client(nodeAlias).CommitmentUTXOChangesFullByID(context.Background(), committmentID)
				require.Error(t, err)
				// commitmentID is valid, but the UTXO changes does not exist in the storage
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_CommitmentUTXOChangesBySlot_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				slot := iotago.SlotIndex(1000_000_000)
				resp, err := d.Client(nodeAlias).CommitmentUTXOChangesBySlot(context.Background(), slot)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_CommitmentUTXOChangesFullBySlot_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				slot := iotago.SlotIndex(1000_000_000)

				resp, err := d.Client(nodeAlias).CommitmentUTXOChangesFullBySlot(context.Background(), slot)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_OutputByID_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				outputID := tpkg.RandOutputID(0)
				resp, err := d.Client(nodeAlias).OutputByID(context.Background(), outputID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_OutputMetadata_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				outputID := tpkg.RandOutputID(0)

				resp, err := d.Client(nodeAlias).OutputMetadataByID(context.Background(), outputID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_OutputWithMetadata_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				outputID := tpkg.RandOutputID(0)

				out, outMetadata, err := d.Client(nodeAlias).OutputWithMetadataByID(context.Background(), outputID)
				require.Error(t, err)
				require.Nil(t, out)
				require.Nil(t, outMetadata)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
			},
		},
		{
			name: "Test_TransactionsIncludedBlock_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				txID := tpkg.RandTransactionID()
				resp, err := d.Client(nodeAlias).TransactionIncludedBlock(context.Background(), txID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_TransactionsIncludedBlockMetadata_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				txID := tpkg.RandTransactionID()

				resp, err := d.Client(nodeAlias).TransactionIncludedBlockMetadata(context.Background(), txID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_TransactionsMetadata_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				txID := tpkg.RandTransactionID()

				resp, err := d.Client(nodeAlias).TransactionMetadata(context.Background(), txID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_Congestion_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				accountAddress := tpkg.RandAccountAddress()
				commitmentID := tpkg.RandCommitmentID()
				resp, err := d.Client(nodeAlias).Congestion(context.Background(), accountAddress, 0, commitmentID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_Committee_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				resp, err := d.Client(nodeAlias).Committee(context.Background(), 4)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusBadRequest))
				require.Nil(t, resp)
			},
		},
		{
			name: "Test_Rewards_Failure",
			testFunc: func(t *testing.T, nodeAlias string) {
				outputID := tpkg.RandOutputID(0)
				resp, err := d.Client(nodeAlias).Rewards(context.Background(), outputID)
				require.Error(t, err)
				require.True(t, dockertestframework.IsStatusCode(err, http.StatusNotFound))
				require.Nil(t, resp)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d.RequestFromNodes(test.testFunc)
		})
	}
}

func getAllValidatorsOnEpoch(t *testing.T, clt mock.Client, epoch iotago.EpochIndex, pageSize uint64) []string {
	actualValidators := make([]string, 0)
	cursor := ""
	if epoch != 0 {
		cursor = fmt.Sprintf("%d,%d", epoch, 0)
	}

	for {
		resp, err := clt.Validators(context.Background(), pageSize, cursor)
		require.NoError(t, err)

		for _, v := range resp.Validators {
			actualValidators = append(actualValidators, v.AddressBech32)
		}

		cursor = resp.Cursor
		if cursor == "" {
			break
		}
	}

	return actualValidators
}
