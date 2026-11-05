package migrations

import "sync"

func registerAdminUINewKeysI18n() {
	registerAdminUINewKeysI18nOnce.Do(registerAdminUINewKeysI18nSeed)
}

var registerAdminUINewKeysI18nOnce sync.Once

func registerAdminUINewKeysI18nSeed() {

	registerSeed(Seed{
		Version:   "592-admin-ui-new-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n
WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (
  'admin.datarules.back_to_edit', 'admin.i18n.deleted', 'pActDone', 'pBulkNoneSel')`,
		SQL: mustSQL("723d_current_592_admin_ui_new_keys_i18n.sql"),
	})
}
