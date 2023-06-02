package utxoledger

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/xerrors"

	"github.com/iotaledger/hive.go/core/account"
	"github.com/iotaledger/hive.go/ds/advancedset"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts/bic"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts/mana"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/booker"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledgerstate"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag/conflictdagv1"
	mempoolv1 "github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/v1"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
)

var ErrUnexpectedUnderlyingType = errors.New("unexpected underlying type provided by the interface")

type Ledger struct {
	utxoLedger       *ledgerstate.Manager
	accountsLedger   *bic.BICManager
	manaManager      *mana.Manager
	commitmentLoader func(iotago.SlotIndex) (*model.Commitment, error)

	memPool      mempool.MemPool[booker.BlockVotePower]
	conflictDAG  conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, booker.BlockVotePower]
	errorHandler func(error)

	module.Module
}

func NewProvider() module.Provider[*engine.Engine, ledger.Ledger] {
	return module.Provide(func(e *engine.Engine) ledger.Ledger {
		ledgerWorkers := e.Workers.CreateGroup("Ledger")

		l := New(
			ledgerWorkers,
			e.Storage.Ledger(),
			e.Storage.Accounts(),
			e.Storage.Commitments().Load,
			e.SybilProtection.OnlineCommittee(),
			e.BlockCache.Block,
			e.Storage.BicDiffs,
			e.API,
			e.ErrorHandler("ledger"),
		)

		e.Events.ConflictDAG.LinkTo(l.conflictDAG.Events())

		// TODO: should this attach to RatifiedAccepted instead?
		e.Events.BlockGadget.BlockAccepted.Hook(l.BlockAccepted)

		e.HookConstructed(func() {
			wpBic := ledgerWorkers.CreateGroup("BIC").CreatePool("trackBurnt", 1)
			e.Events.BlockGadget.BlockRatifiedAccepted.Hook(l.accountsLedger.TrackBlock, event.WithWorkerPool(wpBic))
		})
		e.HookInitialized(func() {
			l.accountsLedger.SetBICIndex(e.Storage.Settings().LatestCommitment().Index())
		})

		return l
	})
}

func New(
	workers *workerpool.Group,
	utxoStore kvstore.KVStore,
	accountsStore kvstore.KVStore,
	commitmentLoader func(iotago.SlotIndex) (*model.Commitment, error),
	committee *account.SelectedAccounts[iotago.AccountID, *iotago.AccountID],
	blocksFunc func(id iotago.BlockID) (*blocks.Block, bool),
	slotDiffFunc func(iotago.SlotIndex) *prunable.BicDiffs,
	apiProviderFunc func() iotago.API,
	errorHandler func(error),
) *Ledger {
	l := &Ledger{
		utxoLedger:       ledgerstate.New(utxoStore, apiProviderFunc),
		accountsLedger:   bic.New(blocksFunc, slotDiffFunc, accountsStore, apiProviderFunc()),
		commitmentLoader: commitmentLoader,
		conflictDAG:      conflictdagv1.New[iotago.TransactionID, iotago.OutputID, booker.BlockVotePower](committee),
		errorHandler:     errorHandler,
	}

	l.memPool = mempoolv1.New(l.executeStardustVM, l.resolveState, workers.CreateGroup("MemPool"), l.conflictDAG, mempoolv1.WithForkAllTransactions[booker.BlockVotePower](true))

	return l
}

func (l *Ledger) ConflictDAG() conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, booker.BlockVotePower] {
	return l.conflictDAG
}

func (l *Ledger) Shutdown() {
	l.TriggerStopped()
	l.conflictDAG.Shutdown()
}

func (l *Ledger) Import(reader io.ReadSeeker) error {
	if err := l.utxoLedger.Import(reader); err != nil {
		return err
	}

	if err := l.accountsLedger.Import(reader); err != nil {
		return err
	}

	return nil
}

