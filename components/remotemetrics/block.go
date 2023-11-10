package remotemetrics

import (
	"time"

	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	mempoolv1 "github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/v1"
	iotago "github.com/iotaledger/iota.go/v4"
)

func sendBlockSchedulerRecord(block *blocks.Block, recordType string) {
	if !deps.Protocol.MainEngineInstance().SyncManager.IsNodeSynced() {
		return
	}
	var nodeID = deps.Host.ID().String()

	record := &BlockScheduledMetrics{
		Type:         recordType,
		NodeID:       nodeID,
		MetricsLevel: ParamsRemoteMetrics.MetricsLevel,
		BlockID:      block.ID().ToHex(),
	}

	issuerID := block.ModelBlock().ProtocolBlock().Header.IssuerID
	record.IssuedTimestamp = block.IssuingTime()
	record.IssuerID = issuerID.String()
	// TODO: implement when mana is refactored
	// record.AccessMana = deps.Protocol.Engine().CongestionControl.Scheduler.GetManaFromCache(issuerID)
	record.StrongEdgeCount = len(block.StrongParents())
	if weakParentsCount := len(block.WeakParents()); weakParentsCount > 0 {
		record.StrongEdgeCount = weakParentsCount
	}
	if likeParentsCount := len(block.ShallowLikeParents()); likeParentsCount > 0 {
		record.StrongEdgeCount = likeParentsCount
	}

	record.ScheduledTimestamp = block.ScheduledTime()
	record.BookedTimestamp = block.BookedTime()
	record.DroppedTimestamp = block.DroppedTime()
	// may be overridden by tx data
	record.SolidTimestamp = block.SolidificationTime()
	record.DeltaSolid = block.SolidificationTime().Sub(record.IssuedTimestamp).Nanoseconds()
	record.DeltaBooked = block.BookedTime().Sub(record.IssuedTimestamp).Nanoseconds()

	var scheduleDoneTime time.Time
	// one of those conditions must be true
	if !record.ScheduledTimestamp.IsZero() {
		scheduleDoneTime = record.ScheduledTimestamp
	} else if !record.DroppedTimestamp.IsZero() {
		scheduleDoneTime = record.DroppedTimestamp
	}
	record.DeltaScheduledIssued = scheduleDoneTime.Sub(record.IssuedTimestamp).Nanoseconds()
	// record.DeltaScheduledReceived = scheduleDoneTime.Sub(block.ReceivedTime()).Nanoseconds()
	// record.DeltaReceivedIssued = block.ReceivedTime().Sub(record.IssuedTimestamp).Nanoseconds()
	record.SchedulingTime = scheduleDoneTime.Sub(block.QueuedTime()).Nanoseconds()

	// override block solidification data if block contains a transaction
	signedTx, isSignedTx := block.ModelBlock().Payload().(*iotago.SignedTransaction)
	if isSignedTx {
		txID, err := signedTx.Transaction.ID()
		if err != nil {
			return
		}
		txMetadata, exist := deps.Protocol.MainEngineInstance().Ledger.MemPool().TransactionMetadata(txID)
		if !exist {
			return
		}

		metadataImpl, isImpl := txMetadata.(*mempoolv1.TransactionMetadata)
		if !isImpl {
			return
		}

		record.SolidTimestamp = metadataImpl.SolidTimestamp()
		record.TransactionID = txID.ToHex()
		record.DeltaSolid = metadataImpl.BookedTimestamp().Sub(record.IssuedTimestamp).Nanoseconds()

	}

	// _ = deps.RemoteLogger.Send(record)
}

// func onTransactionAccepted(transactionEvent *mempool.TransactionEvent) {
// 	// if !deps.Protocol.Engine().IsSynced() {
// 	if !deps.Protocol.MainEngineInstance().SyncManager.IsNodeSynced() {
// 		return
// 	}

// 	earliestAttachment := deps.Protocol.Engine().Tangle.Booker().GetEarliestAttachment(transactionEvent.Metadata.ID())

// 	onBlockFinalized(earliestAttachment.ModelsBlock)
// }

func sendMissingBlockRecord(block *blocks.Block, recordType string) {
	if !deps.Protocol.MainEngineInstance().SyncManager.IsNodeSynced() {
		return
	}
	nodeID := deps.Host.ID().String()

	_ = deps.RemoteLogger.Send(&MissingBlockMetrics{
		Type:         recordType,
		NodeID:       nodeID,
		MetricsLevel: ParamsRemoteMetrics.MetricsLevel,
		BlockID:      block.ID().ToHex(),
		IssuerID:     block.ModelBlock().ProtocolBlock().Header.IssuerID.ToHex(),
	})
}
