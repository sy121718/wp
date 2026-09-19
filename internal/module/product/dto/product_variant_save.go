// product_variant_save.go — 变体清单的「预览—保存」模型（docs/14 §8，2026-09-19 用户拍板）。
//
// 用户口径：「变体 sku 也是一样系统生产 + 可编辑，生产只是显示，并不会存入数据库，
// 必须保存才行，所以删除不是删除，是相当于清除前端显示，不存入数据库」。
//
// 三个动作的语义（对照表见 docs/14 §8）：
//
//	勾选属性值 → 生成（PreviewVariantReq / PreviewVariantCombinations）
//	    **不落库**：把所选值的笛卡尔积追加进前端清单（按组合去重），SKU 由服务端生成、可逐行编辑；
//	删除某行
//	    只是从**前端清单**里移出，库里什么都没变（前端行为，服务端无对应接口）；
//	保存（SaveVariantListReq / SaveVariantList）
//	    **以清单为准落库**：新增清单里库里没有的、更新被编辑过的 SKU、
//	    删除库里存在但清单里没有的既有变体（有库存 / 被引用时跳过逐条回带）。
//
// 本文件只放形状；判定与落库在 service（product_variant_generate.go）。
package productdto

import "encoding/json"

// VariantListRow 前端清单里的一行（保存请求的输入）。
//
// VariantID 为空 = 清单里的**新增行**（库里还没有这个组合）：
// 它的 OptionValues 必须通过服务端重算校验（属性组属于该商品、取值在组内）；
// VariantID 非空 = 清单里保留的**既有变体**：组合以库里的 option_values 为准
// （前端传的 OptionValues 不参与判定），只有 SKUCode 允许被编辑。
type VariantListRow struct {
	VariantID    string          `json:"variantId"`
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
}

// PreviewVariantReq 组合生成预览（不落库）。
//
// Selections 为空 = 「生成全部组合」（与 GenerateVariants 的无表单路径同一语义：
// 全部参与变体的属性组 × 全部启用值）。
//
// ExistingOptionValues 是前端清单里已经有的组合（每行的 option_values 原样回传）：
// 服务端按自己的 optionKey 口径归一后与本次组合比对，命中的不再返回 ——
// 这样「重复点生成不重复追加」的判据只有服务端一份（前端不需要复刻 optionKey）。
type PreviewVariantReq struct {
	ProductID  string                `json:"productId" binding:"required"`
	ProjectID  string                `json:"projectId"`
	Selections []VariantSelectionReq `json:"selections"`
	// WarehouseID 归属仓（issue #15）：与生成路径同一口径，仅用于主体 SKU 的仓码前缀。
	WarehouseID string `json:"warehouseId"`
	// ExistingOptionValues 前端清单里每行的 option_values（JSON 对象）。
	ExistingOptionValues []json.RawMessage `json:"existingOptionValues"`
}

// VariantPreviewRow 预览返回的一行（尚未落库）。
type VariantPreviewRow struct {
	// OptionValues 规范化后的规格组合（jsonb 对象，键序按属性组固定顺序）。
	OptionValues json.RawMessage `json:"optionValues"`
	// OptionKey 组合的规范化键（前端据此去重，与 service 的 optionKey 逐字一致）。
	OptionKey string `json:"optionKey"`
	// SKUCode 系统生成的 SKU（与保存路径同一套拼接规则，运营可在清单里改写）。
	SKUCode string `json:"skuCode"`
}

// PreviewVariantResp 组合生成预览结果。
type PreviewVariantResp struct {
	ProductID string `json:"productId"`
	// Total 本次笛卡尔积的组合总数（去重前）。
	Total int `json:"total"`
	// Skipped 因为「库里已有该组合」或「清单里已有该组合」而没有返回的组合数。
	Skipped int `json:"skipped"`
	// Rows 需要追加进前端清单的行。
	Rows []*VariantPreviewRow `json:"rows"`
}

// SaveVariantListReq 以清单为准保存变体（唯一落库动作）。
type SaveVariantListReq struct {
	ProductID string `json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// WarehouseID 新增行的归属仓（issue #15）：不选即兜底该工程的默认仓；
	// 既有变体不受影响（它们的库存记录早已存在）。
	WarehouseID string `json:"warehouseId"`
	// Rows 清单全部行，顺序即期望的展示顺序。
	Rows []VariantListRow `json:"rows"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略。
	OperatorID string `json:"-" form:"-"`
}

// VariantSaveSkip 保存时被跳过的一行（不整批失败，逐条回带原因）。
//
// Reason 是 enums 常量（= i18n key），由 inbound 取词后展示；
// OptionValues 原样带回，供页面把跳过的那一行拼成可读规格文本。
type VariantSaveSkip struct {
	VariantID    string          `json:"variantId"`
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
	Reason       string          `json:"reason"`
}

// SaveVariantListResp 保存结果（计数 + 逐条跳过原因 + 保存后的全部变体）。
type SaveVariantListResp struct {
	ProductID string `json:"productId"`
	// Created / Updated / Deleted 新增 / 改了 SKU / 删除的变体数。
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	// Skipped 未能删除的既有变体（有非零库存或被引用），逐条带原因 —— 不静默。
	Skipped []VariantSaveSkip `json:"skipped"`
	// Variants 保存后该商品的全部变体。
	Variants []*VariantResp `json:"variants"`
}
