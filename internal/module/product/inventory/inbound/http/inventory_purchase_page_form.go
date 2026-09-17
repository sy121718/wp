package inventoryhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/product/inventory/dto"
)

// inventory_purchase_page_form.go - 采购入库表单解析（收货明细行、草稿行与数值解析）。

// purchaseLinesForm 解析新建表单里的固定几行（空行跳过）。
func purchaseLinesForm(c *gin.Context) (lines []inventorydto.PurchaseLineReq) {
	variantIDs := c.PostFormArray("lineVariantId")
	productIDs := c.PostFormArray("lineProductId")
	skuCodes := c.PostFormArray("lineSKUCode")
	quantities := c.PostFormArray("lineQuantity")
	prices := c.PostFormArray("lineUnitPrice")
	lines = make([]inventorydto.PurchaseLineReq, 0, len(variantIDs))
	for i, raw := range variantIDs {
		variantID := strings.TrimSpace(raw)
		if variantID == "" {
			continue
		}
		line := inventorydto.PurchaseLineReq{
			VariantID: variantID,
			ProductID: formArrayAt(productIDs, i),
			SKUCode:   formArrayAt(skuCodes, i),
			Quantity:  parseIntOr(formArrayAt(quantities, i), 0),
			Sort:      i,
		}
		if price, ok := parseFloatOK(formArrayAt(prices, i)); ok {
			line.UnitPrice = price
		}
		lines = append(lines, line)
	}
	return lines
}

// formArrayAt 取数组第 i 项（越界返回空串，表单行数与数组长度不一致时不 panic）。
func formArrayAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

// parseFloatOK 解析可空小数（空串 / 非法返回 ok=false，交给服务端报参数错误）。
func parseFloatOK(raw string) (value float64, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// purchaseDraftLines 新建窗体里的空行（模板 range 用）。
func purchaseDraftLines() []gin.H {
	out := make([]gin.H, 0, inventoryPurchaseDraftLines)
	for i := 0; i < inventoryPurchaseDraftLines; i++ {
		out = append(out, gin.H{"Index": i})
	}
	return out
}
