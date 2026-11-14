package migrations

// 722z 修复 723（商品剩余短键）在 artex 上命中的两组真实数据冲突。
//
// 723 的冲突守卫按设计拒绝「旧行与新行元数据不同」，绝不静默二选一。但这两组不是运营编辑：
//   - admin.common.action.preview / uiActPrev：文案相同，只有播种 remark 不同；
//   - admin.products.row.preview / pRowPrev：旧行已是 316 的短文案「预览」，
//     新键默认值却是 191 的旧文案「预览详情页」——机械改名把 316 的裁决丢了回去。
//
// 本迁移只处理「旧行文案 = 316 裁决值，且新行仍是机械默认值（无编辑痕迹）或已是裁决值」；
// 任一侧被改过就保持不动，交 723 按原严格语义拒绝。详见 SQL 头注释。
//
// 执行顺序由版本号保证：722z 排在 316 之后、723 之前，先归一文案并退役冗余旧行，
// 723 随后按原逻辑通过守卫。
func init() {
	registerSeed(Seed{
		Version:   "722z-product-preview-retired-key-repair",
		TableName: "sys_i18n",
		// 没有可修复的形态时返回 1 → 跳过；有冲突形态时返回 0 → 执行。
		ConditionSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1
    FROM (VALUES
        ('admin.products.row.preview', 'pRowPrev', 'zh-CN', '预览', '预览详情页'),
        ('admin.products.row.preview', 'pRowPrev', 'en-US', 'Preview', 'Preview detail page'),
        ('admin.common.action.preview', 'uiActPrev', 'zh-CN', '预览', NULL),
        ('admin.common.action.preview', 'uiActPrev', 'en-US', 'Preview', NULL)
    ) AS a(old_key, new_key, lang, item_value, stale_value)
    JOIN sys_i18n n ON n.item_key = a.new_key AND n.lang = a.lang
    WHERE (a.stale_value IS NOT NULL
           AND n.item_value = a.stale_value
           AND (n.update_time IS NULL
                OR (n.create_time IS NOT NULL AND n.create_time = n.update_time)))
       OR (n.item_value = a.item_value
           AND EXISTS (SELECT 1 FROM sys_i18n o
                       WHERE o.item_key = a.old_key AND o.lang = a.lang
                         AND o.item_value = a.item_value))
) THEN 0 ELSE 1 END`,
		SQL: mustSQL("722z_product_preview_retired_key_repair.sql"),
	})
}
