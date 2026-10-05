package migrations

// 525 · 联系人 CRUD 与标签管理的权限点 + 菜单按钮。
//
// 判据枚举**本批自己的六个对象**（3 个权限点 + 3 个菜单按钮），而不是计数或前缀：
//
//	· 用 LIKE 前缀会把别的批次的行算进来 → 计数虚高 → 本批被静默跳过（058 的真实故障）；
//	· 用「全库总量」会随新权限点增长而永远追不平 → 每次启动都重跑（076）。
//	偏差方向刻意选「宁可重跑，不可静默跳过」。
//
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
func init() {
	registerSeed(Seed{
		Version:   "525-mail-contact-crud-permission",
		TableName: "sys_menus",
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = 'mail:contact_save' AND api_path = '/api/mail/contact/save' AND api_method = 'POST')
    AND EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = 'mail:contact_delete' AND api_path = '/api/mail/contact/delete' AND api_method = 'POST')
    AND EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = 'mail:contact_tag' AND api_path = '/api/mail/contact/tag' AND api_method = 'POST')
    AND EXISTS (SELECT 1 FROM sys_menus WHERE type = 3 AND permission_code = 'mail:contact_save' AND deleted_at IS NULL)
    AND EXISTS (SELECT 1 FROM sys_menus WHERE type = 3 AND permission_code = 'mail:contact_delete' AND deleted_at IS NULL)
    AND EXISTS (SELECT 1 FROM sys_menus WHERE type = 3 AND permission_code = 'mail:contact_tag' AND deleted_at IS NULL)
THEN 1 ELSE 0 END`,
		SQL: mustSQL("525_mail_contact_crud_permission.sql"),
	})
}
