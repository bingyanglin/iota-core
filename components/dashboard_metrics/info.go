package dashboardmetrics

import (
	"runtime"
	"time"
)

var (
	nodeStartupTimestamp = time.Now()
)

func nodeInfoExtended() *NodeInfoExtended {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := &NodeInfoExtended{
		Version:       deps.AppInfo.Version,
		LatestVersion: deps.AppInfo.LatestGitHubVersion,
		Uptime:        time.Since(nodeStartupTimestamp).Milliseconds(),
		NodeID:        deps.Host.ID().String(),
		NodeAlias:     ParamsNode.Alias,
		MemoryUsage:   int64(m.HeapAlloc + m.StackSys + m.MSpanSys + m.MCacheSys + m.BuckHashSys + m.GCSys + m.OtherSys),
	}

	return status
}