func (l *Ledger) Export(writer io.WriteSeeker, targetIndex iotago.SlotIndex) error {
	if err := l.utxoLedger.Export(writer, targetIndex); err != nil {
		return err
	}

	if err := l.accountsLedger.Export(writer, targetIndex); err != nil {
		return err
	}

	return nil
}

func (l *Ledger) resolveState(stateRef iotago.IndexedUTXOReferencer) *promise.Promise[mempool.State] {
	p := promise.New[mempool.State]()

	// possible to cast `stateRef` to more specialized interfaces here, e.g. for DustOutput
	output, err := l.utxoLedger.ReadOutputByOutputID(stateRef.Ref())
	if err != nil {
		p.Reject(xerrors.Errorf("output %s not found: %w", stateRef.Ref(), mempool.ErrStateNotFound))
	} else {
		p.Resolve(output)
	}

	return p
}

func (l *Ledger) Output(stateRef iotago.IndexedUTXOReferencer) (*ledgerstate.Output, error) {
	stateWithMetadata, err := l.memPool.StateMetadata(stateRef)
	if err != nil {
		return nil, err
	}

	switch castState := stateWithMetadata.State().(type) {
	case *ledgerstate.Output:
		return castState, nil
	case *ExecutionOutput:
		txWithMetadata, exists := l.memPool.TransactionMetadata(stateRef.Ref().TransactionID())
		if !exists {
			return nil, err
		}

		earliestAttachment := txWithMetadata.EarliestIncludedAttachment()

		tx, ok := txWithMetadata.Transaction().(*iotago.Transaction)
		if !ok {
			return nil, ErrUnexpectedUnderlyingType
		}

		return ledgerstate.CreateOutput(l.utxoLedger.API(), stateWithMetadata.State().OutputID(), earliestAttachment, earliestAttachment.Index(), tx.Essence.CreationTime, stateWithMetadata.State().Output()), nil
	default:
		panic("unexpected State type")
	}
}

func (l *Ledger) CommitSlot(index iotago.SlotIndex) (stateRoot iotago.Identifier, mutationRoot iotago.Identifier, bicRoot iotago.Identifier, err error) {
	ledgerIndex, err := l.utxoLedger.ReadLedgerIndex()
	if err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, err
	}

	if index != ledgerIndex+1 {
		panic(fmt.Errorf("there is a gap in the ledgerstate %d vs %d", ledgerIndex, index))
	}

	stateDiff := l.memPool.StateDiff(index)

	var innerErr error
	var outputs ledgerstate.Outputs
	var spents ledgerstate.Spents
	bicDiffChanges := make(map[iotago.AccountID]*prunable.BicDiffChange)
	destroyedAccounts := advancedset.New[iotago.AccountID]()
	consumedAccounts := make(map[iotago.AccountID]*iotago.AccountOutput)
	outputAccounts := make(map[iotago.AccountID]*iotago.AccountOutput)

	stateDiff.ExecutedTransactions().ForEach(func(txID iotago.TransactionID, txWithMeta mempool.TransactionMetadata) bool {
		tx, ok := txWithMeta.Transaction().(*iotago.Transaction)
		if !ok {
			innerErr = ErrUnexpectedUnderlyingType
			return false
		}
		txCreationTime := tx.Essence.CreationTime

		inputRefs, errInput := tx.Inputs()
		if errInput != nil {
			innerErr = errInput
			return false
		}

		for _, inputRef := range inputRefs {
			inputState, outputErr := l.Output(inputRef)
			if outputErr != nil {
				innerErr = outputErr
				return false
			}

			spent := ledgerstate.NewSpent(inputState, txWithMeta.ID(), index)
			spents = append(spents, spent)

			if spent.OutputType() == iotago.OutputAccount {
				accountOutput := spent.Output().Output().(*iotago.AccountOutput)
				consumedAccounts[accountOutput.AccountID] = accountOutput
				delete(outputAccounts, accountOutput.AccountID)
			}
		}

		if createOutputErr := txWithMeta.Outputs().ForEach(func(element mempool.StateMetadata) error {
			output := ledgerstate.CreateOutput(l.utxoLedger.API(), element.State().OutputID(), txWithMeta.EarliestIncludedAttachment(), index, txCreationTime, element.State().Output())
			outputs = append(outputs, output)

			if output.OutputType() == iotago.OutputAccount {
				accountOutput := output.Output().(*iotago.AccountOutput)
				outputAccounts[accountOutput.AccountID] = accountOutput
			}

			return nil
		}); createOutputErr != nil {
			innerErr = createOutputErr
			return false
		}

		for _, allotment := range tx.Essence.Allotments {
			bicDiffChanges[allotment.AccountID].Change += int64(allotment.Value)
		}

		return true
	})

	for accountID := range consumedAccounts {
		if _, exists := outputAccounts[accountID]; !exists {
			destroyedAccounts.Add(accountID)
		}
	}

	l.manaManager.CommitSlot(index, destroyedAccounts, outputAccounts)

	if innerErr != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, nil
	}

	if err := l.utxoLedger.ApplyDiff(index, outputs, spents); err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, nil
	}
	//todo polulate pubKeys, destroyed accounts before commiting
	if bicRoot, err = l.accountsLedger.CommitSlot(index, bicDiffChanges, destroyedAccounts); err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, iotago.Identifier{}, err
	}

	// Mark the transactions as committed so the mempool can evict it.
	stateDiff.ExecutedTransactions().ForEach(func(_ iotago.TransactionID, tx mempool.TransactionMetadata) bool {
		tx.Commit()
		return true
	})

	return l.utxoLedger.StateTreeRoot(), iotago.Identifier(stateDiff.Mutations().Root()), bicRoot, nil
}

