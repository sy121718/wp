package inventoryhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// purchaseCreateFormFields 是单值字段的回填契约；三列同名采购行保留位置单独处理。
var purchaseCreateFormFields = []string{"projectId", "code", "sourceId", "warehouseId", "remark"}

// purchaseCreateFail 在抽屉内回显原始输入，采购行的空位也不压缩。
func (h *inventoryPurchasePageHandle) purchaseCreateFail(c *gin.Context, projectID string, err error) {
	if !isHXRequest(c) {
		redirectPurchaseErr(c, projectID, err)
		return
	}
	// 直接读取 PostForm：通用 formEcho 会剔除空项、修剪首尾空白，导致空行错位。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	values := c.Request.PostForm
	fields := make(gin.H, len(purchaseCreateFormFields))
	for _, key := range purchaseCreateFormFields {
		fields[key] = values.Get(key)
	}
	rows := make([]gin.H, inventoryPurchaseDraftLines)
	for i := range rows {
		rows[i] = gin.H{
			"Index":     i,
			"SKU":       formArrayAtRaw(values["lineSku"], i),
			"Quantity":  formArrayAtRaw(values["lineQuantity"], i),
			"UnitPrice": formArrayAtRaw(values["lineUnitPrice"], i),
		}
	}
	// 候选项由当前工程重取，表单值只取此次提交。
	ctx := c.Request.Context()
	data := gin.H{
		"FormEcho": fields, "SelectedProject": projectID, "DraftLines": rows,
		"Sources":        sourceOptions(ctx, h.inventory, projectID, ""),
		"Warehouses":     h.purchaseWarehouseOptions(ctx, projectID),
		"VariantOptions": h.purchaseVariantOptions(ctx, projectID),
		"SubmitErr":      inventoryErrText(c, err),
	}
	c.HTML(http.StatusOK, "admin/inventory/inventory_purchase_create_form.html", shell.Prepare(c, data))
}

func formArrayAtRaw(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}

func purchaseCreateSuccess(c *gin.Context, projectID string) {
	redirectWhere(c, inventoryPurchasesPath+"?project="+urlQueryEscape(projectID)+"&ok=1")
}
