package blockdag

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
)

// Events is a collection of Tangle related Events.
type Events struct {
	// BlockAttached is triggered when a previously unknown Block is attached.
	BlockAttached *event.Event1[*blocks.Block]

	// BlockSolid is triggered when a Block becomes solid (its entire past cone is known and solid).
	BlockSolid *event.Event1[*blocks.Block]

	// BlockMissing is triggered when a referenced Block was not attached, yet.
	BlockMissing *event.Event1[*blocks.Block]

	// MissingBlockAttached is triggered when a previously missing Block was attached.
	MissingBlockAttached *event.Event1[*blocks.Block]

	// BlockInvalid is triggered when a Block is found to be invalid.
	BlockInvalid *event.Event2[*blocks.Block, error]
	// TODO: hook this up in engine

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockAttached:        event.New1[*blocks.Block](),
		BlockSolid:           event.New1[*blocks.Block](),
		BlockMissing:         event.New1[*blocks.Block](),
		MissingBlockAttached: event.New1[*blocks.Block](),
		BlockInvalid:         event.New2[*blocks.Block, error](),
	}
})
