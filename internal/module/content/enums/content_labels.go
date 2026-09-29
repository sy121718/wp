// content_labels.go — 内容模块后台 / 工作台展示标签（枚举 → 展示名）的真源。
package contentenums

// EntityTypeLabel 内容类型的**集合源展示名** → (词条 key, 中文兜底)。
//
// 消费方是「集合源 / 字段下拉」（GET /api/content/collections），所以文案说的是
// 「文章列表 / 商品列表 / 分类列表」——集合源取回的是**一批**实体，不是单个详情。
//
// 认不出的取值 key 留空、兜底为原值（与调用点改前的行为一致：宁可显示生值，
// 也不显示空白 —— 空白读不出「有个不认识的类型」）。
func EntityTypeLabel(entityType string) (key, fallback string) {
	switch entityType {
	case "article":
		return "admin.collection.article", "文章列表"
	case "product":
		return "admin.collection.product", "商品列表"
	case "category":
		return "admin.collection.category", "分类列表"
	default:
		return "", entityType
	}
}
