package core

import (
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

type NodeState struct {
	snapshotImported                 *bool
	protocolParameters               *iotago.ProtocolParameters
	latestCommitment                 *iotago.Commitment
	latestCommitmentSlot             *iotago.SlotIndex
	latestCommitmentCumulativeWeight *uint64
	latestFinalizedSlot              *iotago.SlotIndex
	chainID                          *iotago.CommitmentID

	sybilProtectionCommitteeEpoch  *iotago.EpochIndex
	sybilProtectionCommittee       *[]iotago.AccountID
	sybilProtectionOnlineCommittee *[]account.SeatIndex
	sybilProtectionCandidatesEpoch *iotago.EpochIndex
	sybilProtectionCandidates      *[]iotago.AccountID

	storageCommitments      *[]*iotago.Commitment
	storageCommitmentAtSlot *iotago.SlotIndex

	storageRootBlocks *[]*blocks.Block
	activeRootBlocks  *[]*blocks.Block

	evictedSlot *iotago.SlotIndex

	chainManagerSolid *bool
}

func WithSnapshotImported(snapshotImported bool) options.Option[NodeState] {
	return func(state *NodeState) {
		state.snapshotImported = &snapshotImported
	}
}

func WithProtocolParameters(protocolParameters iotago.ProtocolParameters) options.Option[NodeState] {
	return func(state *NodeState) {
		state.protocolParameters = &protocolParameters
	}
}

func WithLatestCommitment(commitment *iotago.Commitment) options.Option[NodeState] {
	return func(state *NodeState) {
		state.latestCommitment = commitment
	}
}

func WithLatestCommitmentSlotIndex(slot iotago.SlotIndex) options.Option[NodeState] {
	return func(state *NodeState) {
		state.latestCommitmentSlot = &slot
	}
}

func WithEqualStoredCommitmentAtIndex(slot iotago.SlotIndex) options.Option[NodeState] {
	return func(state *NodeState) {
		state.storageCommitmentAtSlot = &slot
	}
}

func WithLatestCommitmentCumulativeWeight(cumulativeWeight uint64) options.Option[NodeState] {
	return func(state *NodeState) {
		state.latestCommitmentCumulativeWeight = &cumulativeWeight
	}
}

func WithLatestFinalizedSlot(slot iotago.SlotIndex) options.Option[NodeState] {
	return func(state *NodeState) {
		state.latestFinalizedSlot = &slot
	}
}

func WithChainID(chainID iotago.CommitmentID) options.Option[NodeState] {
	return func(state *NodeState) {
		state.chainID = &chainID
	}
}

func WithSybilProtectionCommittee(epoch iotago.EpochIndex, committee []iotago.AccountID) options.Option[NodeState] {
	return func(state *NodeState) {
		state.sybilProtectionCommitteeEpoch = &epoch
		state.sybilProtectionCommittee = &committee
	}
}

func WithSybilProtectionOnlineCommittee(committee ...account.SeatIndex) options.Option[NodeState] {
	return func(state *NodeState) {
		state.sybilProtectionOnlineCommittee = &committee
	}
}

func WithSybilProtectionCandidates(epoch iotago.EpochIndex, candidates []iotago.AccountID) options.Option[NodeState] {
	return func(state *NodeState) {
		state.sybilProtectionCandidatesEpoch = &epoch
		state.sybilProtectionCandidates = &candidates
	}
}

func WithStorageCommitments(commitments []*iotago.Commitment) options.Option[NodeState] {
	return func(state *NodeState) {
		state.storageCommitments = &commitments
	}
}

func WithStorageRootBlocks(blocks []*blocks.Block) options.Option[NodeState] {
	return func(state *NodeState) {
		state.storageRootBlocks = &blocks
	}
}

func WithActiveRootBlocks(blocks []*blocks.Block) options.Option[NodeState] {
	return func(state *NodeState) {
		state.activeRootBlocks = &blocks
	}
}

func WithEvictedSlot(slot iotago.SlotIndex) options.Option[NodeState] {
	return func(state *NodeState) {
		state.evictedSlot = &slot
	}
}

func WithChainManagerIsSolid() options.Option[NodeState] {
	return func(state *NodeState) {
		solid := true
		state.chainManagerSolid = &solid
	}
}
