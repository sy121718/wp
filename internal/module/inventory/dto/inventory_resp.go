package inventorydto

import json "go_wp/pkg/jsonx"

type SourceResp struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	RelatedParty bool            `json:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice"`
	Status       string          `json:"status"`
	Config       json.RawMessage `json:"config"`
	Sort         int             `json:"sort"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

type SourceSummaryGroupResp struct {
	Type         string `json:"type"`
	RelatedParty bool   `json:"relatedParty"`
	Count        int64  `json:"count"`
}

type SourceSummaryResp struct {
	ProjectID    string                    `json:"projectId"`
	Total        int64                     `json:"total"`
	Internal     int64                     `json:"internal"`
	External     int64                     `json:"external"`
	RelatedParty int64                     `json:"relatedParty"`
	Unrelated    int64                     `json:"unrelated"`
	SettlePriced int64                     `json:"settlePriced"`
	Groups       []*SourceSummaryGroupResp `json:"groups"`
}

type WarehouseThirdPartyResp struct {
	Provider       string `json:"provider"`
	ExternalCode   string `json:"externalCode"`
	Address        string `json:"address"`
	Contact        string `json:"contact"`
	AllowsShipping bool   `json:"allowsShipping"`

	HasCredential bool `json:"hasCredential"`

	CredentialMasked string `json:"credentialMasked"`

	SecretRef string `json:"secretRef"`

	Extras map[string]any `json:"extras,omitempty"`
}

type WarehouseResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	IsDefault bool   `json:"isDefault"`
	Sort      int    `json:"sort"`

	DeductPriority int `json:"deductPriority"`

	ThirdParty *WarehouseThirdPartyResp `json:"thirdParty,omitempty"`
	CreatedAt  string                   `json:"createdAt"`
	UpdatedAt  string                   `json:"updatedAt"`
}

type StockResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`

	ExternalSKU string `json:"externalSku"`

	TrackQuantity bool `json:"trackQuantity"`
	Quantity      int  `json:"quantity"`

	ReservedQuantity int `json:"reservedQuantity"`

	AvailableQuantity int `json:"availableQuantity"`

	CostPrice *float64 `json:"costPrice"`

	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
	Barcode  string `json:"barcode"`
	Remark   string `json:"remark"`
	Status   string `json:"status"`

	FreightPrice *float64 `json:"freightPrice"`

	CostCurrency     string   `json:"costCurrency"`
	CostBaseCurrency string   `json:"costBaseCurrency"`
	CostFXRate       *float64 `json:"costFxRate"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type StockReserveResp struct {
	ProjectID  string                  `json:"projectId"`
	SourceType string                  `json:"sourceType"`
	SourceRef  string                  `json:"sourceRef"`
	Lines      []*StockReserveLineResp `json:"lines"`
}

type StockReserveLineResp struct {
	StockID           string `json:"stockId"`
	WarehouseID       string `json:"warehouseId"`
	WarehouseCode     string `json:"warehouseCode"`
	VariantID         string `json:"variantId"`
	SKUCode           string `json:"skuCode"`
	Delta             int    `json:"delta"`
	ReservedQuantity  int    `json:"reservedQuantity"`
	Quantity          int    `json:"quantity"`
	AvailableQuantity int    `json:"availableQuantity"`
}

type WarehouseStockResp struct {
	WarehouseID   string   `json:"warehouseId"`
	WarehouseCode string   `json:"warehouseCode"`
	WarehouseName string   `json:"warehouseName"`
	SKUCode       string   `json:"skuCode"`
	TrackQuantity bool     `json:"trackQuantity"`
	Quantity      int      `json:"quantity"`
	CostPrice     *float64 `json:"costPrice"`
}

type ProductWarehouseStock struct {
	ProductID     string   `json:"productId"`
	WarehouseID   string   `json:"warehouseId"`
	WarehouseCode string   `json:"warehouseCode"`
	WarehouseName string   `json:"warehouseName"`
	SKUCode       string   `json:"skuCode"`
	TrackQuantity bool     `json:"trackQuantity"`
	Quantity      int      `json:"quantity"`
	CostPrice     *float64 `json:"costPrice"`
}

type WarehouseSKUResp struct {
	ID            string `json:"id"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	IsDefault     bool   `json:"isDefault"`

	SKUCode string `json:"skuCode"`

	ExternalSKU string `json:"externalSku"`

	ProductID string `json:"productId"`
	VariantID string `json:"variantId"`

	HasVariant bool `json:"hasVariant"`

	Quantity          int `json:"quantity"`
	ReservedQuantity  int `json:"reservedQuantity"`
	AvailableQuantity int `json:"availableQuantity"`

	TrackQuantity bool `json:"trackQuantity"`

	CostPrice        *float64 `json:"costPrice"`
	FreightPrice     *float64 `json:"freightPrice"`
	CostCurrency     string   `json:"costCurrency"`
	CostBaseCurrency string   `json:"costBaseCurrency"`
	CostFXRate       *float64 `json:"costFxRate"`
	Name             string   `json:"name"`
	ImageURL         string   `json:"imageUrl"`
	Barcode          string   `json:"barcode"`
	Remark           string   `json:"remark"`
	Status           string   `json:"status"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

type MovementResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`

	StockID string `json:"stockId"`

	StockName     string `json:"stockName"`
	StockImageURL string `json:"stockImageUrl"`
	SKUCode       string `json:"skuCode"`
	Direction     string `json:"direction"`

	Quantity       int    `json:"quantity"`
	Delta          int    `json:"delta"`
	QuantityBefore int    `json:"quantityBefore"`
	QuantityAfter  int    `json:"quantityAfter"`
	ReasonID       string `json:"reasonId"`
	ReasonCode     string `json:"reasonCode"`
	ReasonName     string `json:"reasonName"`
	SourceType     string `json:"sourceType"`
	SourceRef      string `json:"sourceRef"`
	Remark         string `json:"remark"`
	OperatorID     string `json:"operatorId"`
	BatchID        string `json:"batchId"`

	UnitCost  *float64 `json:"unitCost"`
	CreatedAt string   `json:"createdAt"`
}

type StockChangeResp struct {
	BatchID       string          `json:"batchId"`
	Direction     string          `json:"direction"`
	Movements     []*MovementResp `json:"movements"`
	CacheTotals   map[string]int  `json:"cacheTotals"`
	CacheSynced   bool            `json:"cacheSynced"`
	CacheFailures []string        `json:"cacheFailures"`
}

type ReasonResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	IsBuiltin bool   `json:"isBuiltin"`
	Status    string `json:"status"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ReconcileItemResp struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	TrueTotal   int    `json:"trueTotal"`
	CachedTotal int    `json:"cachedTotal"`
	Differed    bool   `json:"differed"`
	Repaired    bool   `json:"repaired"`
	RepairError string `json:"repairError"`
}

type VariantWarehouseCostRef struct {
	VariantID   string `json:"variantId"`
	WarehouseID string `json:"warehouseId"`
}

type VariantWarehouseCost struct {
	VariantID   string `json:"variantId"`
	WarehouseID string `json:"warehouseId"`

	CostPrice *float64 `json:"costPrice"`
}

type StockAlertResp struct {
	ProjectID  string `json:"projectId"`
	Total      int64  `json:"total"`
	OutOfStock int64  `json:"outOfStock"`
	LowStock   int64  `json:"lowStock"`
	Alert      int64  `json:"alert"`
}
