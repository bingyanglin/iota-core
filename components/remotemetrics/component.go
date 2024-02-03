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
	"github.com/iotaledger/iota-core/components/remotelog"
	"github.com/iotaledger/iota-core/pkg/daemon"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/syncmanager"
	"github.com/libp2p/go-libp2p/core/host"
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
	Host host.Host

	Protocol     *protocol.Protocol
	RemoteLogger *remotelog.RemoteLoggerConn
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

		// Do not block until the Ticker is shutdown because we might want to start multiple Tickers and we can
		// safely ignore the last execution when shutting down.
		timeutil.NewTicker(func() {
			// remotemetrics.Events.SchedulerQuery.Trigger(&remotemetrics.SchedulerQueryEvent{Time: time.Now()})
		}, schedulerQueryUpdateTime, ctx)

		// Wait before terminating so we get correct log blocks from the daemon regarding the shutdown order.
		<-ctx.Done()
	}, daemon.PriorityRemoteMetrics); err != nil {
		Component.LogPanicf("Failed to start as daemon: %s", err)
	}

	return nil
}

func configureSyncMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	deps.Protocol.Events.Engine.SyncManager.UpdatedStatus.Hook(func(event *syncmanager.SyncStatus) {
		checkSynced(event)
	}, event.WithWorkerPool(Component.WorkerPool))
}

func configureSchedulerQueryMetrics() {
	if ParamsRemoteMetrics.MetricsLevel > Info {
		return
	}

	// remotemetrics.Events.SchedulerQuery.Hook(func(event *remotemetrics.SchedulerQueryEvent) { obtainSchedulerStats(event.Time) }, event.WithWorkerPool(Component.WorkerPool))
}

func configureBlockScheduledMetrics() {
	deps.Protocol.Events.Engine.Scheduler.BlockScheduled.Hook(func(block *blocks.Block) {
		sendBlockSchedulerRecord(block, "blockScheduled")
		// Component.LogWarn("send data to remote logger: blockScheduled")
	}, event.WithWorkerPool(Component.WorkerPool))
	deps.Protocol.Events.Engine.Scheduler.BlockDropped.Hook(func(block *blocks.Block, err error) {
		sendBlockSchedulerRecord(block, "blockDiscarded")
	}, event.WithWorkerPool(Component.WorkerPool))

	deps.Protocol.Events.Engine.BlockGadget.BlockPreAccepted.Hook(func(block *blocks.Block) {
		sendBlockSchedulerRecord(block, "blockPreAccepted")
	}, event.WithWorkerPool(Component.WorkerPool))

	deps.Protocol.Events.Engine.BlockGadget.BlockAccepted.Hook(func(block *blocks.Block) {
		sendBlockSchedulerRecord(block, "blockAccepted")
	}, event.WithWorkerPool(Component.WorkerPool))

	deps.Protocol.Events.Engine.BlockGadget.BlockPreConfirmed.Hook(func(block *blocks.Block) {
		sendBlockSchedulerRecord(block, "blockPreConfirmed")
	}, event.WithWorkerPool(Component.WorkerPool))

	deps.Protocol.Events.Engine.BlockGadget.BlockConfirmed.Hook(func(block *blocks.Block) {
		sendBlockSchedulerRecord(block, "blockConfirmed")
	}, event.WithWorkerPool(Component.WorkerPool))

}

func configureMissingBlockMetrics() {
	// if ParamsRemoteMetrics.MetricsLevel > Info {
	// 	return
	// }

	// deps.Protocol.Events.Engine.BlockDAG.BlockMissing.Hook(func(block *blocks.Block) {
	// 	sendMissingBlockRecord(block, "missingBlock")
	// }, event.WithWorkerPool(Component.WorkerPool))
	// deps.Protocol.Events.Engine.BlockDAG.MissingBlockAttached.Hook(func(block *blocks.Block) {
	// 	sendMissingBlockRecord(block, "missingBlockStored")
	// }, event.WithWorkerPool(Component.WorkerPool))
}
