package purchasedto

import (
	"strconv"
	"strings"
)

const purchaseLineSkuField = "lineSku"

type PurchaseCreateFormReq struct {
	ID          string `form:"id"`
	ProjectID   string `form:"projectId"`
	Code        string `form:"code"`
	SourceID    string `form:"sourceId"`
	WarehouseID string `form:"warehouseId"`
	Remark      string `form:"remark"`

	AsDraft bool `form:"asDraft"`

	LineSku       []string `form:"lineSku"`
	LineQuantity  []string `form:"lineQuantity"`
	LineUnitPrice []string `form:"lineUnitPrice"`
}

func (r *PurchaseCreateFormReq) CreateReq(operatorID string) *CreatePurchaseOrderReq {
	lines := make([]PurchaseLineReq, 0, len(r.LineSku))
	for i, raw := range r.LineSku {
		variantID, productID, skuCode := parseSKURef(raw)
		if variantID == "" {
			continue
		}
		line := PurchaseLineReq{
			VariantID: variantID,
			ProductID: productID,
			SKUCode:   skuCode,
			Quantity:  formInt(formArrayAt(r.LineQuantity, i), 0),
			Sort:      i,
		}
		if price, ok := parseFloatOK(formArrayAt(r.LineUnitPrice, i)); ok {
			line.UnitPrice = price
		}
		lines = append(lines, line)
	}
	return &CreatePurchaseOrderReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		Code:        strings.TrimSpace(r.Code),
		SourceID:    strings.TrimSpace(r.SourceID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		Remark:      strings.TrimSpace(r.Remark),
		OperatorID:  operatorID,
		AsDraft:     r.AsDraft,
		Lines:       lines,
	}
}

func (r *PurchaseCreateFormReq) LineAt(i int) (sku, quantity, unitPrice string) {
	return formArrayAtRaw(r.LineSku, i), formArrayAtRaw(r.LineQuantity, i), formArrayAtRaw(r.LineUnitPrice, i)
}

type PurchaseReceiptFormReq struct {
	ProjectID string   `form:"projectId"`
	OrderID   string   `form:"orderId"`
	RequestID string   `form:"requestId"`
	Remark    string   `form:"remark"`
	LineID    []string `form:"lineId"`
	Quantity  []string `form:"quantity"`
	UnitPrice []string `form:"unitPrice"`
}

func (r *PurchaseReceiptFormReq) RegisterReq(operatorID string) *RegisterReceiptReq {
	lines := make([]ReceiptLineReq, 0, len(r.LineID))
	for i, rawLineID := range r.LineID {
		lineID := strings.TrimSpace(rawLineID)
		if lineID == "" {
			continue
		}
		quantity := formInt(formArrayAt(r.Quantity, i), 0)
		if quantity <= 0 {
			continue
		}
		line := ReceiptLineReq{LineID: lineID, Quantity: quantity}
		if price, ok := parseFloatOK(formArrayAt(r.UnitPrice, i)); ok {
			line.UnitPrice = &price
		}
		lines = append(lines, line)
	}
	return &RegisterReceiptReq{
		ProjectID:  strings.TrimSpace(r.ProjectID),
		OrderID:    strings.TrimSpace(r.OrderID),
		RequestID:  strings.TrimSpace(r.RequestID),
		Remark:     strings.TrimSpace(r.Remark),
		OperatorID: operatorID,
		Lines:      lines,
	}
}

type ProductionFormReq struct {
	ProjectID   string `form:"projectId"`
	SourceID    string `form:"sourceId"`
	WarehouseID string `form:"warehouseId"`
	LineSku     string `form:"lineSku"`
	Quantity    string `form:"quantity"`
	UnitCost    string `form:"unitCost"`
	RequestID   string `form:"requestId"`
	Remark      string `form:"remark"`
}

func (r *ProductionFormReq) ProductionReq(operatorID string) *ProductionInboundReq {
	variantID, productID, skuCode := parseSKURef(r.LineSku)
	req := &ProductionInboundReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		SourceID:    strings.TrimSpace(r.SourceID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		VariantID:   variantID,
		ProductID:   productID,
		SKUCode:     skuCode,
		Quantity:    formInt(r.Quantity, 0),
		RequestID:   strings.TrimSpace(r.RequestID),
		Remark:      strings.TrimSpace(r.Remark),
		OperatorID:  operatorID,
	}
	if cost, ok := parseFloatOK(r.UnitCost); ok {
		req.UnitCost = &cost
	}
	return req
}

type PurchaseStatusFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Remark    string `form:"remark"`
}

func (r *PurchaseStatusFormReq) StatusReq() *PurchaseStatusReq {
	return &PurchaseStatusReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		ID:        strings.TrimSpace(r.ID),
		Remark:    strings.TrimSpace(r.Remark),
	}
}

func parseSKURef(raw string) (variantID, productID, skuCode string) {
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

func formArrayAt(values []string, index int) string {
	return strings.TrimSpace(formArrayAtRaw(values, index))
}

func formArrayAtRaw(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

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

func formInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}
