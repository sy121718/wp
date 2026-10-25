package inventorydto

// inventory_sku_form_req.go — 「仓库商品 SKU」台账页的三个写表单（迁移 621）。
//
// 三个表单共用一条口径：**归一留给 service**，本层只做「表单字符串 → 结构化入参」的搬运
// 与「哪个字段是必填」的判定。理由与 ExternalSKUFormReq 一致：去空白 / 长度上限 /
// 裸码归一全部只有 service 一处实现，控制器多添一道就会变成两套规则。

import (
	"math"
	"strconv"
	"strings"
)

// ClaimStockFormReq 认领表单：把一条未认领存货挂到某个商品变体上。
type ClaimStockFormReq struct {
	ProjectID string `form:"projectId"`
	StockID   string `form:"stockId"`
	ProductID string `form:"productId"`
	VariantID string `form:"variantId"`
}

// ClaimReq 转成 service 的认领请求。
func (r *ClaimStockFormReq) ClaimReq() *ClaimStockReq {
	return &ClaimStockReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		StockID:   strings.TrimSpace(r.StockID),
		ProductID: strings.TrimSpace(r.ProductID),
		VariantID: strings.TrimSpace(r.VariantID),
	}
}

// UnclaimStockFormReq 解除认领表单。
type UnclaimStockFormReq struct {
	ProjectID string `form:"projectId"`
	StockID   string `form:"stockId"`
}

// UnclaimReq 转成 service 的解除认领请求。
func (r *UnclaimStockFormReq) UnclaimReq() *UnclaimStockReq {
	return &UnclaimStockReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		StockID:   strings.TrimSpace(r.StockID),
	}
}

// UnclaimedStockFormReq 新建未认领存货表单（仓库 + 裸码 + 数量 + 成本）。
//
// TrackQuantity 是**显式二选一**（页面给「跟踪数量 / 无限库存」两个选项）：
// 迁移 261 的 CHECK 不允许「不跟踪却带数字」，猜错的方向是静默记错账，
// 所以表单必须把意图写出来，service 只按意图落库并校验自相矛盾的组合。
type UnclaimedStockFormReq struct {
	ProjectID     string `form:"projectId"`
	WarehouseID   string `form:"warehouseId"`
	SKUCode       string `form:"skuCode"`
	ExternalSKU   string `form:"externalSku"`
	TrackQuantity string `form:"trackQuantity"`
	Quantity      string `form:"quantity"`
	CostPrice     string `form:"costPrice"`
}

// Tracked 复选框语义：有值即「按数量跟踪」。
func (r *UnclaimedStockFormReq) Tracked() bool { return formBool(r.TrackQuantity) }

// CreateReq 转成 service 的建档请求；数量留空按零值，成本留空 = 尚未核算。
//
// 成本非法数字的情况在这里就拦下：`createReq` 的签名不带 error，但页面上直接填错一个
// 字母就能静默变成「未核算」—— 那会让成本凭空消失而不报错。所以非法值给了
// NaN 哨兵（normalizeExplicitCost 会把它拦成 ErrStockCostInvalid），而不是 nil。
func (r *UnclaimedStockFormReq) CreateReq() *CreateUnclaimedStockReq {
	return &CreateUnclaimedStockReq{
		ProjectID:     strings.TrimSpace(r.ProjectID),
		WarehouseID:   strings.TrimSpace(r.WarehouseID),
		SKUCode:       strings.TrimSpace(r.SKUCode),
		ExternalSKU:   r.ExternalSKU,
		TrackQuantity: r.Tracked(),
		Quantity:      formInt(r.Quantity, 0),
		CostPrice:     formCostPrice(r.CostPrice),
	}
}

// formCostPrice 成本价：空串 = 未核算（nil）；非法数字 = NaN 哨兵（让 service 报参数错误）。
//
// 不用「非法就当 nil」：成本悄悄变成「尚未核算」而不报错，是比报错更难查的一种错。
func formCostPrice(raw string) *float64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		nan := math.NaN()
		return &nan
	}
	return &parsed
}

// PurchaseStatusFormReq 采购单终态的页面表单（提前关单 / 取消共用）。
type PurchaseStatusFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Remark    string `form:"remark"`
}

// StatusReq 转成 service 的终态请求。
func (r *PurchaseStatusFormReq) StatusReq() *PurchaseStatusReq {
	return &PurchaseStatusReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		ID:        strings.TrimSpace(r.ID),
		Remark:    strings.TrimSpace(r.Remark),
	}
}
