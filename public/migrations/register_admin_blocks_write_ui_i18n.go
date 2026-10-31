package migrations

import "sync"

func registerAdminBlocksWriteUII18n() {
	registerAdminBlocksWriteUII18nOnce.Do(registerAdminBlocksWriteUII18nSeed)
}

var registerAdminBlocksWriteUII18nOnce sync.Once

func registerAdminBlocksWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "593-admin-blocks-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.blocks.write.refresh', 'admin.blocks.write.noJs')",
		SQL: mustSQL("593_admin_blocks_write_ui_i18n.sql"),
	})
}
