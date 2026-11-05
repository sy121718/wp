package migrations

func init() {
	registerSeed(Seed{
		Version:   "721-product-tag-short-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.product_tags.bulkDeleteConfirm', 'tagBulkConfirm', true),
    ('admin.product_tags.col.actions', 'tagActions', true),
    ('admin.product_tags.col.desc', 'tagDesc', true),
    ('admin.product_tags.col.hits', 'tagHits', true),
    ('admin.product_tags.col.kind', 'tagKindCol', true),
    ('admin.product_tags.col.name', 'tagNameCol', true),
    ('admin.product_tags.col.params', 'tagArgs', true),
    ('admin.product_tags.col.product', 'tagProd', true),
    ('admin.product_tags.col.recalcAt', 'tagRecalcAt', true),
    ('admin.product_tags.col.rule', 'tagRuleCol', true),
    ('admin.product_tags.col.slug', 'tagSlugCol', true),
    ('admin.product_tags.col.state', 'tagState', true),
    ('admin.product_tags.col.type', 'tagType', true),
    ('admin.product_tags.create.hint', 'tagNewHint', true),
    ('admin.product_tags.create.submit', 'tagNewSubmit', true),
    ('admin.product_tags.create.title', 'tagNewTitle', true),
    ('admin.product_tags.filter.phKeyword', 'tagFltKw', true),
    ('admin.product_tags.help.label', 'tagHelp', true),
    ('admin.product_tags.intro', 'tagIntro', true),
    ('admin.product_tags.kind.manual', 'tagManualKind', true),
    ('admin.product_tags.kind.rule', 'tagRuleKind', true),
    ('admin.product_tags.kindManual', 'tagManualLabel', true),
    ('admin.product_tags.kindRule', 'tagRuleKindText', true),
    ('admin.product_tags.label.kind', 'tagKindLabel', true),
    ('admin.product_tags.label.name', 'tagNameLabel', true),
    ('admin.product_tags.label.rule', 'tagRuleLabel', true),
    ('admin.product_tags.label.slug', 'tagSlugLabel', true),
    ('admin.product_tags.list.deleteConfirm', 'tagDelConfirm', true),
    ('admin.product_tags.list.empty', 'tagListEmpty', true),
    ('admin.product_tags.list.emptyFilteredTitle', 'tagNoMatches', true),
    ('admin.product_tags.list.emptyTitle', 'tagEmptyTitle', true),
    ('admin.product_tags.list.hitAria', 'tagListHitAria', true),
    ('admin.product_tags.list.hitEmpty', 'tagListHitEmpty', true),
    ('admin.product_tags.list.hitTitle', 'tagListHitTitle', true),
    ('admin.product_tags.list.recalc', 'tagListRecalc', true),
    ('admin.product_tags.list.save', 'tagListSave', true),
    ('admin.product_tags.list.title', 'tagListTitle', true),
    ('admin.product_tags.ph.days', 'tagDays', true),
    ('admin.product_tags.ph.daysShort', 'tagDaysShort', true),
    ('admin.product_tags.ph.sort', 'tagSort', true),
    ('admin.product_tags.projectAria', 'tagProjAria', true),
    ('admin.product_tags.recalcAll', 'tagRecalcAll', true),
    ('admin.product_tags.recalcNone', 'tagRecalcNone', true),
    ('admin.product_tags.rule.newArrival.describe', 'tagNewDesc', true),
    ('admin.product_tags.rule.newArrival.name', 'tagNewName', true),
    ('admin.product_tags.rule.newArrival.params', 'tagNewArgs', true),
    ('admin.product_tags.rule.onSale.describe', 'tagSaleDesc', true),
    ('admin.product_tags.rule.onSale.name', 'tagSaleName', true),
    ('admin.product_tags.rule.onSale.params', 'tagSaleArgs', true),
    ('admin.product_tags.rule.paramsInvalid', 'tagRuleArgsBad', true),
    ('admin.product_tags.rule.priceRange.describeBoth', 'tagPriceBoth', true),
    ('admin.product_tags.rule.priceRange.describeMax', 'tagPriceMax', true),
    ('admin.product_tags.rule.priceRange.describeMin', 'tagPriceMin', true),
    ('admin.product_tags.rule.priceRange.name', 'tagPriceName', true),
    ('admin.product_tags.rule.priceRange.params', 'tagPriceArgs', true),
    ('admin.product_tags.ruleEmpty', 'tagRuleEmpty', true),
    ('admin.product_tags.rules.aria', 'tagRulesAria', true),
    ('admin.product_tags.rules.hint', 'tagRulesHint', true),
    ('admin.product_tags.rules.note', 'tagRulesNote', true),
    ('admin.product_tags.rules.title', 'tagRulesTitle', true),
    ('admin.product_tags.title', 'tagTitle', true),
    ('shell.pagination.info', 'pagerInfo', false),
    ('shell.pagination.label', 'pager', false),
    ('shell.pagination.next', 'pagerNext', false),
    ('shell.pagination.prev', 'pagerPrev', false)
)
SELECT CASE WHEN
 NOT EXISTS (SELECT 1 FROM keys k CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = langs.lang))
 AND NOT EXISTS (SELECT 1 FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key
 WHERE k.retire OR NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang))
 AND NOT EXISTS (SELECT 1 FROM (VALUES ('tagHitCount'), ('tagHitInfo'), ('tagRuleUnknown')) AS complete(item_key)
 CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = complete.item_key AND t.lang = langs.lang))
 THEN 1 ELSE 0 END`,
		SQL: mustSQL("721_product_tag_short_keys_i18n.sql"),
	})
}
