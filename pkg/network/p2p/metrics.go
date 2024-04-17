package p2p

import (
	"sync/atomic"
)

// P2PMetrics defines P2P metrics over the entire runtime of the node.
type P2PMetrics struct {
	// The number of total received blocks.
	IncomingBlocks atomic.Uint32
	// The number of received blocks which are new.
	IncomingNewBlocks atomic.Uint32
	// The number of sent blocks.
	OutgoingBlocks atomic.Uint32
}
