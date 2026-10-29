package migrations

func init() {
	registerSeed(Seed{Version: "681-user-component-json-i18n", TableName: "sys_i18n", ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN ('site.component.userForms.noScript','site.component.userForms.retry')", SQL: mustSQL("681_user_component_json_i18n.sql")})
}
