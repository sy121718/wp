package productdto

import json "go_wp/pkg/jsonx"

// product_variant_req.go / product_variant_resp.go — 变体清单、组合预览与批量保存的入参与回执。

// VariantOptionResp 可选的变体（跨商品），带所属商品与后台可见的价格 / 成本。
//
// 与 VariantResp 的区别是**选择器视角**：带商品名（下拉要显示「哪件商品的哪个 SKU」），
// 不带条码 / 图片 / 排序这些选择器用不上的字段。
type VariantOptionResp struct {
	VariantID   string   `json:"variantId"`
	SKUCode     string   `json:"skuCode"`
	ProductID   string   `json:"productId"`
	ProductName string   `json:"productName"`
	Price       float64  `json:"price"`
	CostPrice   *float64 `json:"costPrice"`
	Enabled     bool     `json:"enabled"`
}

// VariantListResp 前端清单里的一行（保存请求的输入）。
//
// VariantID 为空 = 清单里的**新增行**（库里还没有这个组合）：
// 它的 OptionValues 必须通过服务端重算校验（属性组属于该商品、取值在组内）；
// VariantID 非空 = 清单里保留的**既有变体**：组合以库里的 option_values 为准
// （前端传的 OptionValues 不参与判定），只有 SKUCode 允许被编辑。
type VariantListResp struct {
	VariantID    string          `json:"variantId"`
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
}

// VariantPreviewResp 预览返回的一行（尚未落库）。
type VariantPreviewResp struct {
	// OptionValues 规范化后的规格组合（jsonb 对象，键序按属性组固定顺序）。
	OptionValues json.RawMessage `json:"optionValues"`
	// OptionKey 组合的规范化键（前端据此去重，与 service 的 optionKey 逐字一致）。
	OptionKey string `json:"optionKey"`
	// SKUCode 系统生成的 SKU（与保存路径同一套拼接规则，运营可在清单里改写）。
	SKUCode string `json:"skuCode"`
	// Spec 规格的可读文本（「颜色：红 · 尺码：L」），前端直接渲染。
	// 由 service 用商品属性组算好：控制器再取一次属性只是为了拼这句，等于把同一份
	// 判定分到两处（而其中一处是可以静默拿到空 attrs 的）。
	Spec string `json:"spec"`
}

// PreviewVariantResp 组合生成预览结果。
type PreviewVariantResp struct {
	// OK 恒为 true（失败走 pkg/response 的错误体）：前端据它区分「生成过」与「没生成过」。
	OK        bool   `json:"ok"`
	ProductID string `json:"productId"`
	// Total 本次笛卡尔积的组合总数（去重前）。
	Total int `json:"total"`
	// Skipped 因为「库里已有该组合」或「清单里已有该组合」而没有返回的组合数。
	Skipped int `json:"skipped"`
	// Rows 需要追加进前端清单的行。
	Rows []*VariantPreviewResp `json:"rows"`
}

// VariantSaveSkipResp 保存时被跳过的一行（不整批失败，逐条回带原因）。
//
// Reason 是 enums 常量（= i18n key），由 inbound 取词后展示；
// OptionValues 原样带回，供页面把跳过的那一行拼成可读规格文本。
//
// Detail 是**可定位明细**（引用面 / 涉及的工程与商品 id，审计 DB-03 §5.1 第 2 条）：
// 它与 Reason 分两个字段是刻意的 —— Reason 会经提示页展示，只应承载受控原因枚举；
// 把自由文本拼进 Reason 会让提示页出现不可控内容。需要看明细的调用方读 Detail（JSON 响应）。
type VariantSaveSkipResp struct {
	VariantID    string          `json:"variantId"`
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
	Reason       string          `json:"reason"`
	// Detail 可定位明细（无额外信息时为空串，序列化仍带上该键，便于调用方稳定取值）。
	Detail string `json:"detail"`
}

// SaveVariantListResp 保存结果（计数 + 逐条跳过原因 + 保存后的全部变体）。
type SaveVariantListResp struct {
	ProductID string `json:"productId"`
	// Created / Updated / Deleted 新增 / 改了 SKU / 删除的变体数。
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	// Skipped 未能删除的既有变体（有非零库存或被引用），逐条带原因 —— 不静默。
	Skipped []VariantSaveSkipResp `json:"skipped"`
	// Variants 保存后该商品的全部变体。
	Variants []*VariantResp `json:"variants"`
}
