// inventory_resp.go — inventory 模块出参。
package inventorydto

// WarehouseResp 仓库（含默认仓标记）。
type WarehouseResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	IsDefault bool   `json:"isDefault"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// StockResp 库存记录（带上仓库展示信息，后台不必二次查询）。
//
// Quantity 是**真源**可用量：直接读 inventory_stocks.quantity，
// 绝不读 product_variants.stock_total（那是列表展示用的冗余缓存）。
type StockResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`
	Quantity      int    `json:"quantity"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// —— 库存变动与流水（issue #16）——

// MovementResp 库存流水一行（含仓库与原因的展示信息，后台不必二次查询）。
type MovementResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`
	Direction     string `json:"direction"`
	// Quantity 是本次变动的绝对量（恒 > 0）；Delta 是带符号的实际变化量。
	Quantity       int    `json:"quantity"`
	Delta          int    `json:"delta"`
	QuantityBefore int    `json:"quantityBefore"`
	QuantityAfter  int    `json:"quantityAfter"`
	ReasonID       string `json:"reasonId"`
	ReasonCode     string `json:"reasonCode"`
	ReasonName     string `json:"reasonName"`
	// ParentVariantID 非空表示这条流水是「按物料清单展开」出的子项扣减。
	ParentVariantID string `json:"parentVariantId"`
	SourceType      string `json:"sourceType"`
	SourceRef       string `json:"sourceRef"`
	Remark          string `json:"remark"`
	OperatorID      string `json:"operatorId"`
	BatchID         string `json:"batchId"`
	CreatedAt       string `json:"createdAt"`
}

// StockChangeResp 一次库存变动（或一次 BOM 展开扣减）的结果。
//
// CacheTotals / CacheSynced / CacheFailures 描述的是**提交之后**的缓存同步：
// 同步失败不影响主流程（真源已经落库），失败项由对账兜底。
type StockChangeResp struct {
	BatchID       string          `json:"batchId"`
	Direction     string          `json:"direction"`
	Movements     []*MovementResp `json:"movements"`
	CacheTotals   map[string]int  `json:"cacheTotals"`
	CacheSynced   bool            `json:"cacheSynced"`
	CacheFailures []string        `json:"cacheFailures"`
}

// —— 变动原因字典（issue #16 验收 4）——

// ReasonResp 变动原因（内置 + 自定义）。
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

// —— 物料清单（issue #16 验收 5）——

// BOMItemResp 物料清单的一条子项。
type BOMItemResp struct {
	ID                 string `json:"id"`
	ComponentVariantID string `json:"componentVariantId"`
	ComponentSKUCode   string `json:"componentSkuCode"`
	Quantity           int    `json:"quantity"`
}

// BOMResp 某个父 SKU 的物料清单。
type BOMResp struct {
	ParentVariantID string         `json:"parentVariantId"`
	ParentSKUCode   string         `json:"parentSkuCode"`
	ProjectID       string         `json:"projectId"`
	Items           []*BOMItemResp `json:"items"`
	UpdatedAt       string         `json:"updatedAt"`
}

// —— 商品侧缓存同步与对账（issue #16 验收 6/7）——

// CacheSyncItemResp 单个变体的缓存同步结果（带时间戳）。
type CacheSyncItemResp struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	TrueTotal   int    `json:"trueTotal"`
	CachedTotal int    `json:"cachedTotal"`
	SyncedAt    string `json:"syncedAt"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

// SyncStockCacheResp 缓存同步结果（Synced + Failed == len(Items)）。
type SyncStockCacheResp struct {
	ProjectID string               `json:"projectId"`
	Items     []*CacheSyncItemResp `json:"items"`
	Synced    int                  `json:"synced"`
	Failed    int                  `json:"failed"`
}

// ReconcileItemResp 对账明细的一行。
type ReconcileItemResp struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	TrueTotal   int    `json:"trueTotal"`
	CachedTotal int    `json:"cachedTotal"`
	Differed    bool   `json:"differed"`
	Repaired    bool   `json:"repaired"`
	RepairError string `json:"repairError"`
}

// ReconcileStockCacheResp 对账结果（Total == Matched + Differed）。
type ReconcileStockCacheResp struct {
	ProjectID string               `json:"projectId"`
	Total     int                  `json:"total"`
	Matched   int                  `json:"matched"`
	Differed  int                  `json:"differed"`
	Repaired  int                  `json:"repaired"`
	Items     []*ReconcileItemResp `json:"items"`
}
