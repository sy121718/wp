package migrations

import "sync"

func registerProductEditI18n() {
	registerProductEditI18nOnce.Do(registerProductEditI18nSeed)
}

var registerProductEditI18nOnce sync.Once

func registerProductEditI18nSeed() {

	registerSeed(Seed{
		Version:   "314-i18n-seed-product-edit",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.products.edit.hint.sub'),
    ('admin.products.edit.hint.subTail'),
    ('admin.products.edit.ph.imageAlts'),
    ('admin.products.edit.ph.images'),
    ('admin.products.hint.editLead'),
    ('admin.products.hint.editTail'),
    ('admin.products.label.imageAlts'),
    ('admin.products.label.seoDescription'),
    ('admin.products.label.seoTitle'),
    ('pEditDefPrice'),
    ('pEditHintFields'),
    ('pEditHintLang'),
    ('pEditPrice'),
    ('pEditSku'),
    ('pEditSlug'),
    ('pEditTitle'),
    ('pEditWeight'),
    ('pFltReset'),
    ('pFltSubmit'),
    ('pImages'),
    ('pKw'),
    ('pListEmptyFlt'),
    ('pMsgSaved'),
    ('pNoMatchesTitle'),
    ('pRowEdit'),
    ('pStatusAll'),
    ('pStatusArchived'),
    ('pStatusDraft'),
    ('pStatusLive'),
    ('pSub'),
    ('pUnit'),
    ('pWeight')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723v_current_314_i18n_seed_product_edit.sql"),
	})
}
