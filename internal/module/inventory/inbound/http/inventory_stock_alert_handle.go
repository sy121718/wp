package inventoryhttp

import (
	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

func (h *Handle) StockAlert(c *gin.Context) {
	query, ok := binding.Query[inventorydto.StockAlertQueryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.StockAlert(c.Request.Context(), query.ProjectID)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}
