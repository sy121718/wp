package migrations

// 752 给商品评分与 SEO 评分的五个权限点补 type=3 按钮节点。
//
// 模板里的按钮显隐必须写按钮码（isisset(.Buttons["product.seo_score"])），
// 而按钮码是菜单节点的 title_key；没有节点时模板写什么都查不到，按钮永远不显示。
func init() {
	registerSeed(Seed{
		Version:   "752-product-score-rating-buttons",
		TableName: "sys_menus",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END FROM sys_menus
WHERE deleted_at IS NULL AND type = 3 AND permission_code IN (
  'product:seo_score', 'product:brand_seo_score', 'product:category_seo_score',
  'product:rating_add', 'product:rating_delete')`,
		SQL: mustSQL("752_product_score_rating_buttons.sql"),
	})
}
