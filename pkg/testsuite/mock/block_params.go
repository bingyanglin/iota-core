package mock

import (
	"time"

	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/wallet"
)

type BlockHeaderParams struct {
	ParentsCount        int
	References          model.ParentReferences
	SlotCommitment      *iotago.Commitment
	LatestFinalizedSlot *iotago.SlotIndex
	IssuingTime         *time.Time
	ProtocolVersion     *iotago.Version
	Issuer              wallet.Account
	ReferenceValidation bool
}
type BasicBlockParams struct {
	BlockHeader *BlockHeaderParams
	Payload     iotago.Payload
}
type ValidationBlockParams struct {
	BlockHeader             *BlockHeaderParams
	HighestSupportedVersion *iotago.Version
	ProtocolParametersHash  *iotago.Identifier
}

func NewValidationBlockParams() *ValidationBlockParams {
	return &ValidationBlockParams{
		BlockHeader: &BlockHeaderParams{},
	}
}

func WithParentsCount(parentsCount int) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.ParentsCount = parentsCount
	}
}

func WithStrongParents(blockIDs ...iotago.BlockID) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		if builder.References == nil {
			builder.References = make(model.ParentReferences)
		}

		builder.References[iotago.StrongParentType] = blockIDs
	}
}
func WithWeakParents(blockIDs ...iotago.BlockID) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		if builder.References == nil {
			builder.References = make(model.ParentReferences)
		}

		builder.References[iotago.WeakParentType] = blockIDs
	}
}

func WithShallowLikeParents(blockIDs ...iotago.BlockID) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		if builder.References == nil {
			builder.References = make(model.ParentReferences)
		}

		builder.References[iotago.ShallowLikeParentType] = blockIDs
	}
}

func WithSlotCommitment(commitment *iotago.Commitment) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.SlotCommitment = commitment
	}
}

func WithLatestFinalizedSlot(commitmentIndex iotago.SlotIndex) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.LatestFinalizedSlot = &commitmentIndex
	}
}

func WithIssuingTime(issuingTime time.Time) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.IssuingTime = &issuingTime
	}
}

func WithProtocolVersion(version iotago.Version) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.ProtocolVersion = &version
	}
}

func WithIssuer(issuer wallet.Account) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.Issuer = issuer
	}
}

func WithReferenceValidation(referenceValidation bool) func(builder *BlockHeaderParams) {
	return func(builder *BlockHeaderParams) {
		builder.ReferenceValidation = referenceValidation
	}
}

func WithValidationBlockHeaderOptions(opts ...options.Option[BlockHeaderParams]) func(builder *ValidationBlockParams) {
	return func(builder *ValidationBlockParams) {
		builder.BlockHeader = options.Apply(&BlockHeaderParams{}, opts)
	}
}

func WithBasicBlockHeader(opts ...options.Option[BlockHeaderParams]) func(builder *BasicBlockParams) {
	return func(builder *BasicBlockParams) {
		builder.BlockHeader = options.Apply(&BlockHeaderParams{}, opts)
	}
}

func WithPayload(payload iotago.Payload) func(builder *BasicBlockParams) {
	return func(builder *BasicBlockParams) {
		builder.Payload = payload
	}
}

func WithHighestSupportedVersion(highestSupportedVersion iotago.Version) func(builder *ValidationBlockParams) {
	return func(builder *ValidationBlockParams) {
		builder.HighestSupportedVersion = &highestSupportedVersion
	}
}

func WithProtocolParametersHash(protocolParametersHash iotago.Identifier) func(builder *ValidationBlockParams) {
	return func(builder *ValidationBlockParams) {
		builder.ProtocolParametersHash = &protocolParametersHash
	}
}
