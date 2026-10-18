package inventorydto

import (
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// StockChangeFormReq 库存调整（盘点 / 报损）表单。
//
// 方向和备注的必填判定留在本层：它们是「没有单据承载的写入口」的自证材料，
// service 侧只看归一后的值，控制器不重复判一遍。
type StockChangeFormReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	Direction   string `form:"direction"`
	ReasonCode  string `form:"reasonCode"`
	SourceType  string `form:"sourceType"`
	SourceRef   string `form:"sourceRef"`
	Remark      string `form:"remark"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`
	ProductID   string `form:"productId"`
	Quantity    string `form:"quantity"`
}

// ValidDirection 本条入口只接受盘点（adjust）与报损（out）：入库必须挂采购单 / 生产单。
func (r *StockChangeFormReq) ValidDirection() bool {
	direction := strings.TrimSpace(r.Direction)
	return direction == inventoryenums.DirectionAdjust || direction == inventoryenums.DirectionOut
}

// RemarkValue 归一后的备注（空串 = 没填，调用点据此拒绝）。
func (r *StockChangeFormReq) RemarkValue() string { return strings.TrimSpace(r.Remark) }

// ChangeReq 转成 service 的变动请求（单行：本页一次只调一行）。
func (r *StockChangeFormReq) ChangeReq(operatorID string) *ChangeStockReq {
	return &ChangeStockReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		Direction:   strings.TrimSpace(r.Direction),
		ReasonCode:  strings.TrimSpace(r.ReasonCode),
		SourceType:  strings.TrimSpace(r.SourceType),
		SourceRef:   strings.TrimSpace(r.SourceRef),
		Remark:      strings.TrimSpace(r.Remark),
		OperatorID:  operatorID,
		Lines: []StockChangeLineReq{{
			VariantID: strings.TrimSpace(r.VariantID),
			SKUCode:   strings.TrimSpace(r.SKUCode),
			ProductID: strings.TrimSpace(r.ProductID),
			Quantity:  formInt(r.Quantity, 0),
		}},
	}
}

// ExternalSKUFormReq 库存行内编辑：登记 / 修改 / 清空外部编码。
type ExternalSKUFormReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	VariantID   string `form:"variantId"`
	ExternalSKU string `form:"externalSku"`
}

// BindReq 转成 service 的绑定请求。
//
// externalSku **不在这里加工**：去空白 / 长度上限 / 控制字符归一是 service 的唯一口径，
// 控制器多一道归一就会出现两套规则（而空串是合法值：清空 = 撤销映射）。
func (r *ExternalSKUFormReq) BindReq() *BindExternalSKUReq {
	return &BindExternalSKUReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		VariantID:   strings.TrimSpace(r.VariantID),
		ExternalSKU: r.ExternalSKU,
	}
}

// StockTrackingFormReq 库存行内编辑：批次追踪开关 + 数量。
type StockTrackingFormReq struct {
	ProjectID     string `form:"projectId"`
	WarehouseID   string `form:"warehouseId"`
	VariantID     string `form:"variantId"`
	TrackQuantity string `form:"trackQuantity"`
	Quantity      string `form:"quantity"`
}

// Tracked 复选框语义：有值即为「按数量跟踪」。
func (r *StockTrackingFormReq) Tracked() bool { return formBool(r.TrackQuantity) }

// QuantityValue 归一后的数量原文（空串 = 没填，调用点据此拒绝跟踪态的空数量）。
func (r *StockTrackingFormReq) QuantityValue() string { return strings.TrimSpace(r.Quantity) }

// TrackingReq 转成 service 的开关请求。
func (r *StockTrackingFormReq) TrackingReq() *UpdateStockTrackingReq {
	return &UpdateStockTrackingReq{
		ProjectID:     strings.TrimSpace(r.ProjectID),
		WarehouseID:   strings.TrimSpace(r.WarehouseID),
		VariantID:     strings.TrimSpace(r.VariantID),
		TrackQuantity: r.Tracked(),
		Quantity:      formInt(r.Quantity, 0),
	}
}
