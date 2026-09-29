// contenttemplate_labels.go — 后台页面展示标签（枚举 → 展示名）的唯一真源。
package contenttemplateenums

// LabelPair 一条展示标签：i18n key + 中文兜底。
//
// 形态统一成「两个都给」：只给中文 → 英文界面恒中文；只给 key → 词条缺失时页面
// 显示裸 key（`admin.content.templates.entity.product`）。两个都给 → 命中出译文、
// 未命中出中文兜底。取词在调用点（enums 零依赖，也不持有请求语言）。
type LabelPair struct {
	Key      string
	Fallback string
}

// 实体类型（content_templates.entity_type）的展示名。
//
// 结构模板（header / footer）与内容实体模板在这张表里是两类东西：前者没有实体来源、
// 也不接受字段绑定。文案上必须一眼分得开 —— 「页眉（结构模板）」而不是「页眉」。
//
// header / footer 两条**复用筛选下拉已经在用的词条**（值就是「页眉（结构模板）」，
// 中英成对已在库内）：同一页的筛选器与表格徽章说同一句话，好过再造一条同值的 key。
var (
	LabelEntityHeader   = LabelPair{"admin.content.templates.entityHeader", "页眉（结构模板）"}
	LabelEntityFooter   = LabelPair{"admin.content.templates.entityFooter", "页脚（结构模板）"}
	LabelEntityProduct  = LabelPair{"admin.content.templates.entityProduct", "商品详情"}
	LabelEntityArticle  = LabelPair{"admin.content.templates.entityArticle", "文章详情"}
	LabelEntityCategory = LabelPair{"admin.content.templates.entityCategory", "分类归档"}
	LabelEntityTag      = LabelPair{"admin.content.templates.entityTag", "标签归档"}
	LabelEntityBrand    = LabelPair{"admin.content.templates.entityBrand", "品牌归档"}
)

// 模板角色（content_templates.template_role）的展示名。
var (
	LabelRoleArchive = LabelPair{"admin.content.templates.roleArchive", "归档页"}
	LabelRoleDetail  = LabelPair{"admin.content.templates.roleDetail", "详情页"}
)

// 结构槽位（构建期的 header / footer）的展示名。
//
// 槽位取值由调用点用 builder 的槽位常量判定后再取这两条 —— enums 零依赖，
// 不 import builder（见 AGENTS.md「enums 包零依赖」）。
var (
	LabelSlotHeader = LabelPair{"admin.content.templates.slotHeader", "页眉"}
	LabelSlotFooter = LabelPair{"admin.content.templates.slotFooter", "页脚"}
)

// EntityTypeLabel 实体类型 → 展示标签。
//
// 认不出的取值 key 留空、兜底为原值：宁可显示生值（`carousel`），也不显示空白 ——
// 空白单元格读不出「这里有个不认识的类型」。
func EntityTypeLabel(entityType string) LabelPair {
	switch entityType {
	case "header":
		return LabelEntityHeader
	case "footer":
		return LabelEntityFooter
	case "product":
		return LabelEntityProduct
	case "article":
		return LabelEntityArticle
	case "category":
		return LabelEntityCategory
	case "tag":
		return LabelEntityTag
	case "brand":
		return LabelEntityBrand
	default:
		return LabelPair{Fallback: entityType}
	}
}

// TemplateRoleLabel 模板角色 → 展示标签。
//
// 空值按 detail：历史数据没有该列时，模板就是详情页模板（列表页此前也这么显示）。
func TemplateRoleLabel(role string) LabelPair {
	switch role {
	case "archive":
		return LabelRoleArchive
	case "", "detail":
		return LabelRoleDetail
	default:
		return LabelPair{Fallback: role}
	}
}
