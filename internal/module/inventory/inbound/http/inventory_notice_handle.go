package inventoryhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

var (
	inventoryStockBackKeys     = []string{"project", "sku", "variantId", "warehouseId", "direction", "reasonCode", "timeFrom", "timeTo", "page", "limit"}
	inventoryWarehouseBackKeys = []string{"project"}
	inventoryReasonBackKeys    = []string{"project"}

	inventoryMovementsBackKeys = []string{"project", "sku", "warehouseId", "direction", "reasonCode", "timeFrom", "timeTo", "page", "limit"}

	inventorySKUBackKeys = []string{"project", "warehouseId", "keyword", "status", "page", "limit"}

	inventoryTransferBackKeys = []string{"project", "status", "keyword", "page", "limit"}
)

func inventoryMsgText(c *gin.Context, m facing.Msg) string { return response.MsgText(c, m) }

func inventoryFail(c *gin.Context, err error) {
	response.ErrorWithMessage(c, response.ErrorStatus(err), inventoryErrText(c, err))
}

func inventoryFailMsg(c *gin.Context, msg facing.Msg) {
	response.ErrorMsg(c, http.StatusBadRequest, msg)
}

func projectOptions(in []projectcontract.ProjectResp) []ProjectOptionResp {
	out := make([]ProjectOptionResp, 0, len(in))
	for _, p := range in {
		out = append(out, ProjectOptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}
