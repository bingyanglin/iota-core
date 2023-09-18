// Package remotemetrics is a plugin that enables log metrics too complex for Prometheus, but still interesting in terms of analysis and debugging.
// It is enabled by default.
// The destination can be set via logger.remotelog.serverAddress.
package remotemetrics

import (
	"context"
	"time"

	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/timeutil"
	"github.com/iotaledger/iota-core/pkg/daemon"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	syncUpdateTime           = 500 * time.Millisecond
	schedulerQueryUpdateTime = 5 * time.Second
)

const (
	// Debug defines the most verbose metrics collection level.
	Debug uint8 = iota
	// Info defines regular metrics collection level.
	Info
	// Important defines the level of collection of only most important metrics.
	Important
	// Critical defines the level of collection of only critical metrics.
	Critical
)

var (
	Component *app.Component
	deps      dependencies
)

type dependencies struct {
	dig.In

	Protocol *protocol.Protocol
	// RemoteLogger *remotelog.RemoteLoggerConn `optional:"true"`
}

// TODO: De-factor the Plugin to be Component

func init() {
	Component = &app.Component{
		Name:      "RemoteLogMetrics",
		DepsFunc:  func(cDeps dependencies) { deps = cDeps },
		Params:    params,
		Configure: configure,
		Run:       run,
		IsEnabled: func(c *dig.Container) bool {
			return ParamsRemoteMetrics.Enabled
		},
	}
}

func configure() error {
	configureSyncMetrics()
	configureConflictConfirmationMetrics()
	configureBlockFinalizedMetrics()
	configureBlockScheduledMetrics()
	configureMissingBlockMetrics()
	configureSchedulerQueryMetrics()

	return nil
}

func run() error {
	Component.LogInfo("Starting RemoteMetrics server ...")

	// create a background worker that update the metrics every second
	if err := Component.Daemon().BackgroundWorker("RemoteMetrics", func(ctx context.Context) {
		Component.LogInfo("Starting RemoteMetrics server ... done")

		measureInitialConflictCounts()

		// Do not block until the Ticker is shutdown because we might want to start multiple Tickers and we can
		// safely ignore the last execution when shutting down.
		timeutil.NewTicker(func() { checkSynced() }, syncUpdateTime, ctx)
		timeutil.NewTicker(func() {
			// remotemetrics.Events.SchedulerQuery.Trigger(&remotemetrics.SchedulerQueryEvent{Time: time.Now()})
		}, schedulerQueryUpdateTime, ctx)

		// Wait before terminating so we get correct log blocks from the daemon regarding the shutdown order.
		<-ctx.Done()
	}, daemon.PriorityRestAPI); err != nil {
		Component.LogPanicf("Failed to start as daemon: %s", err)
	}

	return nil
}

func configureSyncMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	// TODO: we only have event from syncmanager when anything is updated.
	// deps.Protocol.Events.Engine.SyncManager.UpdatedStatus.Hook(func(event *protocol.SyncStatus) {})

	// deps.Protocol.Events.Engine.SyncManager.Events.TangleTimeSyncChanged.Hook(func(event *remotemetrics.TangleTimeSyncChangedEvent) {
	// 	isTangleTimeSynced.Store(event.CurrentStatus)
	// }, event.WithWorkerPool(Component.WorkerPool))
	// remotemetrics.Events.TangleTimeSyncChanged.Hook(func(event *remotemetrics.TangleTimeSyncChangedEvent) {
	// 	sendSyncStatusChangedEvent(event)
	// }, event.WithWorkerPool(Component.WorkerPool))
}

func configureSchedulerQueryMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	// remotemetrics.Events.SchedulerQuery.Hook(func(event *remotemetrics.SchedulerQueryEvent) { obtainSchedulerStats(event.Time) }, event.WithWorkerPool(Component.WorkerPool))
}

func configureConflictConfirmationMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	deps.Protocol.Events.Engine.ConflictDAG.ConflictAccepted.Hook(func(conflictID iotago.Identifier) {
		onConflictConfirmed(conflictID)
	}, event.WithWorkerPool(Component.WorkerPool))

	deps.Protocol.Events.Engine.ConflictDAG.ConflictCreated.Hook(func(conflictID iotago.Identifier) {
		activeConflictsMutex.Lock()
		defer activeConflictsMutex.Unlock()

		if !activeConflicts.Has(conflictID) {
			conflictTotalCountDB.Inc()
			activeConflicts.Add(conflictID)
			sendConflictMetrics()
		}
	}, event.WithWorkerPool(Component.WorkerPool))
}

func configureBlockFinalizedMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	if ParamsRemoteMetrics.MetricsLevel == Info {
		// noo transaction accepted event now, need to do it like retainer: iota-core/pkg/retainer/retainer/retainer.go
		// deps.Protocol.Events.Engine.Ledger.TransactionAccepted.Hook(onTransactionAccepted, event.WithWorkerPool(Component.WorkerPool))
	} else {
		deps.Protocol.Events.Engine.BlockGadget.BlockConfirmed.Hook(func(block *blocks.Block) {
			onBlockFinalized(block.ModelBlock())
		}, event.WithWorkerPool(Component.WorkerPool))
	}
}

func configureBlockScheduledMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	if ParamsRemoteMetrics.MetricsLevel == Info {
		deps.Protocol.Events.Engine.Scheduler.BlockDropped.Hook(func(block *blocks.Block, err error) {
			sendBlockSchedulerRecord(block, "blockDiscarded")
		}, event.WithWorkerPool(Component.WorkerPool))
	} else {
		deps.Protocol.Events.Engine.Scheduler.BlockScheduled.Hook(func(block *blocks.Block) {
			sendBlockSchedulerRecord(block, "blockScheduled")
		}, event.WithWorkerPool(Component.WorkerPool))
		deps.Protocol.Events.Engine.Scheduler.BlockDropped.Hook(func(block *blocks.Block, err error) {
			sendBlockSchedulerRecord(block, "blockDiscarded")
		}, event.WithWorkerPool(Component.WorkerPool))
	}
}

func configureMissingBlockMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	deps.Protocol.Events.Engine.BlockDAG.BlockMissing.Hook(func(block *blocks.Block) {
		sendMissingBlockRecord(block.ModelBlock(), "missingBlock")
	}, event.WithWorkerPool(Component.WorkerPool))
	deps.Protocol.Events.Engine.BlockDAG.MissingBlockAttached.Hook(func(block *blocks.Block) {
		sendMissingBlockRecord(block.ModelBlock(), "missingBlockStored")
	}, event.WithWorkerPool(Component.WorkerPool))
}
