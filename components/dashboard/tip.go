package dashboard

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type TipsResponse struct {
	Tips []string `json:"tips"`
}

func setupTipsRoutes(routeGroup *echo.Group) {
	routeGroup.GET("/tips", func(c echo.Context) (err error) {
		return c.JSON(http.StatusOK, tips())
	})
}

func tips() *TipsResponse {
	allTips := append(deps.Protocol.Engines.Main.Get().TipManager.StrongTips(), deps.Protocol.Engines.Main.Get().TipManager.WeakTips()...)
	t := make([]string, len(allTips))

	for i, tip := range allTips {
		t[i] = tip.ID().ToHex()
	}

	return &TipsResponse{Tips: t}
}
