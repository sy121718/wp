// inventory_purchase_req.go — 采购单与入库入参（issue #18）。
package inventorydto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// PurchaseLineReq 一条采购行（SKU × 采购数量 × 采购单价）。
//
// ProductID / SKUCode 是落库快照列（与库存真源同一口径：本模块按 SKU 取数，
// 不必 JOIN 商品模块的表）；目标变体已存在时服务端会用商品模块给的值兜底校验。
type PurchaseLineReq struct {
	VariantID string  `json:"variantId" form:"variantId" binding:"required"`
	ProductID string  `json:"productId" form:"productId"`
	SKUCode   string  `json:"skuCode" form:"skuCode"`
	Quantity  int     `json:"quantity" form:"quantity"`
	UnitPrice float64 `json:"unitPrice" form:"unitPrice"`
	Sort      int     `json:"sort" form:"sort"`
	Remark    string  `json:"remark" form:"remark"`
}

// CreatePurchaseOrderReq 新建采购单（单头 + 结构化行，验收 1）。
//
// SourceID 是 #17 的货源（外部供应商 / 集团内关联公司），WarehouseID 是收货仓；
// 两者都必填 —— 采购单的「从谁买」「收进哪个仓」是单据身份的一部分。
type CreatePurchaseOrderReq struct {
	ProjectID   string            `json:"projectId" form:"projectId"`
	Code        string            `json:"code" form:"code"`
	SourceID    string            `json:"sourceId" form:"sourceId"`
	WarehouseID string            `json:"warehouseId" form:"warehouseId"`
	ExpectedAt  *utils.JSONTime   `json:"expectedAt" form:"expectedAt"`
	Remark      string            `json:"remark" form:"remark"`
	OperatorID  string            `json:"operatorId" form:"operatorId"`
	Metadata    json.RawMessage   `json:"metadata" form:"metadata"`
	Lines       []PurchaseLineReq `json:"lines" form:"lines"`
}

// UpdatePurchaseOrderReq 修改采购单（单头逐字段可选；nil = 本次不改）。
//
// 状态**不可改**：它是「已入库数量 与 采购数量」的推导值，没有任何人工置位入口。
// ReplaceLines 为真时 Lines 全量替换 —— 只允许在「一行都还没入库」时替换
// （已有入库数量的行被改小会让「已入库 ≤ 采购数量」不成立）。
type UpdatePurchaseOrderReq struct {
	ID              string            `json:"id" form:"id" binding:"required"`
	ProjectID       string            `json:"projectId" form:"projectId"`
	SourceID        *string           `json:"sourceId" form:"sourceId"`
	WarehouseID     *string           `json:"warehouseId" form:"warehouseId"`
	ExpectedAt      *utils.JSONTime   `json:"expectedAt" form:"expectedAt"`
	ClearExpectedAt bool              `json:"clearExpectedAt" form:"clearExpectedAt"`
	Remark          *string           `json:"remark" form:"remark"`
	OperatorID      *string           `json:"operatorId" form:"operatorId"`
	Metadata        json.RawMessage   `json:"metadata" form:"metadata"`
	ReplaceLines    bool              `json:"replaceLines" form:"replaceLines"`
	Lines           []PurchaseLineReq `json:"lines" form:"lines"`
}

// GetPurchaseOrderReq 按 ID 查询采购单（含全部采购行）。
//
// ProjectID 是工程隔离（DB-009）的作用域来源：采购单按工程隔离，读取必须落在
// 某个工程上。为空时由 service 走「唯一工程」兜底，多工程部署下会明确报参数错误
// （好过静默读到别的工程的采购单）。
type GetPurchaseOrderReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

// ListPurchaseOrderReq 采购单列表（状态 / 货源 / 关键词都是可组合的筛选维度）。
type ListPurchaseOrderReq struct {
	ProjectID string `form:"projectId"`
	Status    string `form:"status"`
	SourceID  string `form:"sourceId"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

// ReceiptLineReq 一条采购收货行（按采购行登记，支持分批累加）。
//
// UnitPrice 为空表示沿用采购行上的单价；显式给出即「本批实际到货价」，
// 会同时写进入库单行快照并更新 SKU 成本价。
type ReceiptLineReq struct {
	LineID    string   `json:"lineId" form:"lineId" binding:"required"`
	Quantity  int      `json:"quantity" form:"quantity"`
	UnitPrice *float64 `json:"unitPrice" form:"unitPrice"`
}

// RegisterReceiptReq 登记采购收货（验收 3/4）：一次可收多行，按行累加已入库数量。
//
// RequestID 是**幂等键**：同一个键重复提交命中既有入库单并原样返回，
// 不会第二次动库存（可重放保护）；不传键时每次提交都是一张新的入库单。
type RegisterReceiptReq struct {
	ProjectID   string           `json:"projectId" form:"projectId"`
	OrderID     string           `json:"orderId" form:"orderId" binding:"required"`
	WarehouseID string           `json:"warehouseId" form:"warehouseId"`
	RequestID   string           `json:"requestId" form:"requestId"`
	Remark      string           `json:"remark" form:"remark"`
	OperatorID  string           `json:"operatorId" form:"operatorId"`
	ReceivedAt  *utils.JSONTime  `json:"receivedAt" form:"receivedAt"`
	Lines       []ReceiptLineReq `json:"lines" form:"lines" binding:"required"`
}

// ProductionInboundReq 自家工厂生产入库（验收 5）：无采购单，成本价手工填写。
//
// SourceID 必须是**内部货源**（自家工厂 / 集团内关联公司）—— 外部供应商没有
// 「无采购单的生产入库」这一说，走采购单。
type ProductionInboundReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	SourceID    string `json:"sourceId" form:"sourceId" binding:"required"`
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	ProductID   string `json:"productId" form:"productId"`
	VariantID   string `json:"variantId" form:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode" form:"skuCode"`
	Quantity    int    `json:"quantity" form:"quantity"`
	// UnitCost 是手工填写的成本价（生产入库没有采购单价可引用）。
	UnitCost   *float64        `json:"unitCost" form:"unitCost"`
	RequestID  string          `json:"requestId" form:"requestId"`
	Remark     string          `json:"remark" form:"remark"`
	OperatorID string          `json:"operatorId" form:"operatorId"`
	ReceivedAt *utils.JSONTime `json:"receivedAt" form:"receivedAt"`
}

// ListPurchaseHistoryReq 某 SKU 的进货历史（验收 6）。
//
// 维度是 SKU（skuCode 或 variantId 二选一），可按货源 / 采购单收窄；
// 数据来源是入库单行（含单价快照），不是库存流水 —— 流水没有单价。
type ListPurchaseHistoryReq struct {
	ProjectID string `form:"projectId"`
	SKUCode   string `form:"skuCode"`
	VariantID string `form:"variantId"`
	SourceID  string `form:"sourceId"`
	OrderID   string `form:"orderId"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}
