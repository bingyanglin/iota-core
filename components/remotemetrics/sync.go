package remotemetrics

import (
	"time"

	"go.uber.org/atomic"

	"github.com/iotaledger/goshimmer/packages/app/remotemetrics"
)

var isTangleTimeSynced atomic.Bool

func checkSynced() {
	oldTangleTimeSynced := isTangleTimeSynced.Load()
	clSnapshot := deps.Protocol.MainEngineInstance().Clock.Snapshot()
	tts := deps.Protocol.MainEngineInstance().SyncManager.SyncStatus()
	if oldTangleTimeSynced != tts.NodeSynced {
		var myID string
		if deps.Local != nil {
			myID = deps.Local.ID().String()
		}
		syncStatusChangedEvent := &remotemetrics.TangleTimeSyncChangedEvent{
			Type:           "sync",
			NodeID:         myID,
			MetricsLevel:   ParamsRemoteMetrics.MetricsLevel,
			Time:           time.Now(),
			ATT:            clSnapshot.AcceptedTime,
			RATT:           clSnapshot.RelativeAcceptedTime,
			CTT:            clSnapshot.ConfirmedTime,
			RCTT:           clSnapshot.RelativeConfirmedTime,
			CurrentStatus:  tts,
			PreviousStatus: oldTangleTimeSynced,
		}
		remotemetrics.Events.TangleTimeSyncChanged.Trigger(syncStatusChangedEvent)
	}
}

func sendSyncStatusChangedEvent(syncUpdate *remotemetrics.TangleTimeSyncChangedEvent) {
	// _ = deps.RemoteLogger.Send(syncUpdate)
}
