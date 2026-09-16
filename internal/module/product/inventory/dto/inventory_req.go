// inventory_req.go — inventory 模块入参（inbound 绑定用）。
package inventorydto

import "encoding/json"

// CreateWarehouseReq 新建仓库（验收 1）。
//
// Code 是仓库短码：工程内唯一，且是 SKU 编码的前缀（{仓短码}_{商品码}_{序号}）。
// IsDefault 为真表示同时把它设为该工程的默认仓（「未指定仓库」时的兜底）。
type CreateWarehouseReq struct {
	ProjectID string          `json:"projectId"`
	Code      string          `json:"code" binding:"required"`
	Name      string          `json:"name" binding:"required"`
	IsDefault bool            `json:"isDefault"`
	Status    string          `json:"status"`
	Sort      int             `json:"sort"`
	Metadata  json.RawMessage `json:"metadata"`
}

// UpdateWarehouseReq 修改仓库（逐字段可选；nil = 本次不改）。
type UpdateWarehouseReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string  `json:"projectId"`
	Code      *string `json:"code"`
	Name      *string `json:"name"`
	// IsDefault 置真即「切换默认仓」（同工程唯一）；置假在已是默认仓时被拒绝 ——
	// 取消默认会让「未指定仓库」失去兜底，必须先指定另一个默认仓。
	IsDefault *bool           `json:"isDefault"`
	Status    *string         `json:"status"`
	Sort      *int            `json:"sort"`
	Metadata  json.RawMessage `json:"metadata"`
}

// GetWarehouseReq 按 ID 查询仓库。
type GetWarehouseReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// ListWarehouseReq 仓库列表（工程维度）。
type ListWarehouseReq struct {
	ProjectID string `form:"projectId"`
}

// DeleteWarehouseReq 删除仓库。
type DeleteWarehouseReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
}

// EnsureStockReq 确保某个 SKU 在某仓有一条库存记录（初始 0）。
//
// WarehouseID 为空时兜底到该工程的默认仓；VariantID 必填（库存以 SKU × 仓库 为维度）。
type EnsureStockReq struct {
	ProjectID   string `json:"projectId"`
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode"`
	WarehouseID string `json:"warehouseId"`
}

// GetStockReq 单条库存记录（按 id，或按 变体 × 仓库 定位）。
type GetStockReq struct {
	ID          string `form:"id"`
	VariantID   string `form:"variantId"`
	WarehouseID string `form:"warehouseId"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// ListStockBySKUReq 某 SKU 在各仓的库存（验收 3/4：库存以「SKU × 仓库」为维度）。
type ListStockBySKUReq struct {
	ProjectID string `form:"projectId"`
	SKUCode   string `form:"skuCode" binding:"required"`
}

// ListStockReq 库存记录列表（后台核对用，按需过滤 + 分页）。
type ListStockReq struct {
	ProjectID   string `form:"projectId"`
	WarehouseID string `form:"warehouseId"`
	ProductID   string `form:"productId"`
	VariantID   string `form:"variantId"`
	SKUCode     string `form:"skuCode"`
	Page        int    `form:"page"`
	Size        int    `form:"size"`
}

// —— 库存变动与流水（issue #16）——

// StockChangeLineReq 一条目标变动（SKU × 仓库 维度）。
//
// WarehouseID 为空时逐级兜底：本行 → 请求级 WarehouseID → 该工程的默认仓。
// ProductID / SKUCode 是落库快照（库存表按表隔离约定自带 sku_code 快照列）；
// 目标行已存在时沿用既有值，故只有「首次为新 SKU 建行」才必须给全。
type StockChangeLineReq struct {
	WarehouseID string `json:"warehouseId"`
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode"`
	// Quantity 语义随方向而定：in / out 是正数增减量；adjust 是目标绝对量（>= 0）。
	Quantity int `json:"quantity"`
}

// ChangeStockReq 按 SKU 增减库存（issue #16 验收 1/2/3/4）。
//
// 多行（多 SKU / 多仓）在**同一事务**里整体生效：任一行可用量不足即整体拒绝，
// 不留半截变动；加锁顺序由服务端按 (变体, 仓库) 标识升序固定，与入参顺序无关。
type ChangeStockReq struct {
	ProjectID   string               `json:"projectId"`
	WarehouseID string               `json:"warehouseId"`
	Direction   string               `json:"direction" binding:"required"`
	ReasonCode  string               `json:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType"`
	SourceRef   string               `json:"sourceRef"`
	Remark      string               `json:"remark"`
	OperatorID  string               `json:"operatorId"`
	Lines       []StockChangeLineReq `json:"lines" binding:"required"`
}

// DeductStockReq 按 SKU 扣减库存（issue #16 验收 5：支持按物料清单展开多个子项 SKU）。
//
// ExpandBOM 为真时：入参每个 SKU 若维护了物料清单，就展开成它的子项（用量 × 请求量），
// 递归到**叶子**为止；有清单的 SKU 被展开而不是被扣，因此中间件半成品自身的真源不动。
// 没有清单的 SKU 就是叶子，按自身扣减。展开后的子项集合仍然整体排序加锁、整体生效
// （任一项不足即整批拒绝）。
type DeductStockReq struct {
	ProjectID   string               `json:"projectId"`
	WarehouseID string               `json:"warehouseId"`
	ReasonCode  string               `json:"reasonCode" binding:"required"`
	SourceType  string               `json:"sourceType"`
	SourceRef   string               `json:"sourceRef"`
	Remark      string               `json:"remark"`
	OperatorID  string               `json:"operatorId"`
	ExpandBOM   bool                 `json:"expandBom"`
	Lines       []StockChangeLineReq `json:"lines" binding:"required"`
}

