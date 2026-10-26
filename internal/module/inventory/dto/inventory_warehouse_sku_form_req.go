package inventorydto

import (
	"strconv"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// inventory_warehouse_sku_form_req.go — 仓库商品 SKU 主档（迁移 642 / B3）的表单请求。
//
// 为什么主档表单自成一份而不是塞进 WarehouseFormReq：
// 仓库是「一个地方」，货行是「那个地方的一条货」。两者的字段集合、写动作、权限点
// 都不同（仓库配一次长期不动，货行天天进进出出），合在一份表单里只会让每处判空
// 都要先问「这次提交的是哪一种」。
//
// 建行与编辑共用本结构：id 为空 = 新建（编码必填，同仓唯一），非空 = 编辑
//（编码不可改 —— 它同时是 244 唯一键的一半，改了等于换一条货，历史流水会断）。

// WarehouseSKUFormReq 建 / 改一条仓库货行的主档。
//
// 字段语义几处必须留意的：
//   - CostPrice / FreightPrice / CostFXRate 用字符串接：空串 = 「没填」，
//     与「填 0」必须分开（0 是显式的零成本 / 免运费；空 = 尚未核算 / 用默认值）。
//     service 侧把空串翻成 nil 指针，DB 上就是 NULL / DEFAULT。
//   - Status 只接受 active / disabled，本层就判掉（非法值一律拒绝，不静默按启用处理）。
//   - TrackQuantity 与 Quantity 成对：填了数量却不跟踪是自相矛盾，service 会拒。
type WarehouseSKUFormReq struct {
	ProjectID string `form:"projectId"`
	// ID 为空 = 新建。
	ID string `form:"id"`
	// WarehouseID 新建时必填；编辑时忽略（编码与所在仓都是这条货的身份，不可换仓）。
	WarehouseID string `form:"warehouseId"`
	SKUCode     string `form:"skuCode"`
	Name        string `form:"name"`
	ExternalSKU string `form:"externalSku"`
	ImageURL    string `form:"imageUrl"`
	Barcode     string `form:"barcode"`
	Remark      string `form:"remark"`
	Status      string `form:"status"`
	// Quantity / TrackQuantity 只有新建时才写：已有行的数量只能走变动契约
	//（有变动必有流水，行内新建不是例外）。编辑态这两个字段被模板置为 disabled。
	Quantity      string `form:"quantity"`
	TrackQuantity string `form:"trackQuantity"`
	CostPrice     string `form:"costPrice"`
	FreightPrice  string `form:"freightPrice"`
	CostCurrency  string `form:"costCurrency"`
	// CostBaseCurrency / CostFXRate 只在换算币与计价币不同时才有意义；
	// 留空时 service 沿用计价币并把汇率置 nil（= 未固化，不推算）。
	CostBaseCurrency string `form:"costBaseCurrency"`
	CostFXRate       string `form:"costFxRate"`
}

// ValidStatus 启停只接受两个值（其余一律拒绝）。
func (r *WarehouseSKUFormReq) ValidStatus() bool {
	status := strings.TrimSpace(r.Status)
	return status == "" || status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

// StatusValue 归一后的状态（没填时按启用）。
func (r *WarehouseSKUFormReq) StatusValue() string {
	status := strings.TrimSpace(r.Status)
	if status == "" {
		return inventoryenums.StatusActive
	}
	return status
}

// CreateReq 转成 service 的建档请求。
func (r *WarehouseSKUFormReq) CreateReq() *CreateStockReq {
	return &CreateStockReq{
		ProjectID:        strings.TrimSpace(r.ProjectID),
		WarehouseID:      strings.TrimSpace(r.WarehouseID),
		SKUCode:          strings.TrimSpace(r.SKUCode),
		ExternalSKU:      strings.TrimSpace(r.ExternalSKU),
		Name:             strings.TrimSpace(r.Name),
		ImageURL:         strings.TrimSpace(r.ImageURL),
		Barcode:          strings.TrimSpace(r.Barcode),
		Remark:           strings.TrimSpace(r.Remark),
		Status:           r.StatusValue(),
		Quantity:         formInt(r.Quantity, 0),
		TrackQuantity:    formBool(r.TrackQuantity),
		CostPrice:        formFloatPtr(r.CostPrice),
		FreightPrice:     formFloatPtr(r.FreightPrice),
		CostCurrency:     strings.TrimSpace(r.CostCurrency),
		CostBaseCurrency: strings.TrimSpace(r.CostBaseCurrency),
		CostFXRate:       formFloatPtr(r.CostFXRate),
	}
}

// UpdateReq 转成 service 的编辑请求（编码与所在仓**不在**请求里：它们不可变）。
func (r *WarehouseSKUFormReq) UpdateReq() *UpdateStockReq {
	return &UpdateStockReq{
		ProjectID:        strings.TrimSpace(r.ProjectID),
		ID:               strings.TrimSpace(r.ID),
		Name:             strings.TrimSpace(r.Name),
		ExternalSKU:      strings.TrimSpace(r.ExternalSKU),
		ImageURL:         strings.TrimSpace(r.ImageURL),
		Barcode:          strings.TrimSpace(r.Barcode),
		Remark:           strings.TrimSpace(r.Remark),
		Status:           r.StatusValue(),
		CostPrice:        formFloatPtr(r.CostPrice),
		FreightPrice:     formFloatPtr(r.FreightPrice),
		CostCurrency:     strings.TrimSpace(r.CostCurrency),
		CostBaseCurrency: strings.TrimSpace(r.CostBaseCurrency),
		CostFXRate:       formFloatPtr(r.CostFXRate),
	}
}

// StockStatusFormReq 货行启停（列表行内的快速开关）。
type StockStatusFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Status    string `form:"status"`
}

// Valid 启停只接受两个值。
func (r *StockStatusFormReq) Valid() bool {
	status := strings.TrimSpace(r.Status)
	return status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

// StatusValue 归一后的目标状态。
func (r *StockStatusFormReq) StatusValue() string { return strings.TrimSpace(r.Status) }

// StatusReq 转成 service 的启停请求。
func (r *StockStatusFormReq) StatusReq() *UpdateStockStatusReq {
	return &UpdateStockStatusReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		ID:        strings.TrimSpace(r.ID),
		Status:    r.StatusValue(),
	}
}

// formFloatPtr 表单数字 → 可空浮点。
//
// 空串与「填了 0」是**两件事**（0 是显式的零成本 / 免运费，空是「没填 / 用默认」），
// 所以这里返回指针而不是 float64：把空串当 0 会让「尚未核算」变成「成本为零」。
func formFloatPtr(v string) *float64 {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		// 解析失败也返回 nil：非法输入的拒绝在 service（那里才有统一的错误 key），
		// 本层不重复造一套判定。
		return nil
	}
	return &n
}
