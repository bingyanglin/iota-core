package eviction

import (
	"github.com/iotaledger/hive.go/runtime/event"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Events struct {
	SlotEvicted *event.Event1[iotago.SlotIndex]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (self *Events) {
	return &Events{
		SlotEvicted: event.New1[iotago.SlotIndex](),
	}
})