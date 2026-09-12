// inventory_resp.go — inventory 模块出参。
package inventorydto

import "encoding/json"

// SourceResp 货源（含类型与关联方标志，两者共同构成报表区分维度）。
//
// SettlePrice 仅内部货源有值（自产商品的成本口径）；Config 是异构对接扩展信息。
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

// SourceSummaryGroupResp 一个「类型 × 关联方」分组的计数。
type SourceSummaryGroupResp struct {
	Type         string `json:"type"`
	RelatedParty bool   `json:"relatedParty"`
	Count        int64  `json:"count"`
}

// SourceSummaryResp 货源关联方统计（验收 4「关联方标志可用于报表区分」的数据出口）。
//
// Internal / External 是类型口径，RelatedParty / Unrelated 是关联交易口径 ——
// 两组数字交叉即「内部且关联」「外部但关联」等组合，Groups 给出完整交叉表。
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
