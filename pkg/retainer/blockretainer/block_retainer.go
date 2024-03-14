package blockretainer

import (
	"sync"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/retainer"
	"github.com/iotaledger/iota-core/pkg/storage/prunable/slotstore"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

type (
	StoreFunc         func(iotago.SlotIndex) (*slotstore.BlockMetadataStore, error)
	FinalizedSlotFunc func() iotago.SlotIndex
)

var ErrEntryNotFound = ierrors.New("block metadatanot found")

type BlockRetainer struct {
	events *retainer.Events
	store  StoreFunc
	cache  *BlockRetainerCache

	latestCommittedSlot iotago.SlotIndex
	finalizedSlotFunc   FinalizedSlotFunc
	errorHandler        func(error)

	workerPool *workerpool.WorkerPool
	sync.RWMutex

	module.Module
}

func New(workersGroup *workerpool.Group, retainerStoreFunc StoreFunc, finalizedSlotFunc FinalizedSlotFunc, errorHandler func(error)) *BlockRetainer {
	return &BlockRetainer{
		events:            retainer.NewEvents(),
		workerPool:        workersGroup.CreatePool("Retainer", workerpool.WithWorkerCount(1)),
		store:             retainerStoreFunc,
		cache:             NewBlockRetainerCache(),
		finalizedSlotFunc: finalizedSlotFunc,
		errorHandler:      errorHandler,
	}
}

// NewProvider creates a new BlockRetainer provider.
func NewProvider() module.Provider[*engine.Engine, retainer.BlockRetainer] {
	return module.Provide(func(e *engine.Engine) retainer.BlockRetainer {
		r := New(e.Workers.CreateGroup("Retainer"),
			e.Storage.BlockMetadata,
			func() iotago.SlotIndex {
				return e.SyncManager.LatestFinalizedSlot()
			},
			e.ErrorHandler("retainer"))

		asyncOpt := event.WithWorkerPool(r.workerPool)

		e.Events.Booker.BlockBooked.Hook(func(b *blocks.Block) {
			if err := r.OnBlockBooked(b); err != nil {
				r.errorHandler(ierrors.Wrap(err, "failed to store on BlockBooked in retainer"))
			}
		}, asyncOpt)

		e.Events.BlockGadget.BlockAccepted.Hook(func(b *blocks.Block) {
			if err := r.OnBlockAccepted(b.ID()); err != nil {
				r.errorHandler(ierrors.Wrap(err, "failed to store on BlockAccepted in retainer"))
			}
		}, asyncOpt)

		e.Events.BlockGadget.BlockConfirmed.Hook(func(b *blocks.Block) {
			if err := r.OnBlockConfirmed(b.ID()); err != nil {
				r.errorHandler(ierrors.Wrap(err, "failed to store on BlockConfirmed in retainer"))
			}
		}, asyncOpt)

		e.Events.Scheduler.BlockDropped.Hook(func(b *blocks.Block, _ error) {
			if err := r.OnBlockDropped(b.ID()); err != nil {
				r.errorHandler(ierrors.Wrap(err, "failed to store on BlockDropped in retainer"))
			}
		})

		// this event is fired when a new commitment is detected
		e.Events.Notarization.LatestCommitmentUpdated.Hook(func(commitment *model.Commitment) {
			if err := r.CommitSlot(commitment.Slot()); err != nil {
				panic(err)
			}
		}, asyncOpt)

		e.Events.Retainer.BlockRetained.LinkTo(r.events.BlockRetained)

		r.TriggerInitialized()

		return r
	})
}

// Reset resets the component to a clean state as if it was created at the last commitment.
func (r *BlockRetainer) Reset(targetSlot iotago.SlotIndex) {
	r.Lock()
	defer r.Unlock()

	r.latestCommittedSlot = targetSlot
	r.cache.uncommittedBlockMetadataChanges.Clear()
	r.resetConfirmations(targetSlot)
}

func (r *BlockRetainer) resetConfirmations(targetSlot iotago.SlotIndex) {
	for slot := r.finalizedSlotFunc(); slot <= targetSlot; slot++ {
		store, err := r.store(slot)
		if err != nil {
			r.errorHandler(ierrors.Wrapf(err, "could not get retainer store for slot %d", slot))
			continue
		}

		// reset all block confirmations
		store.ResetConfirmations()
	}
}

func (r *BlockRetainer) Shutdown() {
	r.workerPool.Shutdown()
}

func (r *BlockRetainer) BlockMetadata(blockID iotago.BlockID) (*api.BlockMetadataResponse, error) {
	r.RLock()
	defer r.RUnlock()

	blockStatus, err := r.blockState(blockID)
	if err != nil {
		return nil, ierrors.Wrapf(err, "block %s not found", blockID.ToHex())
	}

	return &api.BlockMetadataResponse{
		BlockID:    blockID,
		BlockState: blockStatus,
	}, nil
}

func (r *BlockRetainer) blockState(blockID iotago.BlockID) (api.BlockState, error) {
	var state api.BlockState
	state, found := r.cache.blockMetadataByID(blockID)
	if !found {
		// block is not committed yet, should be in cache
		if blockID.Slot() > r.latestCommittedSlot {
			return api.BlockStateUnknown, ErrEntryNotFound
		}

		blockMetadata, err := r.getBlockMetadata(blockID)
		if err != nil {
			// use custm error to align with the cache
			if ierrors.Is(err, kvstore.ErrKeyNotFound) {
				err = ierrors.Join(err, ErrEntryNotFound)
			}

			return api.BlockStateUnknown, err
		}

		state = blockMetadata.State
	}

	switch state {
	case api.BlockStatePending, api.BlockStateDropped:
		if blockID.Slot() <= r.latestCommittedSlot {
			return api.BlockStateOrphaned, nil
		}
	case api.BlockStateAccepted, api.BlockStateConfirmed:
		if blockID.Slot() <= r.finalizedSlotFunc() {
			return api.BlockStateFinalized, nil
		}
	}

	return state, nil
}

func (r *BlockRetainer) getBlockMetadata(blockID iotago.BlockID) (*slotstore.BlockMetadata, error) {
	store, err := r.store(blockID.Slot())
	if err != nil {
		return nil, err
	}

	data, err := store.BlockMetadata(blockID)
	if err != nil {
		return nil, ierrors.Wrapf(err, "block %s not found", blockID.String())
	}

	return data, nil
}

// OnBlockBooked triggers storing block in the retainer on block booked event.
func (r *BlockRetainer) OnBlockBooked(block *blocks.Block) error {
	if err := r.setBlockBooked(block.ID()); err != nil {
		return err
	}

	r.events.BlockRetained.Trigger(block)

	return nil
}

func (r *BlockRetainer) setBlockBooked(blockID iotago.BlockID) error {
	return r.UpdateBlockMetadata(blockID, api.BlockStatePending)
}

func (r *BlockRetainer) OnBlockAccepted(blockID iotago.BlockID) error {
	return r.UpdateBlockMetadata(blockID, api.BlockStateAccepted)
}

func (r *BlockRetainer) OnBlockConfirmed(blockID iotago.BlockID) error {
	return r.UpdateBlockMetadata(blockID, api.BlockStateConfirmed)
}

func (r *BlockRetainer) OnBlockDropped(blockID iotago.BlockID) error {
	return r.UpdateBlockMetadata(blockID, api.BlockStateDropped)
}

func (r *BlockRetainer) UpdateBlockMetadata(blockID iotago.BlockID, state api.BlockState) error {
	r.Lock()
	defer r.Unlock()

	// we can safely use this as a check where block is stored as r.latestCommittedSlot is updated on commitment
	if blockID.Slot() > r.latestCommittedSlot {
		r.cache.setBlockMetadata(blockID, state)

		return nil
	}

	//  for blocks the state might still change after the commitment but only on confirmation
	if state != api.BlockStateConfirmed {
		return ierrors.Errorf("cannot update block metadata for block %s with state %s as block is already committed", blockID.String(), state)
	}

	// store in the database
	store, err := r.store(blockID.Slot())
	if err != nil {
		return ierrors.Wrapf(err, "could not get retainer store for slot %d", blockID.Slot())
	}

	return store.StoreBlockMetadata(blockID, state)
}

func (r *BlockRetainer) CommitSlot(committedSlot iotago.SlotIndex) error {
	r.Lock()
	defer r.Unlock()

	slots := make(map[iotago.SlotIndex]map[iotago.BlockID]api.BlockState)
	r.cache.uncommittedBlockMetadataChanges.ForEach(func(cacheSlot iotago.SlotIndex, blocks map[iotago.BlockID]api.BlockState) bool {
		if cacheSlot <= committedSlot {
			slots[committedSlot] = blocks
		}

		return true
	})

	// save committed changes to the database
	for slotIndex, blockStates := range slots {
		store, err := r.store(slotIndex)
		if err != nil {
			return ierrors.Wrapf(err, "could not get retainer store for slot %d", slotIndex)
		}

		for blockID, state := range blockStates {
			if err := store.StoreBlockMetadata(blockID, state); err != nil {
				return ierrors.Wrapf(err, "could not store block metadata for block %s", blockID.String())
			}
		}
	}

	// delete committed changes from the cache
	for slotIndex := range slots {
		r.cache.uncommittedBlockMetadataChanges.Delete(slotIndex)
	}

	r.latestCommittedSlot = committedSlot

	return nil
}
