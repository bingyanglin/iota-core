package management

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/bytes"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/inx-app/pkg/httpserver"
	"github.com/iotaledger/iota.go/v4/api"
)

func pruneDatabase(c echo.Context) (*api.PruneDatabaseResponse, error) {
	if deps.Protocol.Engines.Main.Get().IsSnapshotting() || deps.Protocol.Engines.Main.Get().Storage.IsPruning() {
		return nil, ierrors.WithMessage(echo.ErrServiceUnavailable, "node is already creating a snapshot or pruning is running")
	}

	request := &api.PruneDatabaseRequest{}
	if err := c.Bind(request); err != nil {
		return nil, ierrors.WithMessagef(httpserver.ErrInvalidParameter, "invalid request, error: %w", err)
	}

	// only allow one type of pruning at a time
	if (request.Epoch == 0 && request.Depth == 0 && request.TargetDatabaseSize == "") ||
		(request.Epoch != 0 && request.Depth != 0) ||
		(request.Epoch != 0 && request.TargetDatabaseSize != "") ||
		(request.Depth != 0 && request.TargetDatabaseSize != "") {
		return nil, ierrors.WithMessage(httpserver.ErrInvalidParameter, "either epoch, depth or size has to be specified")
	}

	var err error

	if request.Epoch != 0 {
		err = deps.Protocol.Engines.Main.Get().Storage.PruneByEpochIndex(request.Epoch)
		if err != nil {
			return nil, ierrors.WithMessagef(echo.ErrInternalServerError, "pruning database failed: %w", err)
		}
	}

	if request.Depth != 0 {
		_, _, err := deps.Protocol.Engines.Main.Get().Storage.PruneByDepth(request.Depth)
		if err != nil {
			return nil, ierrors.WithMessagef(echo.ErrInternalServerError, "pruning database failed: %w", err)
		}
	}

	if request.TargetDatabaseSize != "" {
		pruningTargetDatabaseSizeBytes, err := bytes.Parse(request.TargetDatabaseSize)
		if err != nil {
			return nil, ierrors.WithMessagef(echo.ErrInternalServerError, "pruning database failed: %w", err)
		}

		err = deps.Protocol.Engines.Main.Get().Storage.PruneBySize(pruningTargetDatabaseSizeBytes)
		if err != nil {
			return nil, ierrors.WithMessagef(echo.ErrInternalServerError, "pruning database failed: %w", err)
		}
	}

	targetEpoch, hasPruned := deps.Protocol.Engines.Main.Get().Storage.LastPrunedEpoch()
	if hasPruned {
		targetEpoch++
	}

	return &api.PruneDatabaseResponse{
		Epoch: targetEpoch,
	}, nil
}
