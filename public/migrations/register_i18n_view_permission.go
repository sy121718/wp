package migrations

import "sync"

func registerI18nViewPermission() {
	registerI18nViewPermissionOnce.Do(registerI18nViewPermissionSQL)
}

var registerI18nViewPermissionOnce sync.Once

func registerI18nViewPermissionSQL() {

	register(Migration{
		Version:   "294-i18n-view-permission",
		TableName: "sys_permission",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'i18n:view') = 1 AND " +

			"EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype = 'p' AND v1 = '/api/i18n/list' AND v2 = 'GET') AND " +

			"NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/i18n' AND type = 2 " +
			"AND deleted_at IS NULL AND permission_code = 'i18n:manage') " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("294_i18n_view_permission.sql"),
	})
}
