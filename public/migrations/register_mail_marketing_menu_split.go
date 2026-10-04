package migrations

// 521 · 邮件营销独立成「营销」一级目录（页面按职能拆分后的菜单收口）。
//
// 与 224（导航收口）、229（库存独立目录）同一套做法：只改 sys_menus，不动表结构。
// 判定刻意用「营销目录 + 联系人 / 邮件模板 / 群发活动 都在，旧 /admin/mail/marketing
// 已消失，且活动报表行不再出现在侧栏」而不是单条 —— 单条最容易先成功，中途失败会让
// 半成品被误判为已完成而永不修复。最后一条是对 401 首启时序窗口的兜底（见 SQL 文件头）。
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
func init() {
	registerSeed(Seed{
		Version:   "521-mail-marketing-menu-split",
		TableName: "sys_menus",
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND title = '营销' AND type = 1 AND deleted_at IS NULL)
    AND EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/mail/contacts' AND deleted_at IS NULL)
    AND EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/mail/templates' AND deleted_at IS NULL)
    AND EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/mail/campaigns' AND deleted_at IS NULL)
    AND NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/mail/marketing' AND deleted_at IS NULL)
    AND NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/mail/campaign' AND is_hidden = 0 AND deleted_at IS NULL)
THEN 1 ELSE 0 END`,
		SQL: mustSQL("521_mail_marketing_menu_split.sql"),
	})
}
