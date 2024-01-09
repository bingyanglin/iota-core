// Package remotelog is a plugin that enables log blocks being sent via UDP to a central ELK stack for debugging.
// It is disabled by default and when enabled, additionally, logger.disableEvents=false in config.json needs to be set.
// The destination can be set via logger.remotelog.serverAddress.
// All events according to logger.level in config.json are sent.
package remotelog

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/dig"
	"gopkg.in/src-d/go-git.v4"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/logger"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/daemon"
	"github.com/libp2p/go-libp2p/core/host"
)

const (
	remoteLogType = "log"

	levelIndex = 0
	nameIndex  = 1
	blockIndex = 2
)

var (
	// Component is the plugin instance of the remote plugin instance.

	Component *app.Component
	deps      dependencies

	myID          string
	myGitHead     string
	myGitConflict string
)

type dependencies struct {
	dig.In

	Host         host.Host
	RemoteLogger *RemoteLoggerConn
}

func init() {
	Component = &app.Component{
		Name:      "RemoteLogger",
		DepsFunc:  func(cDeps dependencies) { deps = cDeps },
		Params:    params,
		Provide:   provide,
		Configure: configure,
		Run:       run,
		IsEnabled: func(c *dig.Container) bool {
			return ParamsRemoteLog.Enabled
		},
	}
}

func provide(c *dig.Container) error {
	if err := c.Provide(func() *RemoteLoggerConn {
		Component.LogWarn("RemoteLog is enabled. All events will be sent to the configured server.: ", ParamsRemoteLog.ServerAddress)
		remoteLogger, err := newRemoteLoggerConn(ParamsRemoteLog.ServerAddress)
		if err != nil {
			Component.LogPanic(err.Error())
			return nil
		}
		return remoteLogger
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	return nil
}

func configure() error {
	myID = deps.Host.ID().String()

	getGitInfo()

	return nil
}

func run() error {
	Component.LogInfo("Starting RemoteLog server ...")

	if err := Component.Daemon().BackgroundWorker("RemoteLog", func(ctx context.Context) {
		Component.LogInfo("Starting RemoteLog server ... done")
		hook := logger.Events.AnyMsg.Hook(func(logEvent *logger.LogEvent) {
			deps.RemoteLogger.SendLogMsg(logEvent.Level, logEvent.Name, logEvent.Msg)
		}, event.WithWorkerPool(Component.WorkerPool))

		<-ctx.Done()
		Component.LogInfo("Stopping RemoteLog ...")
		hook.Unhook()
		Component.LogInfo("Stopping RemoteLog ... done")
	}, daemon.PriorityRemoteLog); err != nil {
		Component.LogPanicf("Failed to start as daemon: %s", err)
	}

	return nil
}

func getGitInfo() {
	r, err := git.PlainOpen(getGitDir())
	if err != nil {
		Component.LogDebug("Could not open Git repo.")
		return
	}

	// extract git conflict and head
	if h, err := r.Head(); err == nil {
		myGitConflict = h.Name().String()
		myGitHead = h.Hash().String()
	}
}

func getGitDir() string {
	var gitDir string

	// this is valid when running an executable, when using "go run" this is a temp path
	if ex, err := os.Executable(); err == nil {
		temp := filepath.Join(filepath.Dir(ex), ".git")
		if _, err := os.Stat(temp); err == nil {
			gitDir = temp
		}
	}

	// when running "go run" from the same directory
	if gitDir == "" {
		if wd, err := os.Getwd(); err == nil {
			temp := filepath.Join(wd, ".git")
			if _, err := os.Stat(temp); err == nil {
				gitDir = temp
			}
		}
	}

	return gitDir
}

type logBlock struct {
	Version     string    `json:"version"`
	GitHead     string    `json:"gitHead,omitempty"`
	GitConflict string    `json:"gitConflict,omitempty"`
	NodeID      string    `json:"nodeId"`
	Level       string    `json:"level"`
	Name        string    `json:"name"`
	Msg         string    `json:"msg"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
}
