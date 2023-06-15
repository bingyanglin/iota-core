package engine

import (
	"github.com/iotaledger/hive.go/core/eventticker"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blockdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/booker"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/clock"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/blockgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/slotgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/eviction"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/sybilprotection"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/tipselection"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Events struct {
	BlockProcessed *event.Event1[iotago.BlockID]

	EvictionState   *eviction.Events
	Filter          *filter.Events
	BlockRequester  *eventticker.Events[iotago.SlotIndex, iotago.BlockID]
	TipManager      *tipselection.Events
	BlockDAG        *blockdag.Events
	Booker          *booker.Events
	Clock           *clock.Events
	BlockGadget     *blockgadget.Events
	SlotGadget      *slotgadget.Events
	Notarization    *notarization.Events
	ConflictDAG     *conflictdag.Events[iotago.TransactionID, iotago.OutputID]
	SybilProtection *sybilprotection.Events

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockProcessed:  event.New1[iotago.BlockID](),
		EvictionState:   eviction.NewEvents(),
		Filter:          filter.NewEvents(),
		BlockRequester:  eventticker.NewEvents[iotago.SlotIndex, iotago.BlockID](),
		TipManager:      tipselection.NewEvents(),
		BlockDAG:        blockdag.NewEvents(),
		Booker:          booker.NewEvents(),
		Clock:           clock.NewEvents(),
		BlockGadget:     blockgadget.NewEvents(),
		SlotGadget:      slotgadget.NewEvents(),
		Notarization:    notarization.NewEvents(),
		ConflictDAG:     conflictdag.NewEvents[iotago.TransactionID, iotago.OutputID](),
		SybilProtection: sybilprotection.NewEvents(),
	}
})
