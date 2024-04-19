package inx

import (
	"context"

	"github.com/labstack/echo/v4"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/iota-core/components/protocol"
	"github.com/iotaledger/iota-core/pkg/daemon"
	protocolpkg "github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/requesthandler"
	restapipkg "github.com/iotaledger/iota-core/pkg/restapi"
)

func init() {
	Component = &app.Component{
		Name:     "INX",
		DepsFunc: func(cDeps dependencies) { deps = cDeps },
		Params:   params,
		Provide:  provide,
		Run:      run,
		IsEnabled: func(_ *dig.Container) bool {
			return ParamsINX.Enabled
		},
	}
}

var (
	Component *app.Component
	deps      dependencies
)

type dependencies struct {
	dig.In
	Protocol         *protocolpkg.Protocol
	RequestHandler   *requesthandler.RequestHandler
	Echo             *echo.Echo `optional:"true"`
	RestRouteManager *restapipkg.RestRouteManager
	INXServer        *Server
	BaseToken        *protocol.BaseToken
}

func provide(c *dig.Container) error {
	//nolint:gocritic // easier to read which type is returned
	if err := c.Provide(func() *Server {
		return newServer()
	}); err != nil {
		Component.LogPanic(err.Error())
	}

	return nil
}

func run() error {
	if err := Component.Daemon().BackgroundWorker("INX", func(ctx context.Context) {
		Component.LogInfo("Starting INX ... done")
		deps.INXServer.Start()
		<-ctx.Done()
		Component.LogInfo("Stopping INX ...")
		deps.INXServer.Stop()
		Component.LogInfo("Stopping INX ... done")
	}, daemon.PriorityINX); err != nil {
		Component.LogPanicf("failed to start worker: %s", err)
	}

	return nil
}
