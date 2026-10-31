package contenttemplateenums

type LabelPair struct {
	Key      string
	Fallback string
}

var (
	LabelEntityHeader   = LabelPair{"admin.content.templates.entityHeader", "页眉（结构模板）"}
	LabelEntityFooter   = LabelPair{"admin.content.templates.entityFooter", "页脚（结构模板）"}
	LabelEntityProduct  = LabelPair{"admin.content.templates.entityProduct", "商品详情"}
	LabelEntityArticle  = LabelPair{"admin.content.templates.entityArticle", "文章详情"}
	LabelEntityCategory = LabelPair{"admin.content.templates.entityCategory", "分类归档"}
	LabelEntityTag      = LabelPair{"admin.content.templates.entityTag", "标签归档"}
	LabelEntityBrand    = LabelPair{"admin.content.templates.entityBrand", "品牌归档"}
)

var (
	LabelRoleArchive = LabelPair{"admin.content.templates.roleArchive", "归档页"}
	LabelRoleDetail  = LabelPair{"admin.content.templates.roleDetail", "详情页"}
)

var (
	LabelSlotHeader = LabelPair{"admin.content.templates.slotHeader", "页眉"}
	LabelSlotFooter = LabelPair{"admin.content.templates.slotFooter", "页脚"}
)

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
