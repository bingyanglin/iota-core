package retainer

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

// BlockRetainerEvents is a collection of Retainer related BlockRetainerEvents.
type BlockRetainerEvents struct {
	// BlockRetained is triggered when a block is stored in the retainer.
	BlockRetained *event.Event1[*blocks.Block]

	event.Group[BlockRetainerEvents, *BlockRetainerEvents]
}

// NewBlockRetainerEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewBlockRetainerEvents = event.CreateGroupConstructor(func() (newEvents *BlockRetainerEvents) {
	return &BlockRetainerEvents{
		BlockRetained: event.New1[*blocks.Block](),
	}
})

// TransactionRetainerEvents is a collection of Retainer related TransactionRetainerEvents.
type TransactionRetainerEvents struct {
	// TransactionRetained is triggered when a transaction is stored in the retainer.
	TransactionRetained *event.Event1[iotago.TransactionID]

	event.Group[TransactionRetainerEvents, *TransactionRetainerEvents]
}

// NewTransactionRetainerEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewTransactionRetainerEvents = event.CreateGroupConstructor(func() (newEvents *TransactionRetainerEvents) {
	return &TransactionRetainerEvents{
		TransactionRetained: event.New1[iotago.TransactionID](),
	}
})
