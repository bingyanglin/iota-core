package utxoledger

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
	"golang.org/x/xerrors"

	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/core/vote"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledgerstate"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag/conflictdagv1"
	mempoolv1 "github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/v1"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/sybilprotection"
	iotago "github.com/iotaledger/iota.go/v4"
)

var ErrUnexpectedUnderlyingType = errors.New("unexpected underlying type provided by the interface")

type Ledger struct {
	ledgerState     *ledgerstate.Manager
	sybylProtection sybilprotection.SybilProtection
	memPool         mempool.MemPool[ledger.BlockVotePower]
	conflictDAG     conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, ledger.BlockVotePower]
	errorHandler    func(error)

	module.Module
}

func NewProvider() module.Provider[*engine.Engine, ledger.Ledger] {
	return module.Provide(func(e *engine.Engine) ledger.Ledger {
		l := New(e.Workers.CreateGroup("Ledger"), e.Storage.Ledger(), executeStardustVM, e.API, e.SybilProtection, e.ErrorHandler("ledger"))

		e.Events.ConflictDAG.LinkTo(l.conflictDAG.Events())

		e.Events.BlockGadget.BlockPreAccepted.Hook(l.blockPreAccepted)

		e.Events.BlockGadget.BlockAccepted.Hook(l.BlockAccepted)
		e.EvictionState.Events.SlotEvicted.Hook(l.memPool.Evict)
		// TODO: when should ledgerState be pruned?

		return l
	})
}

func New(workers *workerpool.Group, store kvstore.KVStore, vm mempool.VM, apiProviderFunc func() iotago.API, sybylProtection sybilprotection.SybilProtection, errorHandler func(error)) *Ledger {
	l := &Ledger{
		sybylProtection: sybylProtection,
		ledgerState:     ledgerstate.New(store, apiProviderFunc),
		conflictDAG:     conflictdagv1.New[iotago.TransactionID, iotago.OutputID, ledger.BlockVotePower](sybylProtection.OnlineCommittee().Size),
		errorHandler:    errorHandler,
	}

	l.memPool = mempoolv1.New(vm, l.resolveState, workers.CreateGroup("MemPool"), l.conflictDAG, mempoolv1.WithForkAllTransactions[ledger.BlockVotePower](true))

	return l
}

func (l *Ledger) ConflictDAG() conflictdag.ConflictDAG[iotago.TransactionID, iotago.OutputID, ledger.BlockVotePower] {
	return l.conflictDAG
}

func (l *Ledger) Shutdown() {
	l.TriggerStopped()
	l.conflictDAG.Shutdown()
}

func (l *Ledger) Import(reader io.ReadSeeker) error {
	return l.ledgerState.Import(reader)
}

func (l *Ledger) Export(writer io.WriteSeeker, targetIndex iotago.SlotIndex) error {
	return l.ledgerState.Export(writer, targetIndex)
}

func (l *Ledger) resolveState(stateRef iotago.IndexedUTXOReferencer) *promise.Promise[mempool.State] {
	p := promise.New[mempool.State]()

	l.ledgerState.ReadLockLedger()
	defer l.ledgerState.ReadUnlockLedger()

	isUnspent, err := l.ledgerState.IsOutputIDUnspentWithoutLocking(stateRef.Ref())
	if err != nil {
		p.Reject(xerrors.Errorf("error while retrieving output %s: %w", stateRef.Ref(), err))
	}

	if !isUnspent {
		p.Reject(xerrors.Errorf("unspent output %s not found: %w", stateRef.Ref(), mempool.ErrStateNotFound))
	}

	// possible to cast `stateRef` to more specialized interfaces here, e.g. for DustOutput
	output, err := l.ledgerState.ReadOutputByOutputID(stateRef.Ref())

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

		return ledgerstate.CreateOutput(l.ledgerState.API(), stateWithMetadata.State().OutputID(), earliestAttachment, earliestAttachment.Index(), tx.Essence.CreationTime, stateWithMetadata.State().Output()), nil
	default:
		panic("unexpected State type")
	}
}

func (l *Ledger) CommitSlot(index iotago.SlotIndex) (stateRoot iotago.Identifier, mutationRoot iotago.Identifier, err error) {
	ledgerIndex, err := l.ledgerState.ReadLedgerIndex()
	if err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, err
	}

	if index != ledgerIndex+1 {
		panic(fmt.Errorf("there is a gap in the ledgerstate %d vs %d", ledgerIndex, index))
	}

	stateDiff := l.memPool.StateDiff(index)

	var innerErr error
	var outputs ledgerstate.Outputs
	var spents ledgerstate.Spents

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

			spent := ledgerstate.NewSpent(inputState, txWithMeta.ID(), txCreationTime, index)
			spents = append(spents, spent)
		}

		if createOutputErr := txWithMeta.Outputs().ForEach(func(element mempool.StateMetadata) error {
			output := ledgerstate.CreateOutput(l.ledgerState.API(), element.State().OutputID(), txWithMeta.EarliestIncludedAttachment(), index, txCreationTime, element.State().Output())
			outputs = append(outputs, output)

			return nil
		}); createOutputErr != nil {
			innerErr = createOutputErr
			return false
		}

		return true
	})

	if innerErr != nil {
		return iotago.Identifier{}, iotago.Identifier{}, innerErr
	}

	if err := l.ledgerState.ApplyDiff(index, outputs, spents); err != nil {
		return iotago.Identifier{}, iotago.Identifier{}, err
	}

	// Mark the transactions as committed so the mempool can evict it.
	stateDiff.ExecutedTransactions().ForEach(func(_ iotago.TransactionID, tx mempool.TransactionMetadata) bool {
		tx.Commit()
		return true
	})

	return l.ledgerState.StateTreeRoot(), iotago.Identifier(stateDiff.Mutations().Root()), nil
}

func (l *Ledger) IsOutputUnspent(outputID iotago.OutputID) (bool, error) {
	return l.ledgerState.IsOutputIDUnspentWithoutLocking(outputID)
}

func (l *Ledger) Spent(outputID iotago.OutputID) (*ledgerstate.Spent, error) {
	return l.ledgerState.ReadSpentForOutputIDWithoutLocking(outputID)
}

func (l *Ledger) StateDiffs(index iotago.SlotIndex) (*ledgerstate.SlotDiff, error) {
	return l.ledgerState.SlotDiffWithoutLocking(index)
}

func (l *Ledger) AddUnspentOutput(unspentOutput *ledgerstate.Output) error {
	return l.ledgerState.AddUnspentOutput(unspentOutput)
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

func (l *Ledger) blockPreAccepted(block *blocks.Block) {
	votePower := ledger.NewBlockVotePower(block.ID(), block.Block().IssuingTime)

	seat, exists := l.sybylProtection.Committee(block.ID().Index()).GetSeat(block.Block().IssuerID)
	if !exists {
		return
	}

	if err := l.conflictDAG.CastVotes(vote.NewVote(seat, votePower), block.ConflictIDs()); err != nil {
		// TODO: here we need to check what kind of error and potentially mark the block as invalid.
		//  Do we track witness weight of invalid blocks?
		l.errorHandler(errors.Wrapf(err, "failed to cast votes for block %s", block.ID()))
	}
}
