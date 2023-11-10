package drr

import (
	"container/ring"
	"math"
	"time"

	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"

	iotago "github.com/iotaledger/iota.go/v4"
)

// region BufferQueue /////////////////////////////////////////////////////////////////////////////////////////////

// BufferQueue represents a buffer of IssuerQueue.
type BufferQueue struct {
	activeIssuers *shrinkingmap.ShrinkingMap[iotago.AccountID, *ring.Ring]
	ring          *ring.Ring
	// size is the number of blocks in the buffer.
	size int

	tokenBucket      float64
	lastScheduleTime time.Time

	blockChan chan *blocks.Block
}

// NewBufferQueue returns a new BufferQueue.
func NewBufferQueue() *BufferQueue {
	return &BufferQueue{
		activeIssuers:    shrinkingmap.New[iotago.AccountID, *ring.Ring](),
		ring:             nil,
		lastScheduleTime: time.Now(),
		blockChan:        make(chan *blocks.Block, 1),
	}
}

// NumActiveIssuers returns the number of active issuers in b.
func (b *BufferQueue) NumActiveIssuers() int {
	return b.activeIssuers.Size()
}

// Size returns the total number of blocks in BufferQueue.
func (b *BufferQueue) Size() int {
	return b.size
}

// IssuerQueue returns the queue for the corresponding issuer.
func (b *BufferQueue) IssuerQueue(issuerID iotago.AccountID) *IssuerQueue {
	element, ok := b.activeIssuers.Get(issuerID)
	if !ok {
		return nil
	}
	issuerQueue, isIQ := element.Value.(*IssuerQueue)
	if !isIQ {
		return nil
	}

	return issuerQueue
}

// IssuerQueueWork returns the total WorkScore of block in the queue for the corresponding issuer.
func (b *BufferQueue) IssuerQueueWork(issuerID iotago.AccountID) iotago.WorkScore {
	issuerQueue := b.IssuerQueue(issuerID)
	if issuerQueue == nil {
		return 0
	}

	return issuerQueue.Work()
}

// IssuerQueueSize returns the number of blocks in the queue for the corresponding issuer.
func (b *BufferQueue) IssuerQueueBlockCount(issuerID iotago.AccountID) int {
	issuerQueue := b.IssuerQueue(issuerID)
	if issuerQueue == nil {
		return 0
	}

	return issuerQueue.Size()
}

func (b *BufferQueue) CreateIssuerQueue(issuerID iotago.AccountID) *IssuerQueue {
	issuerQueue := NewIssuerQueue(issuerID)
	b.activeIssuers.Set(issuerID, b.ringInsert(issuerQueue))

	return issuerQueue
}

func (b *BufferQueue) GetOrCreateIssuerQueue(issuerID iotago.AccountID) *IssuerQueue {
	element, issuerActive := b.activeIssuers.Get(issuerID)
	if !issuerActive {
		// create new issuer queue
		return b.CreateIssuerQueue(issuerID)
	}
	issuerQueue, isIQ := element.Value.(*IssuerQueue)
	if !isIQ {
		panic("buffer contains elements that are not issuer queues")
	}

	return issuerQueue
}

// RemoveIssuerQueue removes all blocks (submitted and ready) for the given issuer and deletes the issuer queue.
func (b *BufferQueue) RemoveIssuerQueue(issuerID iotago.AccountID) {
	element, ok := b.activeIssuers.Get(issuerID)
	if !ok {
		return
	}
	issuerQueue, isIQ := element.Value.(*IssuerQueue)
	if !isIQ {
		panic("buffer contains elements that are not issuer queues")
	}
	b.size -= issuerQueue.Size()

	b.ringRemove(element)
	b.activeIssuers.Delete(issuerID)
}

// RemoveIssuerQueueIfEmpty removes all blocks (submitted and ready) for the given issuer and deletes the issuer queue if it is empty.
func (b *BufferQueue) RemoveIssuerQueueIfEmpty(issuerID iotago.AccountID) {
	element, ok := b.activeIssuers.Get(issuerID)
	if !ok {
		return
	}
	issuerQueue, isIQ := element.Value.(*IssuerQueue)
	if !isIQ {
		panic("buffer contains elements that are not issuer queues")
	}

	if issuerQueue.Size() == 0 {
		b.ringRemove(element)
		b.activeIssuers.Delete(issuerID)
	}
}

// Submit submits a block. Return blocks dropped from the scheduler to make room for the submitted block.
// The submitted block can also be returned as dropped if the issuer does not have enough mana.
func (b *BufferQueue) Submit(blk *blocks.Block, issuerQueue *IssuerQueue, quantumFunc func(iotago.AccountID) Deficit, maxBuffer int) ([]*blocks.Block, bool) {
	// first we submit the block, and if it turns out that the issuer doesn't have enough bandwidth to submit, it will be removed by dropTail
	if !issuerQueue.Submit(blk) {
		return nil, false
	}

	b.size++

	// if max buffer size exceeded, drop from tail of the longest mana-scaled queue
	if b.Size() > maxBuffer {
		return b.dropTail(quantumFunc, maxBuffer), true
	}

	return nil, true
}

