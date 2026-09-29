package migrations

// 449 — product / inventory 两模块「Go 侧生成、会进页面的中文」词条化（key + 中文兜底）。
//
// 门槛判据**逐条枚举本批自己的 item_key**（上界封闭），不用 LIKE 前缀、也不用全库总量 ——
// 依据 AGENTS.md §数据库·迁移 的两条判据与 058 / 076 两次真实故障：
// 前缀判据在「已有别的批次同前缀行」时计数虚高 → 本批被静默跳过；在用全库总量时
// 将来新增同前缀 key 会让计数永远追不平 → 每次启动重跑。
//
// 行数 = key 数 × 2（每个 key 中英各一行）：137 × 2 = 274。
//
// 137 而不是 142：本批原先含 admin.seo.grade.{green,lightgreen,yellow,red,redBlocking}
// 5 个 key（10 行）—— product 侧改用共享映射 seoscore.ScoreGradeText 后它们成为孤儿，
// 由 458 退役（register_retire_seo_grade_keys.go）。这 5 个 key 已从下面的列表移出，
// 门槛同步 284 → 274：**留着它们会让计数永远差 10、本 seed 每轮启动都重跑**。
// 449 的 SQL 保持原样（历史迁移 SQL 不改），因此那 10 行仍由 458 在每轮 seed 末尾清掉。
func init() {
	registerSeed(Seed{
		Version:   "449-i18n-product-inventory-go-texts",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 274 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.inventory.direction.in', 'admin.inventory.direction.out', 'admin.inventory.direction.adjust', " +
			"'admin.inventory.err.externalSkuOwner', 'admin.inventory_sources.type.internal', 'admin.inventory_sources.relatedTypeDefault', " +
			"'admin.inventory_purchases.status.pending', 'admin.inventory_purchases.status.partial', 'admin.inventory_purchases.kind.purchase', " +
			"'admin.inventory_purchases.option.costUnset', 'admin.inventory_purchases.option.costLabel', 'admin.product_categories.children.pageLabel', " +
			"'admin.products.option.noPrimaryCategory', 'admin.products.option.noBrand', 'admin.products.list.tagCount', " +
			"'admin.products.create.quantityInvalid', 'admin.product_bundle.noProduct', 'admin.product_detail_template.noProductPrompt', " +
			"'admin.product_detail_template.nameRequired', 'admin.product_detail_template.pathRequired', 'admin.product_edit.err.defaultPriceInvalid', " +
			"'admin.product_edit.err.weightInvalid', 'admin.product_pricing.rule.costMultiple.name', 'admin.product_pricing.rule.costMultiple.params', " +
			"'admin.product_pricing.rule.costMultiple.describe', 'admin.product_pricing.rule.costMarkup.name', 'admin.product_pricing.rule.costMarkup.params', " +
			"'admin.product_pricing.rule.costMarkup.describe', 'admin.product_pricing.rule.targetMargin.name', 'admin.product_pricing.rule.targetMargin.params', " +
			"'admin.product_pricing.rule.targetMargin.describe', 'admin.product_pricing.rule.fixedPrice.name', 'admin.product_pricing.rule.fixedPrice.params', " +
			"'admin.product_pricing.rule.fixedPrice.describe', 'admin.product_pricing.rule.paramsInvalid', 'admin.product_pricing.rule.unknown', " +
			"'admin.product_pricing.rounding.none', 'admin.product_pricing.rounding.integer', 'admin.product_pricing.rounding.end9', " +
			"'admin.product_pricing.rounding.end99', 'admin.product_pricing.rounding.unknown', 'admin.product_pricing.scope.sku', " +
			"'admin.product_pricing.scope.product', 'admin.product_pricing.scope.filter', 'admin.product_pricing.scope.unknown', " +
			"'admin.product_pricing.filter.status', 'admin.product_pricing.filter.keyword', 'admin.product_pricing.filter.categoryId', " +
			"'admin.product_pricing.filter.brandId', 'admin.product_pricing.filter.tagId', 'admin.product_pricing.line.changed', " +
			"'admin.product_pricing.line.unchanged', 'admin.product_pricing.line.skipped', 'admin.product_pricing.skip.costMissing', " +
			"'admin.product_pricing.skip.outOfRange', 'admin.product_tags.rule.newArrival.name', 'admin.product_tags.rule.newArrival.params', " +
			"'admin.product_tags.rule.newArrival.describe', 'admin.product_tags.rule.priceRange.name', 'admin.product_tags.rule.priceRange.params', " +
			"'admin.product_tags.rule.priceRange.describeBoth', 'admin.product_tags.rule.priceRange.describeMin', 'admin.product_tags.rule.priceRange.describeMax', " +
			"'admin.product_tags.rule.onSale.name', 'admin.product_tags.rule.onSale.params', 'admin.product_tags.rule.onSale.describe', " +
			"'admin.product_tags.rule.paramsInvalid', 'admin.product_tags.rule.unknown', 'admin.product_tags.kind.rule', " +
			"'admin.product_tags.kind.manual', 'admin.product_tags.recalcNone', 'admin.product_translations.savedNote', " +
			"'admin.product_translations.savedNone', 'admin.product_translations.err.contextInvalid', 'admin.product_translations.err.sourceSkipped', " +
			"'admin.product_translations.err.targetEmpty', 'admin.product_translations.err.shapeMismatch', " +
			"'admin.seo.empty.missingProduct', 'admin.seo.empty.productUnreadable', " +
			"'admin.seo.empty.missingCategory', 'admin.seo.empty.categoryUnreadable', 'admin.seo.empty.missingBrand', " +
			"'admin.seo.empty.brandUnreadable', 'admin.seo.serp.titleEmpty', 'admin.seo.serp.urlEmpty', " +
			"'admin.seo.serp.descEmpty', 'admin.seo.issue.line', 'admin.seo.index.article', " +
			"'admin.product_pricing.detail.paramsNotObject', 'admin.product_pricing.detail.paramRequired', 'admin.product_pricing.detail.paramNotNumber', " +
			"'admin.product_pricing.detail.paramMissingOrNotNumber', 'admin.product_pricing.detail.paramOutOfRange', 'admin.product_pricing.detail.paramOutOfRangePlain', " +
			"'admin.product_pricing.detail.unknownKeys', 'admin.product_pricing.detail.unknownRuleType', 'admin.product_pricing.detail.unknownRounding', " +
			"'admin.product_pricing.detail.filterProjectRequired', 'admin.product_pricing.detail.filterConditionRequired', 'admin.product_pricing.detail.targetTooMany', " +
			"'admin.product_tags.detail.paramsNotObject', 'admin.product_tags.detail.paramRequired', 'admin.product_tags.detail.paramNotInteger', " +
			"'admin.product_tags.detail.paramNotNumber', 'admin.product_tags.detail.paramOutOfRange', 'admin.product_tags.detail.paramOutOfRangePlain', " +
			"'admin.product_tags.detail.unknownKeys', 'admin.product_tags.detail.priceNeedsOne', 'admin.product_tags.detail.priceOrderWrong', " +
			"'admin.product_tags.detail.unknownRuleType', 'admin.products.detail.variationCountExceed', 'admin.products.detail.variationDimensionExceed', " +
			"'admin.products.detail.variationAttributeUnreferenced', 'admin.products.detail.variationAttributeNotInProduct', 'admin.products.detail.variationValueNotEnabled', " +
			"'admin.products.detail.relatedSelfReference', 'admin.products.detail.relatedMissing', 'admin.products.detail.relatedCrossProject', " +
			"'admin.products.detail.refGuardBlocked', " +
			// 可翻译字段的展示名（entityType / field，调用点在 product/contract/product_entity.go）：
			// 2026-09 补进本批 —— 它们此前**从未 seed 过**，英文界面一直回落到中文兜底。
			"'admin.product_translations.entityType.product', 'admin.product_translations.entityType.category', " +
			"'admin.product_translations.entityType.brand', 'admin.product_translations.entityType.tag', " +
			"'admin.product_translations.entityType.attribute', 'admin.product_translations.field.productName', " +
			"'admin.product_translations.field.productSubtitle', 'admin.product_translations.field.productDescription', " +
			"'admin.product_translations.field.productImageAlts', 'admin.product_translations.field.categoryName', " +
			"'admin.product_translations.field.categoryDescription', 'admin.product_translations.field.categorySeoTitle', " +
			"'admin.product_translations.field.brandName', 'admin.product_translations.field.brandDescription', " +
			"'admin.product_translations.field.brandSeoTitle', 'admin.product_translations.field.tagName', " +
			"'admin.product_translations.field.attributeName', 'admin.product_translations.field.attributeValues'" +
			") AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("449_i18n_product_inventory_go_texts.sql"),
	})
}
