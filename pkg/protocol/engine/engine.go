package engine

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/core/eventticker"
	"github.com/iotaledger/hive.go/crypto/identity"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blockdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/eviction"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter"
	"github.com/iotaledger/iota-core/pkg/storage"
	iotago "github.com/iotaledger/iota.go/v4"
)

// region Engine /////////////////////////////////////////////////////////////////////////////////////////////////////

type Engine struct {
	Events         *Events
	Storage        *storage.Storage
	Filter         filter.Filter
	EvictionState  *eviction.State
	BlockRequester *eventticker.EventTicker[iotago.SlotIndex, iotago.BlockID]
	blockDAG       blockdag.BlockDAG

	Workers *workerpool.Group

	isBootstrapped      bool
	isBootstrappedMutex sync.Mutex

	optsBootstrappedThreshold time.Duration
	optsEntryPointsDepth      int
	optsSnapshotDepth         int
	optsBlockRequester        []options.Option[eventticker.EventTicker[iotago.SlotIndex, iotago.BlockID]]

	module.Module
}

func New(
	workers *workerpool.Group,
	storageInstance *storage.Storage,
	filterProvider module.Provider[*Engine, filter.Filter],
	blockDAGProvider module.Provider[*Engine, blockdag.BlockDAG],
	opts ...options.Option[Engine],
) (engine *Engine) {
	return options.Apply(
		&Engine{
			Events:        NewEvents(),
			Storage:       storageInstance,
			EvictionState: eviction.NewState(storageInstance),
			Workers:       workers,

			optsBootstrappedThreshold: 10 * time.Second,
			optsSnapshotDepth:         5,
		}, opts, func(e *Engine) {
			e.blockDAG = blockDAGProvider(e)
			e.Filter = filterProvider(e)
			e.BlockRequester = eventticker.New(e.optsBlockRequester...)

			e.HookInitialized(lo.Batch(
				e.Storage.Settings.TriggerInitialized,
				e.Storage.Commitments.TriggerInitialized,
			))
		},
		(*Engine).setupBlockStorage,
		(*Engine).setupEvictionState,
		(*Engine).setupBlockRequester,
		(*Engine).TriggerConstructed,
	)
}

func (e *Engine) Shutdown() {
	if !e.WasStopped() {
		e.TriggerStopped()

		e.BlockRequester.Shutdown()
		e.Workers.Shutdown()
		e.Storage.Shutdown()
	}
}

func (e *Engine) ProcessBlockFromPeer(block *model.Block, source identity.ID) {
	e.Filter.ProcessReceivedBlock(block, source)
	e.Events.BlockProcessed.Trigger(block.ID())
}

func (e *Engine) Block(id iotago.BlockID) (block *blockdag.Block, exists bool) {
	// var err error
	// if e.EvictionState.IsRootBlock(id) {
	// 	block, err = e.Storage.Blocks.Load(id)
	// 	exists = block != nil && err == nil
	// 	return
	// }

	// if cachedBlock, cachedBlockExists := e.Tangle.BlockDAG().Block(id); cachedBlockExists {
	// 	return cachedBlock.ModelsBlock, !cachedBlock.IsMissing()
	// }

	// if id.Index() > e.Storage.Settings.LatestCommitment().Index() {
	// 	return nil, false
	// }
	//
	// block, err = e.Storage.Blocks.Load(id)
	// exists = block != nil && err == nil

	return e.blockDAG.Block(id)
}

func (e *Engine) IsBootstrapped() (isBootstrapped bool) {
	e.isBootstrappedMutex.Lock()
	defer e.isBootstrappedMutex.Unlock()

	if e.isBootstrapped {
		return true
	}

	// if isBootstrapped = time.Since(e.Clock.Accepted().RelativeTime()) < e.optsBootstrappedThreshold && e.Notarization.IsFullyCommitted(); isBootstrapped {
	// 	e.isBootstrapped = true
	// }

	return isBootstrapped
}

func (e *Engine) IsSynced() (isBootstrapped bool) {
	return e.IsBootstrapped() // && time.Since(e.Clock.Accepted().Time()) < e.optsBootstrappedThreshold
}

func (e *Engine) API() iotago.API {
	return e.Storage.Settings.API()
}

func (e *Engine) Initialize(snapshot ...string) (err error) {
	if !e.Storage.Settings.SnapshotImported() {
		if len(snapshot) == 0 || snapshot[0] == "" {
			panic("no snapshot path specified")
		}
		if err = e.readSnapshot(snapshot[0]); err != nil {
			return errors.Wrapf(err, "failed to read snapshot from file '%s'", snapshot)
		}
	} else {
		e.Storage.Settings.UpdateAPI()
		e.Storage.Settings.TriggerInitialized()
		e.Storage.Commitments.TriggerInitialized()
		e.EvictionState.PopulateFromStorage(e.Storage.Settings.LatestCommitment().Index)

		// e.Notarization.Attestations().SetLastCommittedSlot(e.Storage.Settings.LatestCommitment().Index())
		// e.Notarization.Attestations().TriggerInitialized()
		//
		// e.Notarization.TriggerInitialized()
	}

	e.TriggerInitialized()

	return
}

