package inventorydto

import (
	"strconv"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

type WarehouseSKUFormReq struct {
	ProjectID string `form:"projectId"`

	ID string `form:"id"`

	WarehouseID string `form:"warehouseId"`
	SKUCode     string `form:"skuCode"`
	Name        string `form:"name"`
	ExternalSKU string `form:"externalSku"`
	ImageURL    string `form:"imageUrl"`
	Barcode     string `form:"barcode"`
	Remark      string `form:"remark"`
	Status      string `form:"status"`

	Quantity      string `form:"quantity"`
	TrackQuantity string `form:"trackQuantity"`
	CostPrice     string `form:"costPrice"`
	FreightPrice  string `form:"freightPrice"`
	CostCurrency  string `form:"costCurrency"`

	CostBaseCurrency string `form:"costBaseCurrency"`
	CostFXRate       string `form:"costFxRate"`
}

func (r *WarehouseSKUFormReq) ValidStatus() bool {
	status := strings.TrimSpace(r.Status)
	return status == "" || status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

func (r *WarehouseSKUFormReq) StatusValue() string {
	status := strings.TrimSpace(r.Status)
	if status == "" {
		return inventoryenums.StatusActive
	}
	return status
}

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

type StockStatusFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Status    string `form:"status"`
}

func (r *StockStatusFormReq) Valid() bool {
	status := strings.TrimSpace(r.Status)
	return status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

func (r *StockStatusFormReq) StatusValue() string { return strings.TrimSpace(r.Status) }

func (r *StockStatusFormReq) StatusReq() *UpdateStockStatusReq {
	return &UpdateStockStatusReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		ID:        strings.TrimSpace(r.ID),
		Status:    r.StatusValue(),
	}
}

func formFloatPtr(v string) *float64 {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {

		return nil
	}
	return &n
}
