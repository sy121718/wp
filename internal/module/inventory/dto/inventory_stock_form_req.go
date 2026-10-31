package inventorydto

import (
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

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

func (r *StockChangeFormReq) ValidDirection() bool {
	direction := strings.TrimSpace(r.Direction)
	return direction == inventoryenums.DirectionAdjust || direction == inventoryenums.DirectionOut
}

func (r *StockChangeFormReq) RemarkValue() string { return strings.TrimSpace(r.Remark) }

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

type ExternalSKUFormReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	VariantID   string `form:"variantId"`
	ExternalSKU string `form:"externalSku"`
}

func (r *ExternalSKUFormReq) BindReq() *BindExternalSKUReq {
	return &BindExternalSKUReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		VariantID:   strings.TrimSpace(r.VariantID),
		ExternalSKU: r.ExternalSKU,
	}
}

type StockTrackingFormReq struct {
	ProjectID     string `form:"projectId"`
	WarehouseID   string `form:"warehouseId"`
	VariantID     string `form:"variantId"`
	TrackQuantity string `form:"trackQuantity"`
	Quantity      string `form:"quantity"`
}

func (r *StockTrackingFormReq) Tracked() bool { return formBool(r.TrackQuantity) }

func (r *StockTrackingFormReq) QuantityValue() string { return strings.TrimSpace(r.Quantity) }

func (r *StockTrackingFormReq) TrackingReq() *UpdateStockTrackingReq {
	return &UpdateStockTrackingReq{
		ProjectID:     strings.TrimSpace(r.ProjectID),
		WarehouseID:   strings.TrimSpace(r.WarehouseID),
		VariantID:     strings.TrimSpace(r.VariantID),
		TrackQuantity: r.Tracked(),
		Quantity:      formInt(r.Quantity, 0),
	}
}