func (e *Engine) WriteSnapshot(filePath string, targetSlot ...iotago.SlotIndex) (err error) {
	if len(targetSlot) == 0 {
		targetSlot = append(targetSlot, e.Storage.Settings.LatestCommitment().Index)
	}

	if fileHandle, err := os.Create(filePath); err != nil {
		return errors.Wrap(err, "failed to create snapshot file")
	} else if err = e.Export(fileHandle, targetSlot[0]); err != nil {
		return errors.Wrap(err, "failed to write snapshot")
	} else if err = fileHandle.Close(); err != nil {
		return errors.Wrap(err, "failed to close snapshot file")
	}

	return
}

func (e *Engine) Import(reader io.ReadSeeker) (err error) {
	if err = e.Storage.Settings.Import(reader); err != nil {
		return errors.Wrap(err, "failed to import settings")
	} else if err = e.Storage.Commitments.Import(reader); err != nil {
		return errors.Wrap(err, "failed to import commitments")
		// } else if err = e.Ledger.Import(reader); err != nil {
		// 	return errors.Wrap(err, "failed to import ledger")
	} else if err = e.EvictionState.Import(reader); err != nil {
		return errors.Wrap(err, "failed to import eviction state")
		// } else if err = e.Notarization.Import(reader); err != nil {
		// 	return errors.Wrap(err, "failed to import notarization state")
	}

	return
}

func (e *Engine) Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) (err error) {
	if err = e.Storage.Settings.Export(writer); err != nil {
		return errors.Wrap(err, "failed to export settings")
	} else if err = e.Storage.Commitments.Export(writer, targetSlot); err != nil {
		return errors.Wrap(err, "failed to export commitments")
		// } else if err = e.Ledger.Export(writer, targetSlot); err != nil {
		// 	return errors.Wrap(err, "failed to export ledger")
	} else if err = e.EvictionState.Export(writer, targetSlot); err != nil {
		return errors.Wrap(err, "failed to export eviction state")
		// } else if err = e.Notarization.Export(writer, targetSlot); err != nil {
		// 	return errors.Wrap(err, "failed to export notarization state")
	}

	return
}

// RemoveFromFilesystem removes the directory of the engine from the filesystem.
func (e *Engine) RemoveFromFilesystem() error {
	return os.RemoveAll(e.Storage.Directory)
}

func (e *Engine) Name() string {
	return filepath.Base(e.Storage.Directory)
}

func (e *Engine) setupBlockStorage() {
}

func (e *Engine) setupEvictionState() {
	e.Events.EvictionState.LinkTo(e.EvictionState.Events)
}

func (e *Engine) setupBlockRequester() {
	e.Events.BlockRequester.LinkTo(e.BlockRequester.Events)

	e.Events.EvictionState.SlotEvicted.Hook(e.BlockRequester.EvictUntil)

	// We need to hook to make sure that the request is created before the block arrives to avoid a race condition
	// where we try to delete the request again before it is created. Thus, continuing to request forever.
	e.Events.BlockDAG.BlockMissing.Hook(func(block *blockdag.Block) {
		// TODO: ONLY START REQUESTING WHEN NOT IN WARPSYNC RANGE (or just not attach outside)?
		e.BlockRequester.StartTicker(block.ID())
	})
	e.Events.BlockDAG.MissingBlockAttached.Hook(func(block *blockdag.Block) {
		e.BlockRequester.StopTicker(block.ID())
	}, event.WithWorkerPool(e.Workers.CreatePool("BlockRequester", 1))) // Using just 1 worker to avoid contention
}

func (e *Engine) readSnapshot(filePath string) (err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return errors.Wrap(err, "failed to open snapshot file")
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			panic(closeErr)
		}
	}()

	if err = e.Import(file); err != nil {
		return errors.Wrap(err, "failed to import snapshot")
	} else if err = e.Storage.Settings.SetSnapshotImported(true); err != nil {
		return errors.Wrap(err, "failed to set snapshot imported flag")
	}

	return
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Options //////////////////////////////////////////////////////////////////////////////////////////////////////

func WithBootstrapThreshold(threshold time.Duration) options.Option[Engine] {
	return func(e *Engine) {
		e.optsBootstrappedThreshold = threshold
	}
}

func WithEntryPointsDepth(entryPointsDepth int) options.Option[Engine] {
	return func(engine *Engine) {
		engine.optsEntryPointsDepth = entryPointsDepth
	}
}

func WithSnapshotDepth(depth int) options.Option[Engine] {
	return func(e *Engine) {
		e.optsSnapshotDepth = depth
	}
}

func WithRequesterOptions(opts ...options.Option[eventticker.EventTicker[iotago.SlotIndex, iotago.BlockID]]) options.Option[Engine] {
	return func(e *Engine) {
		e.optsBlockRequester = append(e.optsBlockRequester, opts...)
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
