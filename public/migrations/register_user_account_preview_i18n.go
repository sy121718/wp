package migrations

func init() {
	registerSeed(Seed{Version: "683-user-account-preview-i18n", TableName: "sys_i18n", ConditionSQL: "SELECT CASE WHEN COUNT(*) = 16 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN ('site.component.userForms.preview.loading','site.component.userForms.preview.empty','site.component.userForms.preview.validationError','site.component.userForms.preview.serviceError','site.component.userForms.preview.submitting','site.component.userForms.preview.success','site.component.userForms.preview.forbidden','site.component.userForms.actionUnavailable')", SQL: mustSQL("683_user_account_preview_i18n.sql")})
}
