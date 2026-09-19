package inventoryhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/product/inventory/dto"
)

// inventory_purchase_page_form.go - 采购入库表单解析（收货明细行、草稿行与数值解析）。

// purchaseLineSkuField 采购行 SKU 选择器的表单字段名（模板与解析共用同一个字面量）。
//
// 它承载的是三段拼接的引用 —— "<变体ID>|<商品ID>|<仓库侧裸码>"，不是裸的 SKU 编码：
// 本页不引入自定义 JS（多端契约要求交互全走原生控件），一个 <select> 只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧 SKU 必须**同源**（都取自候选列表的同一行）。
// 字段名与值形状两边（模板 + 本文件）必须一起改 —— 历史上正是「handler 读 lineSKUCode、
// 模板却根本没有这个字段」让空串静默通过了校验，见 parsePurchaseSkuRef 的注释。
const purchaseLineSkuField = "lineSku"

// purchaseLinesForm 解析新建表单里的固定几行（空行跳过）。
//
// 驱动数组是 purchaseLineSkuField（未选 SKU 的行整行跳过，数量 / 单价各按同一序号取值）——
// 与改造前用 lineVariantId 驱动是同一个形状，只是那个字段名当时在模板里并不存在。
// SKUCode 为空**不在这里兜底**：归一旦校验在 service（normalizeStockSKU），
// 这条路径只把表单原样翻译成入参（含空值），让唯一的规则在唯一的入口上生效。
func purchaseLinesForm(c *gin.Context) (lines []inventorydto.PurchaseLineReq) {
	refs := c.PostFormArray(purchaseLineSkuField)
	quantities := c.PostFormArray("lineQuantity")
	prices := c.PostFormArray("lineUnitPrice")
	lines = make([]inventorydto.PurchaseLineReq, 0, len(refs))
	for i, raw := range refs {
		variantID, productID, skuCode := parsePurchaseSkuRef(raw)
		if variantID == "" {
			continue
		}
		line := inventorydto.PurchaseLineReq{
			VariantID: variantID,
			ProductID: productID,
			SKUCode:   skuCode,
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

// parsePurchaseSkuRef 拆开采购行 SKU 选择器的值 "<变体ID>|<商品ID>|<仓库侧裸码>"。
//
// 按 SplitN(…, 3) 拆：前两段是 uuid（不含 '|'），第三段（SKU 编码）原样收下 ——
// 运营自定义的编码里若真的出现 '|'，也只会在最后一段里，不会被误切。
// 段数不足时后面的段为空串（模板不会这么渲染，但**不能因此 panic 或静默错位**：
// 空值会一路走到 service 的归一校验并被明确拒绝）。
func parsePurchaseSkuRef(raw string) (variantID, productID, skuCode string) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 3)
	variantID = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		productID = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		skuCode = strings.TrimSpace(parts[2])
	}
	return variantID, productID, skuCode
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
