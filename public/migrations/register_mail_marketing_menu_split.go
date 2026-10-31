package migrations

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
