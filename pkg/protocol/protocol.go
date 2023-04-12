package protocol

import (
	"fmt"

	"github.com/iotaledger/hive.go/crypto/identity"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/database"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/network"
	"github.com/iotaledger/iota-core/pkg/network/protocols/core"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blockdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blockdag/inmemoryblockdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter/blockfilter"
	"github.com/iotaledger/iota-core/pkg/protocol/enginemanager"
	"github.com/iotaledger/iota-core/pkg/protocol/tipmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/tipmanager/trivialtipmanager"
	iotago "github.com/iotaledger/iota.go/v4"
)

// region Protocol /////////////////////////////////////////////////////////////////////////////////////////////////////

type Protocol struct {
	Events        *Events
	engineManager *enginemanager.EngineManager
	tipManager    tipmanager.TipManager

	Workers         *workerpool.Group
	dispatcher      network.Endpoint
	networkProtocol *core.Protocol
	api             iotago.API

	mainEngine *engine.Engine

	optsBaseDirectory    string
	optsSnapshotPath     string
	optsPruningThreshold uint64

	optsEngineOptions                 []options.Option[engine.Engine]
	optsStorageDatabaseManagerOptions []options.Option[database.Manager]

	optsFilterProvider     module.Provider[*engine.Engine, filter.Filter]
	optsBlockDAGProvider   module.Provider[*engine.Engine, blockdag.BlockDAG]
	optsTipManagerProvider module.Provider[*engine.Engine, tipmanager.TipManager]
}

func New(workers *workerpool.Group, dispatcher network.Endpoint, api iotago.API, opts ...options.Option[Protocol]) (protocol *Protocol) {
	return options.Apply(&Protocol{
		Events:                 NewEvents(),
		Workers:                workers,
		dispatcher:             dispatcher,
		api:                    api,
		optsFilterProvider:     blockfilter.NewProvider(),
		optsBlockDAGProvider:   inmemoryblockdag.NewProvider(),
		optsTipManagerProvider: trivialtipmanager.NewProvider(),

		optsBaseDirectory:    "",
		optsPruningThreshold: 6 * 60, // 1 hour given that slot duration is 10 seconds
	}, opts,
		(*Protocol).initNetworkEvents,
		(*Protocol).initEngineManager,
		(*Protocol).initTipManager,
	)
}

// Run runs the protocol.
func (p *Protocol) Run() {
	p.Events.Engine.LinkTo(p.mainEngine.Events)

	if err := p.mainEngine.Initialize(p.optsSnapshotPath); err != nil {
		panic(err)
	}

	// p.linkTo(p.mainEngine) -> CC and TipManager
	// TODO: why do we create a protocol only when running?
	// TODO: fill up protocol params
	p.networkProtocol = core.NewProtocol(p.dispatcher, p.Workers.CreatePool("NetworkProtocol"), p.api, p.SlotTimeProvider()) // Use max amount of workers for networking
	p.Events.Network.LinkTo(p.networkProtocol.Events)
}

func (p *Protocol) Shutdown() {
	if p.networkProtocol != nil {
		p.networkProtocol.Unregister()
	}

	p.mainEngine.Shutdown()

	p.Workers.Shutdown()
}

func (p *Protocol) initNetworkEvents() {
	wpBlocks := p.Workers.CreatePool("NetworkEvents.Blocks") // Use max amount of workers for sending, receiving and requesting blocks

	p.Events.Network.BlockReceived.Hook(func(block *model.Block, id identity.ID) {
		if err := p.ProcessBlock(block, id); err != nil {
			p.Events.Error.Trigger(err)
		}
	}, event.WithWorkerPool(wpBlocks))
	p.Events.Network.BlockRequestReceived.Hook(func(blockID iotago.BlockID, id identity.ID) {
		if block, exists := p.MainEngineInstance().Block(blockID); exists {
			p.networkProtocol.SendBlock(block.Block(), id)
		}
	}, event.WithWorkerPool(wpBlocks))
	p.Events.Engine.BlockRequester.Tick.Hook(func(blockID iotago.BlockID) {
		p.networkProtocol.RequestBlock(blockID)
	}, event.WithWorkerPool(wpBlocks))

	p.Events.Engine.BlockDAG.BlockSolid.Hook(func(block *blockdag.Block) {
		p.networkProtocol.SendBlock(block.Block())
	}, event.WithWorkerPool(wpBlocks))
}

func (p *Protocol) initEngineManager() {
	p.engineManager = enginemanager.New(
		p.Workers.CreateGroup("EngineManager"),
		p.optsBaseDirectory,
		DatabaseVersion,
		p.optsStorageDatabaseManagerOptions,
		p.optsEngineOptions,
		p.optsFilterProvider,
		p.optsBlockDAGProvider,
	)

	mainEngine, err := p.engineManager.LoadActiveEngine()
	if err != nil {
		panic(err)
	}
	p.mainEngine = mainEngine
}

func (p *Protocol) initTipManager() {
	p.tipManager = p.optsTipManagerProvider(p.mainEngine)
}

func (p *Protocol) API() iotago.API {
	return p.api
}

func (p *Protocol) ProcessBlock(block *model.Block, src identity.ID) error {
	mainEngine := p.MainEngineInstance()

	fmt.Println("process block", block.ID(), src)
	mainEngine.ProcessBlockFromPeer(block, src)

	return nil
}

func (p *Protocol) MainEngineInstance() *engine.Engine {
	return p.mainEngine
}

func (p *Protocol) Network() *core.Protocol {
	return p.networkProtocol
}

func (p *Protocol) SlotTimeProvider() *iotago.SlotTimeProvider {
	return p.MainEngineInstance().SlotTimeProvider()
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

func WithBaseDirectory(baseDirectory string) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsBaseDirectory = baseDirectory
	}
}

func WithPruningThreshold(pruningThreshold uint64) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsPruningThreshold = pruningThreshold
	}
}

func WithSnapshotPath(snapshot string) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsSnapshotPath = snapshot
	}
}

func WithFilterProvider(optsFilterProvider module.Provider[*engine.Engine, filter.Filter]) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsFilterProvider = optsFilterProvider
	}
}

func WithBlockDAGProvider(optsBlockDAGProvider module.Provider[*engine.Engine, blockdag.BlockDAG]) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsBlockDAGProvider = optsBlockDAGProvider
	}
}

func WithTipManagerProvider(optsTipManagerProvider module.Provider[*engine.Engine, tipmanager.TipManager]) options.Option[Protocol] {
	return func(n *Protocol) {
		n.optsTipManagerProvider = optsTipManagerProvider
	}
}

func WithEngineOptions(opts ...options.Option[engine.Engine]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsEngineOptions = append(p.optsEngineOptions, opts...)
	}
}

func WithStorageDatabaseManagerOptions(opts ...options.Option[database.Manager]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsStorageDatabaseManagerOptions = append(p.optsStorageDatabaseManagerOptions, opts...)
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
