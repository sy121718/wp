package inventoryhttp

import (
	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

func (h *Handle) StockAlert(c *gin.Context) {
	query, err := binding.ParseQuery[inventorydto.StockAlertQueryReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.StockAlert(c.Request.Context(), query.ProjectID)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}