// Unsubmit removes a block from the submitted blocks.
// If that block is already marked as ready, Unsubmit has no effect.
func (b *BufferQueue) Unsubmit(block *blocks.Block) bool {
	issuerID := block.ProtocolBlock().Header.IssuerID

	issuerQueue := b.IssuerQueue(issuerID)
	if issuerQueue == nil {
		return false
	}

	if !issuerQueue.Unsubmit(block) {
		return false
	}

	b.size--

	return true
}

// Ready marks a previously submitted block as ready to be scheduled.
func (b *BufferQueue) Ready(block *blocks.Block) bool {
	issuerQueue := b.IssuerQueue(block.ProtocolBlock().Header.IssuerID)
	if issuerQueue == nil {
		return false
	}

	return issuerQueue.Ready(block)
}

// ReadyBlocksCount returns the number of ready blocks in the buffer.
func (b *BufferQueue) ReadyBlocksCount() (readyBlocksCount int) {
	start := b.Current()
	if start == nil {
		return
	}

	for q := start; ; {
		readyBlocksCount += q.inbox.Len()
		q = b.Next()
		if q == start {
			break
		}
	}

	return
}

// TotalBlocksCount returns the number of blocks in the buffer.
func (b *BufferQueue) TotalBlocksCount() (blocksCount int) {
	start := b.Current()
	if start == nil {
		return
	}
	for q := start; ; {
		blocksCount += q.inbox.Len()
		blocksCount += q.submitted.Size()
		q = b.Next()
		if q == start {
			break
		}
	}

	return
}

// Next returns the next IssuerQueue in round-robin order.
func (b *BufferQueue) Next() *IssuerQueue {
	if b.ring != nil {
		b.ring = b.ring.Next()
		if issuerQueue, isIQ := b.ring.Value.(*IssuerQueue); isIQ {
			return issuerQueue
		}
	}

	return nil
}

// Current returns the current IssuerQueue in round-robin order.
func (b *BufferQueue) Current() *IssuerQueue {
	if b.ring == nil {
		return nil
	}
	if issuerQueue, isIQ := b.ring.Value.(*IssuerQueue); isIQ {
		return issuerQueue
	}

	return nil
}

// PopFront removes the first ready block from the queue of the current issuer.
func (b *BufferQueue) PopFront() *blocks.Block {
	q := b.Current()
	if q == nil {
		return nil
	}

	block := q.PopFront()
	if block == nil {
		return nil

	}

	b.size--

	return block
}

// IssuerIDs returns the issuerIDs of all issuers.
func (b *BufferQueue) IssuerIDs() []iotago.AccountID {
	var issuerIDs []iotago.AccountID
	start := b.Current()
	if start == nil {
		return nil
	}
	for q := start; ; {
		issuerIDs = append(issuerIDs, q.IssuerID())
		q = b.Next()
		if q == start {
			break
		}
	}

	return issuerIDs
}

func (b *BufferQueue) dropTail(quantumFunc func(iotago.AccountID) Deficit, maxBuffer int) (droppedBlocks []*blocks.Block) {
	// remove as many blocks as necessary to stay within max buffer size
	for b.Size() > maxBuffer {
		// find the longest mana-scaled queue
		maxIssuerID := b.longestQueueIssuerID(quantumFunc)
		if longestQueue := b.IssuerQueue(maxIssuerID); longestQueue != nil {
			if tail := longestQueue.RemoveTail(); tail != nil {
				b.size--
				droppedBlocks = append(droppedBlocks, tail)
			}
		}
	}

	return droppedBlocks
}

func (b *BufferQueue) longestQueueIssuerID(quantumFunc func(iotago.AccountID) Deficit) iotago.AccountID {
	start := b.Current()
	ringStart := b.ring
	maxScale := math.Inf(-1)
	var maxIssuerID iotago.AccountID
	for q := start; ; {
		if issuerQuantum := quantumFunc(q.IssuerID()); issuerQuantum > 0 {
			if scale := float64(q.Work()) / float64(issuerQuantum); scale > maxScale {
				maxScale = scale
				maxIssuerID = q.IssuerID()
			}
		} else if q.Size() > 0 {
			// if the issuer has no quantum, then this is the max queue size
			maxIssuerID = q.IssuerID()
			b.ring = ringStart

			break
		}
		q = b.Next()
		if q == start {
			break
		}
	}

	return maxIssuerID
}

func (b *BufferQueue) ringRemove(r *ring.Ring) {
	n := b.ring.Next()
	if r == b.ring {
		if n == b.ring {
			b.ring = nil
			return
		}
		b.ring = n
	}
	r.Prev().Link(n)
}

func (b *BufferQueue) ringInsert(v interface{}) *ring.Ring {
	p := ring.New(1)
	p.Value = v
	if b.ring == nil {
		b.ring = p
		return p
	}

	return p.Link(b.ring)
}

func (b *BufferQueue) waitTime(rate float64, block *blocks.Block) time.Duration {
	tokensRequired := float64(block.WorkScore()) - (b.tokenBucket + rate*time.Since(b.lastScheduleTime).Seconds())

	return lo.Max(0, time.Duration(tokensRequired/rate))
}

func (b *BufferQueue) updateTokenBucket(rate float64, tokenBucketSize float64) {
	b.tokenBucket = lo.Min(
		tokenBucketSize,
		b.tokenBucket+rate*time.Since(b.lastScheduleTime).Seconds(),
	)
	b.lastScheduleTime = time.Now()
}

func (b *BufferQueue) deductTokens(tokens float64) {
	b.tokenBucket -= tokens
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
