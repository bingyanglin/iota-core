package protocol

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/protocols/core"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (p *Protocol) runNetworkProtocol() {
	p.networkProtocol = core.NewProtocol(p.dispatcher, p.Workers.CreatePool("NetworkProtocol"), p.API()) // Use max amount of workers for networking
	p.Events.Network.LinkTo(p.networkProtocol.Events)

	// TODO: the distinction between different worker pools as it is right now does not make much sense:
	//  instead we should distinguish between wp for handling requested/requesting and wp for handling received stuff

	wpBlocks := p.Workers.CreatePool("NetworkEvents.Blocks") // Use max amount of workers for sending, receiving and requesting blocks

	p.Events.Network.BlockReceived.Hook(func(block *model.Block, id network.PeerID) {
		if err := p.ProcessBlock(block, id); err != nil {
			p.ErrorHandler()(err)
		}
	}, event.WithWorkerPool(wpBlocks))

	p.Events.Network.BlockRequestReceived.Hook(func(blockID iotago.BlockID, id network.PeerID) {
		if block, exists := p.MainEngineInstance().Block(blockID); exists {
			p.networkProtocol.SendBlock(block, id)
		}
	}, event.WithWorkerPool(wpBlocks))

	p.Events.Engine.BlockRequester.Tick.Hook(func(blockID iotago.BlockID) {
		p.networkProtocol.RequestBlock(blockID)
	}, event.WithWorkerPool(wpBlocks))

	p.Events.Engine.Booker.BlockBooked.Hook(func(block *blocks.Block) {
		p.networkProtocol.SendBlock(block.ModelBlock())
	}, event.WithWorkerPool(wpBlocks))

	wpCommitments := p.Workers.CreatePool("NetworkEvents.SlotCommitments")

	p.Events.Network.SlotCommitmentRequestReceived.Hook(func(commitmentID iotago.CommitmentID, source network.PeerID) {
		// when we receive a commitment request, do not look it up in the ChainManager but in the storage, else we might answer with commitments we did not issue ourselves and for which we cannot provide attestations
		if requestedCommitment, err := p.MainEngineInstance().Storage.Commitments().Load(commitmentID.Index()); err == nil && requestedCommitment.ID() == commitmentID {
			p.networkProtocol.SendSlotCommitment(requestedCommitment, source)
		}
	}, event.WithWorkerPool(wpCommitments))

	p.Events.Network.SlotCommitmentReceived.Hook(func(commitment *model.Commitment, source network.PeerID) {
		p.ChainManager.ProcessCommitmentFromSource(commitment, source)
	}, event.WithWorkerPool(wpCommitments))

	p.Events.ChainManager.RequestCommitment.Hook(func(commitmentID iotago.CommitmentID) {
		p.networkProtocol.RequestSlotCommitment(commitmentID)
	}, event.WithWorkerPool(wpCommitments))

	wpAttestations := p.Workers.CreatePool("NetworkEvents.Attestations", 1) // Using just 1 worker to avoid contention

	p.Events.Network.AttestationsRequestReceived.Hook(p.processAttestationsRequest, event.WithWorkerPool(wpAttestations))
}