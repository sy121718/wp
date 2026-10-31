package migrations

import "sync"

func registerControlSelectionI18n() {
	registerControlSelectionI18nOnce.Do(registerControlSelectionI18nSeed)
}

var registerControlSelectionI18nOnce sync.Once

func registerControlSelectionI18nSeed() {

	registerSeed(Seed{
		Version:   "416-i18n-control-selection",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.coupons.detail.ph.max_uses','admin.coupons.detail.ph.per_user_limit'," +
			"'admin.coupons.detail.ph.remark'," +
			"'admin.content.templates.entityGroupStructure','admin.content.templates.entityGroupContent')",
		SQL: mustSQL("416_i18n_control_selection.sql"),
	})
}

func init() { registerControlSelectionI18n() }
