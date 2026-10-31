package migrations

import "sync"

func registerListEmptyStateI18n() {
	registerListEmptyStateI18nOnce.Do(registerListEmptyStateI18nSeed)
}

var registerListEmptyStateI18nOnce sync.Once

func registerListEmptyStateI18nSeed() {

	registerSeed(Seed{
		Version:   "399-i18n-list-empty-states",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 12 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key IN ('admin.admins.empty.title','admin.roles.empty.title','admin.permissions.empty.title','admin.datarules.empty.title') " +
			"AND lang IN ('zh-CN','en-US')) " +
			"OR (item_key = 'admin.admins.empty' AND lang = 'zh-CN' AND item_value <> '还没有管理员，点右上「新建管理员」创建一个。') " +
			"OR (item_key = 'admin.admins.empty' AND lang = 'en-US' AND item_value <> 'No administrators yet. Click \"New administrator\" in the top right to create one.') " +
			"OR (item_key = 'admin.roles.empty' AND lang = 'zh-CN' AND item_value <> '还没有角色，点右上「新建角色」创建一个。') " +
			"OR (item_key = 'admin.roles.empty' AND lang = 'en-US' AND item_value <> 'No roles yet. Click \"New role\" in the top right to create one.') " +
			"OR (item_key = 'admin.permissions.empty' AND lang = 'zh-CN' AND item_value <> '还没有权限点，点右上「新建权限点」创建一个。') " +
			"OR (item_key = 'admin.permissions.empty' AND lang = 'en-US' AND item_value <> 'No permissions yet. Click \"New permission\" in the top right to create one.') " +
			"OR (item_key = 'admin.datarules.empty' AND lang = 'zh-CN' AND item_value <> '还没有数据规则，点右上「新建规则」创建一个。') " +
			"OR (item_key = 'admin.datarules.empty' AND lang = 'en-US' AND item_value <> 'No data rules yet. Click \"New rule\" in the top right to create one.')",
		SQL: mustSQL("399_i18n_list_empty_states.sql"),
	})
}

func init() { registerListEmptyStateI18n() }
