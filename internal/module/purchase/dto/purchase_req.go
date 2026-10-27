// purchase_req.go — 采购单与入库入参。
package purchasedto

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
// SourceID 是货源（外部供应商 / 集团内关联公司），WarehouseID 是收货仓；
// 两者都必填 —— 采购单的「从谁买」「收进哪个仓」是单据身份的一部分。
//
// AsDraft 为真即「存为草稿」（670）：草稿不可收货，行可继续改，下单是一次显式动作。
// 不传就是直接下单（默认路径不变，老调用方无需改动）。
type CreatePurchaseOrderReq struct {
	ProjectID   string            `json:"projectId" form:"projectId"`
	Code        string            `json:"code" form:"code"`
	SourceID    string            `json:"sourceId" form:"sourceId"`
	WarehouseID string            `json:"warehouseId" form:"warehouseId"`
	ExpectedAt  *utils.JSONTime   `json:"expectedAt" form:"expectedAt"`
	Remark      string            `json:"remark" form:"remark"`
	OperatorID  string            `json:"operatorId" form:"operatorId"`
	AsDraft     bool              `json:"asDraft" form:"asDraft"`
	Metadata    json.RawMessage   `json:"metadata" form:"metadata"`
	Lines       []PurchaseLineReq `json:"lines" form:"lines"`
}

// UpdatePurchaseOrderReq 修改采购单（单头逐字段可选；nil = 本次不改）。
//
// 状态**不可改**：推导态（已下单 / 部分到货 / 已到货）是「已入库数量 与 采购数量」
// 的结果，人工态（草稿 / 异常 / 已完结 / 已取消）只由动作推进 —— 表单里没有状态字段。
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

// PurchaseStatusReq 采购单终态的写动作入参（提前关单 / 取消）。
//
// Remark 是**必填**吗？不是 —— 但页面上会提示填写：一句话说清「为什么提前关单」
// 是这类单据事后唯一能读到的解释（货的数量本身看不出来原因）。
type PurchaseStatusReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ID        string `json:"id" form:"id" binding:"required"`
	Remark    string `json:"remark" form:"remark"`
}

// PurchaseListQueryReq 采购入库页的查询串（筛选 + 分页 + 工程）。
//
// 键名与模板里的筛选栏 / 分页条一致：三处（表单、链接、这里的 tag）对不上时，
// 症状是「筛选后翻页丢条件」，页面不报错、日志也干净。
type PurchaseListQueryReq struct {
	ProjectID string `form:"project"`
	Status    string `form:"status"`
	SourceID  string `form:"sourceId"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}
