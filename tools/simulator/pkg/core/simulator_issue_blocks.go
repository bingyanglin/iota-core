package core

import (
	"context"

	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *Simulator) limitParentsCountInBlockOptions(blockOpts []options.Option[mock.BlockHeaderParams], maxCount int) []options.Option[mock.BlockHeaderParams] {
	params := options.Apply(&mock.BlockHeaderParams{}, blockOpts)
	if len(params.References[iotago.StrongParentType]) > maxCount {
		blockOpts = append(blockOpts, mock.WithStrongParents(params.References[iotago.StrongParentType][:maxCount]...))
	}
	if len(params.References[iotago.WeakParentType]) > maxCount {
		blockOpts = append(blockOpts, mock.WithWeakParents(params.References[iotago.WeakParentType][:maxCount]...))
	}
	if len(params.References[iotago.ShallowLikeParentType]) > maxCount {
		blockOpts = append(blockOpts, mock.WithShallowLikeParents(params.References[iotago.ShallowLikeParentType][:maxCount]...))
	}

	return blockOpts
}

func (t *Simulator) IssueValidationBlockWithOptions(node *mock.Node, blockOpts ...options.Option[mock.ValidationBlockParams]) *blocks.Block {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	block := node.IssueValidationBlock(context.Background(), blockOpts...)

	return block
}

func (t *Simulator) SlotsForEpoch(epoch iotago.EpochIndex) []iotago.SlotIndex {
	slotsPerEpoch := t.API.TimeProvider().EpochDurationSlots()

	slots := make([]iotago.SlotIndex, 0, slotsPerEpoch)
	epochStart := t.API.TimeProvider().EpochStart(epoch)
	for i := epochStart; i < epochStart+slotsPerEpoch; i++ {
		if i == 0 {
			continue
		}
		slots = append(slots, i)
	}

	return slots
}
