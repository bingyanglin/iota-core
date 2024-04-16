package management

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/inx-app/pkg/httpserver"
	"github.com/iotaledger/iota.go/v4/api"
)

func createSnapshots(c echo.Context) (*api.CreateSnapshotResponse, error) {
	if deps.Protocol.Engines.Main.Get().IsSnapshotting() || deps.Protocol.Engines.Main.Get().Storage.IsPruning() {
		return nil, ierrors.WithMessage(echo.ErrServiceUnavailable, "node is already creating a snapshot or pruning is running")
	}

	request := &api.CreateSnapshotRequest{}
	if err := c.Bind(request); err != nil {
		return nil, ierrors.WithMessagef(httpserver.ErrInvalidParameter, "invalid request: %w", err)
	}
	if request.Slot == 0 {
		return nil, ierrors.WithMessage(httpserver.ErrInvalidParameter, "slot needs to be specified")
	}

	directory := filepath.Dir(deps.SnapshotFilePath)
	fileName := filepath.Base(deps.SnapshotFilePath)
	fileExt := filepath.Ext(fileName)
	fileNameWithoutExt := strings.TrimSuffix(fileName, fileExt)
	filePath := filepath.Join(directory, fmt.Sprintf("%s_%d%s", fileNameWithoutExt, request.Slot, fileExt))

	if err := deps.Protocol.Engines.Main.Get().WriteSnapshot(filePath, request.Slot); err != nil {
		return nil, ierrors.WithMessagef(echo.ErrInternalServerError, "creating snapshot failed: %w", err)
	}

	return &api.CreateSnapshotResponse{
		Slot:     request.Slot,
		FilePath: filePath,
	}, nil
}
