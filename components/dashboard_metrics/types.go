package dashboardmetrics

// NodeInfoExtended represents extended information about the node.
type NodeInfoExtended struct {
	Version       string `json:"version"`
	LatestVersion string `json:"latestVersion"`
	Uptime        int64  `json:"uptime"`
	NodeID        string `json:"nodeId"`
	NodeAlias     string `json:"nodeAlias"`
	MemoryUsage   int64  `json:"memUsage"`
}

// DatabaseSizesMetric represents database size metrics.
type DatabaseSizesMetric struct {
	Permanent  int64 `json:"permanent"`
	Prunable   int64 `json:"prunable"`
	TxRetainer int64 `json:"txRetainer"`
	Total      int64 `json:"total"`
	Time       int64 `json:"ts"`
}

// GossipMetrics represents the metrics for blocks per second.
type GossipMetrics struct {
	Incoming uint32 `json:"incoming"`
	New      uint32 `json:"new"`
	Outgoing uint32 `json:"outgoing"`
}
