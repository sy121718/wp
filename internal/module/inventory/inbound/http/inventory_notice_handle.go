package inventoryhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
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

// inventoryFail 把失败事实交给共享 HTML 出口：界面写端点不再自己拼成品句。
func inventoryFail(c *gin.Context, err error) {
	pageview.FailStatus(c, response.ErrorStatus(err), inventoryErrScene, err)
}

// inventoryFailMsg 按业务消息渲染失败区。
func inventoryFailMsg(c *gin.Context, msg facing.Msg) {
	key := msg.Key
	if len(msg.Args) > 0 {
		key += "|" + strings.Join(msg.Args, "|")
	}
	pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, facing.WithStatus(facing.New(key), http.StatusBadRequest))
}

// inventoryWriteOK 是写操作的统一成功片段。
func inventoryWriteOK(c *gin.Context) {
	pageview.Success(c)
}

func projectOptions(in []projectcontract.ProjectResp) []ProjectOptionResp {
	out := make([]ProjectOptionResp, 0, len(in))
	for _, p := range in {
		out = append(out, ProjectOptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}
