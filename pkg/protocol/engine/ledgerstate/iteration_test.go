//nolint:forcetypeassert,varnamelen,revive,exhaustruct // we don't care about these linters in test cases
package ledgerstate_test

import (
	"github.com/iotaledger/iota-core/pkg/utils"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledgerstate"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledgerstate/tpkg"
	iotago "github.com/iotaledger/iota.go/v4"
)

func TestUTXOComputeBalance(t *testing.T) {
	manager := ledgerstate.New(mapdb.NewMapDB(), tpkg.API)

	initialOutput := tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519), 2_134_656_365)
	require.NoError(t, manager.AddUnspentOutput(initialOutput))
	require.NoError(t, manager.AddUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputAccount, utils.RandAddress(iotago.AddressAccount), 56_549_524)))
	require.NoError(t, manager.AddUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount), 25_548_858)))
	require.NoError(t, manager.AddUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputNFT, utils.RandAddress(iotago.AddressEd25519), 545_699_656)))
	require.NoError(t, manager.AddUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount), 626_659_696)))

	index := iotago.SlotIndex(756)

	outputs := ledgerstate.Outputs{
		tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT), 2_134_656_365),
	}

	spents := ledgerstate.Spents{
		tpkg.RandLedgerStateSpentWithOutput(initialOutput, index, utils.RandTimestamp()),
	}

	require.NoError(t, manager.ApplyDiffWithoutLocking(index, outputs, spents))

	spent, err := manager.SpentOutputs()
	require.NoError(t, err)
	require.Equal(t, 1, len(spent))

	unspent, err := manager.UnspentOutputs()
	require.NoError(t, err)
	require.Equal(t, 5, len(unspent))

	balance, count, err := manager.ComputeLedgerBalance()
	require.NoError(t, err)
	require.Equal(t, 5, count)
	require.Equal(t, uint64(2_134_656_365+56_549_524+25_548_858+545_699_656+626_659_696), balance)
}

func TestUTXOIteration(t *testing.T) {
	manager := ledgerstate.New(mapdb.NewMapDB(), tpkg.API)

	outputs := ledgerstate.Outputs{
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputAccount, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
	}

	index := iotago.SlotIndex(756)

	spents := ledgerstate.Spents{
		tpkg.RandLedgerStateSpentWithOutput(outputs[3], index, utils.RandTimestamp()),
		tpkg.RandLedgerStateSpentWithOutput(outputs[2], index, utils.RandTimestamp()),
		tpkg.RandLedgerStateSpentWithOutput(outputs[9], index, utils.RandTimestamp()),
	}

	require.NoError(t, manager.ApplyDiffWithoutLocking(index, outputs, spents))

	// Prepare values to check
	outputByID := make(map[string]struct{})
	unspentByID := make(map[string]struct{})
	spentByID := make(map[string]struct{})

	for _, output := range outputs {
		outputByID[output.MapKey()] = struct{}{}
		unspentByID[output.MapKey()] = struct{}{}
	}
	for _, spent := range spents {
		spentByID[spent.MapKey()] = struct{}{}
		delete(unspentByID, spent.MapKey())
	}

	// Test iteration without filters
	require.NoError(t, manager.ForEachOutput(func(output *ledgerstate.Output) bool {
		_, has := outputByID[output.MapKey()]
		require.True(t, has)
		delete(outputByID, output.MapKey())

		return true
	}))

	require.Empty(t, outputByID)

	require.NoError(t, manager.ForEachUnspentOutput(func(output *ledgerstate.Output) bool {
		_, has := unspentByID[output.MapKey()]
		require.True(t, has)
		delete(unspentByID, output.MapKey())

		return true
	}))
	require.Empty(t, unspentByID)

	require.NoError(t, manager.ForEachSpentOutput(func(spent *ledgerstate.Spent) bool {
		_, has := spentByID[spent.MapKey()]
		require.True(t, has)
		delete(spentByID, spent.MapKey())

		return true
	}))

	require.Empty(t, spentByID)
}