// ListMovementReq 库存流水查询（按工程 / 仓 / 商品 / 变体 / SKU / 方向 / 原因 / 来源过滤 + 分页）。
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
	Page        int    `form:"page"`
	Size        int    `form:"size"`
}

// —— 变动原因字典（issue #16 验收 4）——

// ListReasonReq 变动原因列表。
type ListReasonReq struct {
	ProjectID       string `form:"projectId"`
	Direction       string `form:"direction"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
}

// CreateReasonReq 新建自定义变动原因（内置原因由迁移 103 seed，全工程可见）。
type CreateReasonReq struct {
	ProjectID string `json:"projectId"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Direction string `json:"direction" binding:"required"`
	Sort      int    `json:"sort"`
}

// UpdateReasonReq 修改自定义变动原因（逐字段可选；内置原因一律拒绝）。
type UpdateReasonReq struct {
	ID     string  `json:"id" binding:"required"`
	Name   *string `json:"name"`
	Status *string `json:"status"`
	Sort   *int    `json:"sort"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
}

// —— 物料清单（issue #16 验收 5）——

// BOMItemReq 物料清单的一条子项。
type BOMItemReq struct {
	ComponentVariantID string `json:"componentVariantId" binding:"required"`
	ComponentSKUCode   string `json:"componentSkuCode"`
	Quantity           int    `json:"quantity"`
}

// SetBOMReq 全量替换某个父 SKU 的物料清单（派生物，非追加）。
//
// Items 为空表示**清空**该父 SKU 的清单（之后扣减它就按自身扣）。
type SetBOMReq struct {
	ProjectID       string       `json:"projectId"`
	ParentVariantID string       `json:"parentVariantId" binding:"required"`
	ParentSKUCode   string       `json:"parentSkuCode"`
	Items           []BOMItemReq `json:"items"`
}

// GetBOMReq 查看某个父 SKU 的物料清单。
type GetBOMReq struct {
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID       string `form:"projectId"`
	ParentVariantID string `form:"parentVariantId" binding:"required"`
}

// —— 货源（issue #17 验收 1/2/4）——

// CreateSourceReq 新建货源（外部供应商 / 集团内关联公司 / 自家工厂）。
//
// Type 只认 external / internal；RelatedParty 为 nil 时按类型取默认 ——
// 内部货源恒为关联方（内部交易必须能被关联方报表捕获），外部默认非关联方。
// SettlePrice 是内部结算价，只允许出现在内部货源上。
// Config 是**异构对接扩展信息**（JSON 对象，不同来源字段形状各不相同）。
type CreateSourceReq struct {
	ProjectID    string          `json:"projectId"`
	Code         string          `json:"code" binding:"required"`
	Name         string          `json:"name" binding:"required"`
	Type         string          `json:"type"`
	RelatedParty *bool           `json:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice"`
	Status       string          `json:"status"`
	Config       json.RawMessage `json:"config"`
	Sort         int             `json:"sort"`
	Metadata     json.RawMessage `json:"metadata"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// UpdateSourceReq 修改货源（逐字段可选；nil = 本次不改）。
//
// ClearSettlePrice 显式清空结算价 —— 指针为 nil 表示「不改」，没有它就无法把值改回 NULL。
type UpdateSourceReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID        string          `json:"projectId"`
	Code             *string         `json:"code"`
	Name             *string         `json:"name"`
	Type             *string         `json:"type"`
	RelatedParty     *bool           `json:"relatedParty"`
	SettlePrice      *float64        `json:"settlePrice"`
	ClearSettlePrice bool            `json:"clearSettlePrice"`
	Status           *string         `json:"status"`
	Config           json.RawMessage `json:"config"`
	Sort             *int            `json:"sort"`
	Metadata         json.RawMessage `json:"metadata"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// GetSourceReq 按 ID 查询货源。
type GetSourceReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// DeleteSourceReq 删除货源。
type DeleteSourceReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// ListSourceReq 货源列表（报表区分维度直接落在查询上）。
//
// RelatedParty 是三态字符串："" 全部 / "true" 仅关联方 / "false" 仅非关联方 ——
// 用字符串而不是 *bool，是因为 GET 查询里「参数缺失」与「参数为空」必须能区分开。
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

// SourceSummaryReq 货源关联方统计（按类型 × 关联方分组计数）。
type SourceSummaryReq struct {
	ProjectID string `form:"projectId"`
}

// —— 商品侧缓存同步与对账（issue #16 验收 6/7）——

// SyncStockCacheReq 【已废弃，121 删缓存列】历史 DTO，仅保留类型兼容。

// ReconcileStockCacheReq 缓存对账（可选对齐修复）。
