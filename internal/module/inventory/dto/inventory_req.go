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
	ID   string  `json:"id" binding:"required"`
	Code *string `json:"code"`
	Name *string `json:"name"`
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
}

// ListWarehouseReq 仓库列表（工程维度）。
type ListWarehouseReq struct {
	ProjectID string `form:"projectId"`
}

// DeleteWarehouseReq 删除仓库。
type DeleteWarehouseReq struct {
	ID string `json:"id" binding:"required"`
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
