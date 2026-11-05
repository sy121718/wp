package inventorydto

import json "go_wp/pkg/jsonx"

type WarehouseThirdPartyReq struct {
	Provider       string `json:"provider" form:"provider"`
	ExternalCode   string `json:"externalCode" form:"externalCode"`
	Address        string `json:"address" form:"address"`
	Contact        string `json:"contact" form:"contact"`
	AllowsShipping bool   `json:"allowsShipping" form:"allowsShipping"`

	APICredential string `json:"apiCredential" form:"apiCredential"`

	ClearCredential bool `json:"clearCredential" form:"clearCredential"`

	SecretRef string `json:"secretRef" form:"secretRef"`
}

type CreateWarehouseReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Code      string `json:"code" form:"code" binding:"required"`
	Name      string `json:"name" form:"name" binding:"required"`

	Type      string `json:"type" form:"type"`
	IsDefault bool   `json:"isDefault" form:"isDefault"`
	Status    string `json:"status" form:"status"`
	Sort      int    `json:"sort" form:"sort"`

	DeductPriority int             `json:"deductPriority" form:"deductPriority"`
	Metadata       json.RawMessage `json:"metadata" form:"metadata"`

	ThirdParty *WarehouseThirdPartyReq `json:"thirdParty" form:"thirdParty"`
}

type UpdateWarehouseReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string  `json:"projectId" form:"projectId"`
	Code      *string `json:"code" form:"code"`
	Name      *string `json:"name" form:"name"`

	IsDefault *bool   `json:"isDefault" form:"isDefault"`
	Status    *string `json:"status" form:"status"`
	Sort      *int    `json:"sort" form:"sort"`

	DeductPriority *int `json:"deductPriority" form:"deductPriority"`

	Type     *string         `json:"type" form:"type"`
	Metadata json.RawMessage `json:"metadata" form:"metadata"`

	ThirdParty *WarehouseThirdPartyReq `json:"thirdParty" form:"thirdParty"`
}

type GetWarehouseReq struct {
	ID string `form:"id" binding:"required"`

	ProjectID string `form:"projectId"`
}

type ListWarehouseReq struct {
	ProjectID string `form:"projectId"`
}

type DeleteWarehouseReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`
}

// BulkDeleteWarehouseReq 批量删除仓库：整批一个事务，任一失败全量回滚。
type BulkDeleteWarehouseReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`

	ProjectID string `json:"projectId" form:"projectId"`
}

// BulkReasonStatusReq 批量改原因启停：整批一个事务，任一失败全量回滚。
type BulkReasonStatusReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`

	ProjectID string `json:"projectId" form:"projectId"`

	Status string `json:"status" form:"status" binding:"required"`
}

type EnsureStockReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	ProductID   string `json:"productId" form:"productId"`
	VariantID   string `json:"variantId" form:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode" form:"skuCode"`
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
}

type UpdateStockTrackingReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	WarehouseID string `json:"warehouseId" form:"warehouseId" binding:"required"`

	SKUCode   string `json:"skuCode" form:"skuCode"`
	VariantID string `json:"variantId" form:"variantId"`

	TrackQuantity bool `json:"trackQuantity" form:"trackQuantity"`

	Quantity int `json:"quantity" form:"quantity"`
}

type GetStockReq struct {
	ID          string `form:"id"`
	VariantID   string `form:"variantId"`
	WarehouseID string `form:"warehouseId"`

	ProjectID string `form:"projectId"`
}

type ListStockBySKUReq struct {
	ProjectID string `form:"projectId"`
	SKUCode   string `form:"skuCode" binding:"required"`
}

type ListStockReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	ProductID   string `form:"productId"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`

	ExternalSKU string `form:"externalSku"`
	Page        int    `form:"page"`
	Size        int    `form:"size"`
}

type ListWarehouseSKUReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	Keyword     string `form:"keyword"`

	Status string `form:"status"`
	Page   int    `form:"page"`
	Size   int    `form:"size"`
}

type GetWarehouseSKUReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId" binding:"required"`
	SKUCode     string `form:"skuCode" binding:"required"`
}

type BindExternalSKUReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	WarehouseID string `json:"warehouseId" form:"warehouseId" binding:"required"`
	VariantID   string `json:"variantId" form:"variantId" binding:"required"`
	ExternalSKU string `json:"externalSku" form:"externalSku"`
}

type CreateStockReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	WarehouseID string `json:"warehouseId" form:"warehouseId" binding:"required"`

	SKUCode       string   `json:"skuCode" form:"skuCode" binding:"required"`
	ExternalSKU   string   `json:"externalSku" form:"externalSku"`
	Quantity      int      `json:"quantity" form:"quantity"`
	TrackQuantity bool     `json:"trackQuantity" form:"trackQuantity"`
	CostPrice     *float64 `json:"costPrice" form:"costPrice"`

	Name             string   `json:"name" form:"name"`
	ImageURL         string   `json:"imageUrl" form:"imageUrl"`
	Barcode          string   `json:"barcode" form:"barcode"`
	Remark           string   `json:"remark" form:"remark"`
	Status           string   `json:"status" form:"status"`
	FreightPrice     *float64 `json:"freightPrice" form:"freightPrice"`
	CostCurrency     string   `json:"costCurrency" form:"costCurrency"`
	CostBaseCurrency string   `json:"costBaseCurrency" form:"costBaseCurrency"`
	CostFXRate       *float64 `json:"costFxRate" form:"costFxRate"`
}

type UpdateStockReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ID        string `json:"id" form:"id" binding:"required"`

	Name             string   `json:"name" form:"name"`
	ExternalSKU      string   `json:"externalSku" form:"externalSku"`
	ImageURL         string   `json:"imageUrl" form:"imageUrl"`
	Barcode          string   `json:"barcode" form:"barcode"`
	Remark           string   `json:"remark" form:"remark"`
	Status           string   `json:"status" form:"status"`
	CostPrice        *float64 `json:"costPrice" form:"costPrice"`
	FreightPrice     *float64 `json:"freightPrice" form:"freightPrice"`
	CostCurrency     string   `json:"costCurrency" form:"costCurrency"`
	CostBaseCurrency string   `json:"costBaseCurrency" form:"costBaseCurrency"`
	CostFXRate       *float64 `json:"costFxRate" form:"costFxRate"`
}

type UpdateStockStatusReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ID        string `json:"id" form:"id" binding:"required"`
	Status    string `json:"status" form:"status" binding:"required"`
}

type ReserveStockLine struct {
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode"`
	Quantity    int    `json:"quantity"`
	WarehouseID string `json:"warehouseId"`
}

type ReserveStockReq struct {
	ProjectID  string             `json:"projectId" form:"projectId"`
	SourceType string             `json:"sourceType" form:"sourceType"`
	SourceRef  string             `json:"sourceRef" form:"sourceRef"`
	Remark     string             `json:"remark" form:"remark"`
	Lines      []ReserveStockLine `json:"lines" form:"lines" binding:"required"`
}

type StockChangeLineReq struct {
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	ProductID   string `json:"productId" form:"productId"`
	VariantID   string `json:"variantId" form:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode" form:"skuCode"`

	Quantity int `json:"quantity" form:"quantity"`

	CostPrice *float64 `json:"costPrice" form:"costPrice"`
}

type ChangeStockReq struct {
	ProjectID   string               `json:"projectId" form:"projectId"`
	WarehouseID string               `json:"warehouseId" form:"warehouseId"`
	Direction   string               `json:"direction" form:"direction" binding:"required"`
	ReasonCode  string               `json:"reasonCode" form:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType" form:"sourceType"`
	SourceRef   string               `json:"sourceRef" form:"sourceRef"`
	Remark      string               `json:"remark" form:"remark"`
	OperatorID  string               `json:"operatorId" form:"operatorId"`
	Lines       []StockChangeLineReq `json:"lines" form:"lines" binding:"required"`
}

