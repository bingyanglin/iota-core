package thresholdblockgadget

import (
	"github.com/iotaledger/hive.go/ds"
	"github.com/iotaledger/hive.go/ds/walker"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/blockgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/seatmanager"
	iotago "github.com/iotaledger/iota.go/v4"
)

// region Gadget ///////////////////////////////////////////////////////////////////////////////////////////////////////

type Gadget struct {
	events *blockgadget.Events

	seatManager  seatmanager.SeatManager
	blockCache   *blocks.Blocks
	errorHandler func(error)

	optsAcceptanceThreshold               float64
	optsConfirmationThreshold             float64
	optsConfirmationRatificationThreshold iotago.SlotIndex

	module.Module
}

func NewProvider(opts ...options.Option[Gadget]) module.Provider[*engine.Engine, blockgadget.Gadget] {
	return module.Provide(func(e *engine.Engine) blockgadget.Gadget {
		g := New(e.NewSubModule("ThresholdBlockGadget"), e.BlockCache, e.SybilProtection.SeatManager(), e.ErrorHandler("gadget"), opts...)

		e.ConstructedEvent().OnTrigger(func() {
			wp := e.Workers.CreatePool("ThresholdBlockGadget", workerpool.WithWorkerCount(1))
			e.Events.Booker.BlockBooked.Hook(g.TrackWitnessWeight, event.WithWorkerPool(wp))

			e.Events.BlockGadget.LinkTo(g.events)

			g.InitializedEvent().Trigger()
		})

		return g
	})
}

func New(subModule module.Module, blockCache *blocks.Blocks, seatManager seatmanager.SeatManager, errorHandler func(error), opts ...options.Option[Gadget]) *Gadget {
	return options.Apply(&Gadget{
		Module:       subModule,
		events:       blockgadget.NewEvents(),
		seatManager:  seatManager,
		blockCache:   blockCache,
		errorHandler: errorHandler,

		optsAcceptanceThreshold:               0.67,
		optsConfirmationThreshold:             0.67,
		optsConfirmationRatificationThreshold: 2,
	}, opts, func(g *Gadget) {
		g.ShutdownEvent().OnTrigger(func() {
			g.StoppedEvent().Trigger()
		})

		g.ConstructedEvent().Trigger()
	})
}

func (g *Gadget) Events() *blockgadget.Events {
	return g.events
}

// Reset resets the component to a clean state as if it was created at the last commitment.
func (g *Gadget) Reset() {}

// propagate performs a breadth-first past cone walk starting at initialBlockIDs. evaluateFunc is called for every block visited
// and needs to return whether to continue the walk further.
func (g *Gadget) propagate(initialBlockIDs iotago.BlockIDs, evaluateFunc func(block *blocks.Block) bool) {
	walk := walker.New[iotago.BlockID](false).PushAll(initialBlockIDs...)
	for walk.HasNext() {
		blockID := walk.Next()
		block, exists := g.blockCache.Block(blockID)

		// If the block doesn't exist it is either in the process of being evicted (accepted or orphaned), or we should
		// find it as a root block.
		if !exists || block.IsRootBlock() {
			continue
		}

		if !evaluateFunc(block) {
			continue
		}

		walk.PushAll(block.Parents()...)
	}
}

func (g *Gadget) isCommitteeValidationBlock(block *blocks.Block) (seat account.SeatIndex, isValid bool) {
	if !block.IsValidationBlock() {
		return 0, false
	}

	committee, exists := g.seatManager.CommitteeInSlot(block.ID().Slot())
	if !exists {
		g.errorHandler(ierrors.Errorf("committee for slot %d does not exist", block.ID().Slot()))

		return 0, false
	}

	// Only accept blocks for issuers that are part of the committee.
	return committee.GetSeat(block.IssuerID())
}

func anyChildInSet(block *blocks.Block, set ds.Set[iotago.BlockID]) bool {
	if err := block.Children().ForEach(func(child *blocks.Block) error {
		if set.Has(child.ID()) {
			return ierrors.New("stop iteration: child in set")
		}

		// We continue the iteration if the child is not in the set.
		return nil
	}); err != nil {
		// There can only be the stop iteration error -> there's a child in the set.
		return true
	}

	return false
}
