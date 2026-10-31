package contentenums

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
