package migrations

import "sync"

func registerTailHardcodedI18n() {
	registerTailHardcodedI18nOnce.Do(registerTailHardcodedI18nSeed)
}

var registerTailHardcodedI18nOnce sync.Once

func registerTailHardcodedI18nSeed() {

	registerSeed(Seed{
		Version:   "435-i18n-tail-hardcoded",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.common.locale.empty'),
    ('admin.common.locale.enabled'),
    ('admin.common.locale.ph.lang'),
    ('admin.product_attributes.values.col.status'),
    ('admin.product_attributes.values.empty'),
    ('admin.product_attributes.values.ph.key'),
    ('admin.product_attributes.values.ph.label'),
    ('admin.theme_settings.global_blocks_note'),
    ('admin.theme_settings.group_items'),
    ('admin.theme_settings.structure_templates_note'),
    ('attrValsAdd'),
    ('attrValsOn'),
    ('attrValsSort'),
    ('attrValueLabel')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("720i_current_435_i18n_tail_hardcoded.sql"),
	})
}

func init() { registerTailHardcodedI18n() }
