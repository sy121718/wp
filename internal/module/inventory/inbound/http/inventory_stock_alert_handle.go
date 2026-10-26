// 库存预警接口（GET /api/inventory/stock/alert）。
package inventoryhttp

import (
	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

// StockAlert 库存预警（GET /api/inventory/stock/alert）。
//
// 复用 stock/list 的权限点：它是同一个真源的另一种读法（「哪些行要补货」），
// 不是新能力 —— 能看库存的人就能看预警。
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