func (l *Ledger) IsOutputUnspent(outputID iotago.OutputID) (bool, error) {
	return l.utxoLedger.IsOutputIDUnspentWithoutLocking(outputID)
}

func (l *Ledger) Spent(outputID iotago.OutputID) (*ledgerstate.Spent, error) {
	return l.utxoLedger.ReadSpentForOutputIDWithoutLocking(outputID)
}

func (l *Ledger) StateDiffs(index iotago.SlotIndex) (*ledgerstate.SlotDiff, error) {
	return l.utxoLedger.SlotDiffWithoutLocking(index)
}

func (l *Ledger) AddUnspentOutput(unspentOutput *ledgerstate.Output) error {
	return l.utxoLedger.AddUnspentOutput(unspentOutput)
}

func (l *Ledger) AttachTransaction(block *blocks.Block) (transactionMetadata mempool.TransactionMetadata, containsTransaction bool) {
	switch payload := block.Block().Payload.(type) {
	case mempool.Transaction:
		transactioMetadata, err := l.memPool.AttachTransaction(payload, block.ID())
		if err != nil {
			l.errorHandler(err)

			return nil, true
		}

		return transactioMetadata, true
	default:

		return nil, false
	}
}

func (l *Ledger) OnTransactionAttached(handler func(transaction mempool.TransactionMetadata), opts ...event.Option) {
	l.memPool.OnTransactionAttached(handler, opts...)
}

func (l *Ledger) TransactionMetadata(transactionID iotago.TransactionID) (mempool.TransactionMetadata, bool) {
	return l.memPool.TransactionMetadata(transactionID)
}
func (l *Ledger) TransactionMetadataByAttachment(blockID iotago.BlockID) (mempool.TransactionMetadata, bool) {
	return l.memPool.TransactionMetadataByAttachment(blockID)
}

func (l *Ledger) BlockAccepted(block *blocks.Block) {
	switch block.Block().Payload.(type) {
	case *iotago.Transaction:
		l.memPool.MarkAttachmentIncluded(block.ID())
	default:
		return
	}
}