type DeductStockReq struct {
	ProjectID   string               `json:"projectId" form:"projectId"`
	WarehouseID string               `json:"warehouseId" form:"warehouseId"`
	ReasonCode  string               `json:"reasonCode" form:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType" form:"sourceType"`
	SourceRef   string               `json:"sourceRef" form:"sourceRef"`
	Remark      string               `json:"remark" form:"remark"`
	OperatorID  string               `json:"operatorId" form:"operatorId"`
	Lines       []StockChangeLineReq `json:"lines" form:"lines" binding:"required"`
}

type ListMovementReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	ProductID   string `form:"productId"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`
	Direction   string `form:"direction"`
	ReasonCode  string `form:"reasonCode"`
	SourceType  string `form:"sourceType"`
	SourceRef   string `form:"sourceRef"`
	BatchID     string `form:"batchId"`

	TimeFrom string `form:"timeFrom"`
	TimeTo   string `form:"timeTo"`
	Page     int    `form:"page"`
	Size     int    `form:"size"`
}

type MovementPageReq struct {
	ProjectID   string `form:"project"`
	SKUCode     string `form:"sku"`
	WarehouseID string `form:"warehouseId"`
	Direction   string `form:"direction"`
	ReasonCode  string `form:"reasonCode"`
	TimeFrom    string `form:"timeFrom"`
	TimeTo      string `form:"timeTo"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
}

type ListReasonReq struct {
	ProjectID       string `form:"projectId"`
	Direction       string `form:"direction"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
}

type CreateReasonReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Code      string `json:"code" form:"code" binding:"required"`
	Name      string `json:"name" form:"name" binding:"required"`
	Direction string `json:"direction" form:"direction" binding:"required"`
	Sort      int    `json:"sort" form:"sort"`
}

type UpdateReasonReq struct {
	ID     string  `json:"id" form:"id" binding:"required"`
	Name   *string `json:"name" form:"name"`
	Status *string `json:"status" form:"status"`
	Sort   *int    `json:"sort" form:"sort"`

	ProjectID string `json:"projectId" form:"projectId"`
}

type CreateSourceReq struct {
	ProjectID    string          `json:"projectId" form:"projectId"`
	Code         string          `json:"code" form:"code" binding:"required"`
	Name         string          `json:"name" form:"name" binding:"required"`
	Type         string          `json:"type" form:"type"`
	RelatedParty *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice" form:"settlePrice"`
	Status       string          `json:"status" form:"status"`
	Config       json.RawMessage `json:"config" form:"config"`
	Sort         int             `json:"sort" form:"sort"`
	Metadata     json.RawMessage `json:"metadata" form:"metadata"`

	OperatorID string `json:"-"`
}

type UpdateSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID        string          `json:"projectId" form:"projectId"`
	Code             *string         `json:"code" form:"code"`
	Name             *string         `json:"name" form:"name"`
	Type             *string         `json:"type" form:"type"`
	RelatedParty     *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice      *float64        `json:"settlePrice" form:"settlePrice"`
	ClearSettlePrice bool            `json:"clearSettlePrice" form:"clearSettlePrice"`
	Status           *string         `json:"status" form:"status"`
	Config           json.RawMessage `json:"config" form:"config"`
	Sort             *int            `json:"sort" form:"sort"`
	Metadata         json.RawMessage `json:"metadata" form:"metadata"`

	OperatorID string `json:"-"`
}

type GetSourceReq struct {
	ID string `form:"id" binding:"required"`

	ProjectID string `form:"projectId"`
}

type DeleteSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	OperatorID string `json:"-"`
}

type ListSourceReq struct {
	ProjectID       string `form:"projectId"`
	Type            string `form:"type"`
	RelatedParty    string `form:"relatedParty"`
	Status          string `form:"status"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
	Page            int    `form:"page"`
	Size            int    `form:"size"`
}

type SourceSummaryReq struct {
	ProjectID string `form:"projectId"`
}

type StockAlertQueryReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
}
