package migrations

import "sync"

// registerAdminBulkReworkI18n 登记 admin 批量删除返工新增的错误词条（706）。
//
// 这批 key 在返工里才出现：批量删除整体被拒时的主消息（此前一律报「超管不可删」，
// 与真实原因无关）、空选择、以及非法 ID。它们都走 JSON 出口的取词链，
// 所以 http_code 按各自真实状态码给，category 记 error。
// 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
func registerAdminBulkReworkI18n() {
	registerAdminBulkReworkI18nOnce.Do(registerAdminBulkReworkI18nSeed)
}

var registerAdminBulkReworkI18nOnce sync.Once

func registerAdminBulkReworkI18nSeed() {
	registerSeed(Seed{
		Version:   "706-admin-bulk-rework-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.bulk.emptySelection', 'admin.bulk.invalidSelection')",
		SQL: mustSQL("706_admin_bulk_rework_i18n.sql"),
	})
}
