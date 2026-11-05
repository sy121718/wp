package migrations

import "sync"

// registerAdminI18nBulkDeleteI18n 登记词库批量删除返工新增的两条错误词条（708）。
//
// 这两条 key 在返工里才出现：一条是「选中的词条已经不存在」（整批打回，明细走 data.items），
// 一条是「一个都没选中」。它们都走 JSON 出口的取词链，http_code 按各自真实状态码给
// （404 不存在 / 400 参数问题），category 记 error。
// 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
func registerAdminI18nBulkDeleteI18n() {
	registerAdminI18nBulkDeleteI18nOnce.Do(registerAdminI18nBulkDeleteI18nSeed)
}

var registerAdminI18nBulkDeleteI18nOnce sync.Once

func registerAdminI18nBulkDeleteI18nSeed() {
	registerSeed(Seed{
		Version:   "708-admin-i18n-bulk-delete-i18n",
		TableName: "sys_i18n",
		// 门槛精确枚举本批 4 个 (item_key, lang) 组合：中英各一条、缺一不可。
		// 不能只数 zh 的两条 —— 那样 en 缺了门槛仍然成立，seed 会被跳过，
		// 英文界面就退回归口文案。也不用前缀 LIKE、不用全库总量。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.err.i18nEntryMissing', 'zh-CN'), " +
			"('admin.err.i18nEntryMissing', 'en-US'), " +
			"('admin.err.i18nSelectionEmpty', 'zh-CN'), " +
			"('admin.err.i18nSelectionEmpty', 'en-US'))",
		SQL: mustSQL("708_admin_i18n_bulk_delete_i18n.sql"),
	})
}
