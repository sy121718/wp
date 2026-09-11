// Package productcontract product 模块对外契约（issue #12 补充）。
package productcontract

// TranslationCandidate 一条可翻译文本（商品域翻译工作台一行）。
//
// 放契约包：它是跨模块传递的不可变值（dashboard 的翻译工作台消费），
// 按「跨模块只传 contract 与不可变 dto」的约定，不能让 dashboard 导入 product/service。
type TranslationCandidate struct {
	// EntityType 实体类型（product / product_category / product_brand /
	// product_tag / product_attribute）。
	EntityType string
	// EntityID 实体 id（工作台分组与「同一实体的行」判定）。
	EntityID string
	// EntityName 实体展示名（工作台分组标题）。
	EntityName string
	// Field 字段名（白名单字段，如 name / imageAlts / values）。
	Field string
	// FieldLabel 字段中文名（工作台字段列）。
	FieldLabel string
	// Context 语境（"实体类型.字段名"），写入 sys_translation 的 context 列。
	Context string
	// SourceText 原文（作者填写；无译文时产物即此文本）。
	SourceText string
	// SourceHash sha256hex(TrimSpace(SourceText))，写入与校验共用。
	SourceHash string
}
