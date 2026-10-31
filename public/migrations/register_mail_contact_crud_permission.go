package migrations

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
