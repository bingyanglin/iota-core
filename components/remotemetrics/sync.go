package remotemetrics

import (
	"time"

	"github.com/iotaledger/iota-core/pkg/protocol/engine/syncmanager"
	"go.uber.org/atomic"
)

var isTangleTimeSynced atomic.Bool

func checkSynced(newStatus *syncmanager.SyncStatus) {
	oldTangleTimeSynced := isTangleTimeSynced.Load()
	clSnapshot := deps.Protocol.MainEngineInstance().Clock.Snapshot()
	if oldTangleTimeSynced != newStatus.NodeSynced {
		nodeID := deps.Host.ID().String()

		syncStatusChangedEvent := &TangleTimeSyncChangedEvent{
			Type:           "sync",
			NodeID:         nodeID,
			MetricsLevel:   ParamsRemoteMetrics.MetricsLevel,
			Time:           time.Now(),
			ATT:            clSnapshot.AcceptedTime,
			RATT:           clSnapshot.RelativeAcceptedTime,
			CTT:            clSnapshot.ConfirmedTime,
			RCTT:           clSnapshot.RelativeConfirmedTime,
			CurrentStatus:  newStatus.NodeSynced,
			PreviousStatus: oldTangleTimeSynced,
		}
		// remotemetrics.Events.TangleTimeSyncChanged.Trigger(syncStatusChangedEvent)

		isTangleTimeSynced.Store(newStatus.NodeSynced)

		_ = deps.RemoteLogger.Send(syncStatusChangedEvent)
	}
}

func sendSyncStatusChangedEvent(syncUpdate *TangleTimeSyncChangedEvent) {
	// _ = deps.RemoteLogger.Send(syncUpdate)
}
