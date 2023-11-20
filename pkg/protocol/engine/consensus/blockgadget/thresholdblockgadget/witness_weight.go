package thresholdblockgadget

import (
	"github.com/iotaledger/hive.go/ds"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/votes"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (g *Gadget) TrackWitnessWeight(votingBlock *blocks.Block) {
	// Only track witness weight for issuers that are part of the committee.
	seat, isValid := g.isCommitteeValidationBlock(votingBlock)
	if !isValid {
		return
	}

	var toPreAccept []*blocks.Block
	toPreAcceptByID := ds.NewSet[iotago.BlockID]()

	var toPreConfirm []*blocks.Block
	toPreConfirmByID := ds.NewSet[iotago.BlockID]()

	process := func(block *blocks.Block) bool {
		shouldPreAccept, shouldPreConfirm := g.shouldPreAcceptAndPreConfirm(block)

		var propagateFurther bool
		if !block.IsPreAccepted() && (shouldPreAccept || anyChildInSet(block, toPreAcceptByID)) {
			toPreAccept = append([]*blocks.Block{block}, toPreAccept...)
			toPreAcceptByID.Add(block.ID())
			propagateFurther = true
		}

		if !block.IsPreConfirmed() && (shouldPreConfirm || anyChildInSet(block, toPreConfirmByID)) {
			toPreConfirm = append([]*blocks.Block{block}, toPreConfirm...)
			toPreConfirmByID.Add(block.ID())
			propagateFurther = true
		}

		return propagateFurther
	}

	// Add the witness to the voting block itself as each block carries a vote for itself.
	if votingBlock.AddWitness(seat) {
		process(votingBlock)
	}

	evaluateFunc := func(block *blocks.Block) bool {
		// Propagate further if the witness is new.
		propagateFurther := block.AddWitness(seat)

		if process(block) {
			// Even if the witness is not new, we should preAccept or preConfirm this block just now (potentially due to OnlineCommittee changes).
			// That means, we should check its parents to ensure monotonicity (at least for preAcceptance):
			//  1. If they are not yet preAccepted, we will add them to the stack and preAccept them.
			//  2. If they are preAccepted, we will stop the walk.
			propagateFurther = true
		}

		return propagateFurther
	}

	g.propagate(votingBlock.Parents(), evaluateFunc)

	var acceptanceRatifierWeights []*blocks.Block
	for _, block := range toPreAccept {
		if block.SetPreAccepted() {
			g.events.BlockPreAccepted.Trigger(block)
			acceptanceRatifierWeights = append(acceptanceRatifierWeights, block)
		}
	}

	var confirmationRatifierWeights []*blocks.Block
	for _, block := range toPreConfirm {
		if block.SetPreConfirmed() {
			g.events.BlockPreConfirmed.Trigger(block)
			confirmationRatifierWeights = append(confirmationRatifierWeights, block)
		}
	}

	for _, block := range acceptanceRatifierWeights {
		g.trackAcceptanceRatifierWeight(block)
	}

	for _, block := range confirmationRatifierWeights {
		g.trackConfirmationRatifierWeight(block)
	}
}

func (g *Gadget) shouldPreAcceptAndPreConfirm(block *blocks.Block) (preAccept bool, preConfirm bool) {
	committeeTotalSeats := g.seatManager.SeatCountInSlot(block.ID().Slot())
	blockSeats := len(block.Witnesses())

	onlineCommitteeTotalSeats := g.seatManager.OnlineCommittee().Size()
	blockSeatsOnline := len(block.Witnesses())

	if votes.IsThresholdReached(blockSeats, committeeTotalSeats, g.optsConfirmationThreshold) {
		return true, true
	} else if votes.IsThresholdReached(blockSeatsOnline, onlineCommitteeTotalSeats, g.optsAcceptanceThreshold) {
		return true, false
	}

	return false, false
}
