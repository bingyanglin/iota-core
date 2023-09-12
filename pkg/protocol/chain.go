package protocol

import (
	"github.com/iotaledger/hive.go/ds/reactive"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Chain struct {
	ForkingPoint             reactive.Variable[*Commitment]
	LatestCommitment         reactive.Variable[*Commitment]
	LatestAttestedCommitment reactive.Variable[*Commitment]
	LatestVerifiedCommitment reactive.Variable[*Commitment]
	ClaimedWeight            reactive.Variable[uint64]
	AttestedWeight           reactive.Variable[uint64]
	VerifiedWeight           reactive.Variable[uint64]
	SyncThreshold            reactive.Variable[iotago.SlotIndex]
	WarpSyncThreshold        reactive.Variable[iotago.SlotIndex]
	RequestAttestations      reactive.Variable[bool]
	Engine                   *chainEngine
	IsSolid                  reactive.Event
	IsEvicted                reactive.Event

	commitments *shrinkingmap.ShrinkingMap[iotago.SlotIndex, *Commitment]
}

func NewChain() *Chain {
	c := &Chain{
		ForkingPoint:             reactive.NewVariable[*Commitment](),
		LatestCommitment:         reactive.NewVariable[*Commitment](),
		LatestAttestedCommitment: reactive.NewVariable[*Commitment](),
		LatestVerifiedCommitment: reactive.NewVariable[*Commitment](),
		RequestAttestations:      reactive.NewVariable[bool](),
		IsEvicted:                reactive.NewEvent(),

		commitments: shrinkingmap.New[iotago.SlotIndex, *Commitment](),
	}

	c.Engine = newChainEngine(c)

	c.ClaimedWeight = reactive.NewDerivedVariable(cumulativeWeight, c.LatestCommitment)
	c.AttestedWeight = reactive.NewDerivedVariable(cumulativeWeight, c.LatestAttestedCommitment)
	c.VerifiedWeight = reactive.NewDerivedVariable(cumulativeWeight, c.LatestVerifiedCommitment)

	c.WarpSyncThreshold = reactive.NewDerivedVariable[iotago.SlotIndex](func(latestCommitment *Commitment) iotago.SlotIndex {
		if latestCommitment == nil || latestCommitment.Index() < WarpSyncOffset {
			return 0
		}

		return latestCommitment.Index() - WarpSyncOffset
	}, c.LatestCommitment)

	c.SyncThreshold = reactive.NewDerivedVariable[iotago.SlotIndex](func(latestVerifiedCommitment *Commitment) iotago.SlotIndex {
		if latestVerifiedCommitment == nil {
			return SyncWindow + 1
		}

		return latestVerifiedCommitment.Index() + SyncWindow + 1
	}, c.LatestVerifiedCommitment)

	return c
}

func (c *Chain) Commitment(index iotago.SlotIndex) (commitment *Commitment, exists bool) {
	for currentChain := c; currentChain != nil; {
		switch forkingPoint := currentChain.ForkingPoint.Get(); {
		case forkingPoint == nil:
			return nil, false // this should never happen, but we can handle it gracefully anyway
		case forkingPoint.Index() == index:
			return forkingPoint, true
		case index > forkingPoint.Index():
			return currentChain.commitments.Get(index)
		default:
			parent := forkingPoint.Parent.Get()
			if parent == nil {
				return nil, false
			}

			currentChain = parent.Chain.Get()
		}
	}

	return nil, false
}

func (c *Chain) InSyncRange(index iotago.SlotIndex) bool {
	if latestVerifiedCommitment := c.LatestVerifiedCommitment.Get(); latestVerifiedCommitment != nil {
		return index > c.LatestVerifiedCommitment.Get().Index() && index < c.SyncThreshold.Get()
	}

	return false
}

func (c *Chain) registerCommitment(commitment *Commitment) {
	c.commitments.Set(commitment.Index(), commitment)

	// maxCommitment returns the Commitment object with the higher index.
	maxCommitment := func(other *Commitment) *Commitment {
		if commitment == nil || other != nil && other.Index() >= commitment.Index() {
			return other
		}

		return commitment
	}

	c.LatestCommitment.Compute(maxCommitment)

	unsubscribe := lo.Batch(
		commitment.IsAttested.OnTrigger(func() { c.LatestAttestedCommitment.Compute(maxCommitment) }),
		commitment.IsVerified.OnTrigger(func() { c.LatestVerifiedCommitment.Compute(maxCommitment) }),
	)

	// unsubscribe and unregister the commitment when it changes its chain
	commitment.Chain.OnUpdateOnce(func(_, _ *Chain) {
		unsubscribe()

		c.unregisterCommitment(commitment)
	}, func(_, newChain *Chain) bool { return newChain != c })
}

func (c *Chain) unregisterCommitment(commitment *Commitment) {
	c.commitments.Delete(commitment.Index())

	resetToParent := func(latestCommitment *Commitment) *Commitment {
		if commitment.Index() > latestCommitment.Index() {
			return latestCommitment
		}

		return commitment.Parent.Get()
	}

	c.LatestCommitment.Compute(resetToParent)
	c.LatestAttestedCommitment.Compute(resetToParent)
	c.LatestVerifiedCommitment.Compute(resetToParent)
}

type chainEngine struct {
	reactive.Variable[*engine.Engine]

	parentEngine reactive.Variable[*engine.Engine]

	spawnedEngine reactive.Variable[*engine.Engine]

	instantiate reactive.Variable[bool]
}

func newChainEngine(chain *Chain) *chainEngine {
	e := &chainEngine{
		parentEngine:  reactive.NewVariable[*engine.Engine](),
		instantiate:   reactive.NewVariable[bool](),
		spawnedEngine: reactive.NewVariable[*engine.Engine](),
	}

	chain.ForkingPoint.OnUpdateWithContext(func(_, forkingPoint *Commitment, withinContext func(subscriptionFactory func() (unsubscribe func()))) {
		withinContext(func() func() {
			return forkingPoint.Parent.OnUpdate(func(_, parent *Commitment) {
				withinContext(func() func() {
					return e.parentEngine.InheritFrom(parent.Engine)
				})
			})
		})
	})

	e.Variable = reactive.NewDerivedVariable2(func(spawnedEngine, parentEngine *engine.Engine) *engine.Engine {
		if spawnedEngine != nil {
			return spawnedEngine
		}

		return parentEngine
	}, e.spawnedEngine, e.parentEngine)

	return e
}

func cumulativeWeight(commitment *Commitment) uint64 {
	if commitment == nil {
		return 0
	}

	return commitment.CumulativeWeight()
}
