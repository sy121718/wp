package productdto

import json "go_wp/pkg/jsonx"

// product_variant_req.go / product_variant_resp.go — 变体清单、组合预览与批量保存的入参与回执。

// PreviewVariantReq 组合生成预览（不落库）。
//
// Selections 为空 = 「生成全部组合」（与 GenerateVariants 的无表单路径同一语义：
// 全部参与变体的属性组 × 全部启用值）。
//
// ExistingOptionValues 是前端清单里已经有的组合（每行的 option_values 原样回传）：
// 服务端按自己的 optionKey 口径归一后与本次组合比对，命中的不再返回 ——
// 这样「重复点生成不重复追加」的判据只有服务端一份（前端不需要复刻 optionKey）。
type PreviewVariantReq struct {
	ProductID string                `json:"productId" form:"productId" binding:"required"`
	ProjectID string                `json:"projectId" form:"projectId"`
	Selections []VariantSelectionReq `json:"selections" form:"selections"`
	// WarehouseID 归属仓（issue #15）：与生成路径同一口径，仅用于主体 SKU 的仓码前缀。
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	// ExistingOptionValues 前端清单里每行的 option_values（JSON 对象）。
	ExistingOptionValues []json.RawMessage `json:"existingOptionValues"`
}

// SaveVariantListReq 以清单为准保存变体（唯一落库动作）。
type SaveVariantListReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId" form:"projectId"`
	// WarehouseID 新增行的归属仓（issue #15）：不选即兜底该工程的默认仓；
	// 既有变体不受影响（它们的库存记录早已存在）。
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	// Rows 清单全部行，顺序即期望的展示顺序。
	Rows []VariantListResp `json:"rows" form:"rows"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略。
	OperatorID string `json:"-" form:"-"`
}
