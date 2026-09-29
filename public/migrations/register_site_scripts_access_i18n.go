package migrations

// 461 — 站点自定义注入代码（PIPE-8）与访问守卫面板（PIPE-6）的词条。
//
// 为什么单独一条而不是并进 460a：460a 是 PIPE-7（定时上下线）自己的两半（表 + 它的词条），
// 本批是另外两个独立能力（PIPE-8 的站点级 Head/Body 注入、PIPE-6 的页面访问守卫）为守
// 「零迁移」边界而欠下的词条，与 460a 没有同批关系，硬并进去会让「谁负责哪批词条」失去边界。
//
// 门槛判据**逐条枚举本批自己的 19 个 item_key**（上界封闭，19 × 2 = 38 行）：
//   - 不用 LIKE 前缀：别的批次已有同前缀行时计数虚高 → 本批被静默跳过（058 的真实故障）；
//   - 不用全库总量：将来新增同前缀 key 时永远追不平 → 每次启动重跑（076 的真实故障）。
//
// 放在**种子**台账（registerSeed 而不是 register）：只新增词条、不改任何既有 key，
// 属 seed 语义（可重复写入的默认值）。
//
// 注册方式：本文件自带 init()（与 460a 的 register_page_schedule_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "461-site-scripts-access-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 38 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			// PIPE-8：自定义 Head / Body 代码校验失败的就地提示（2 条，
			// 真源 internal/module/project/enums/site_scripts_keys.go）。
			"'admin.settings.scripts.head_invalid', 'admin.settings.scripts.body_invalid', " +
			// PIPE-8：站点设置表单的字段名 / 提示 / 占位（8 条，
			// 真源 internal/templates/admin/project/settings.html）。
			"'admin.settings.field.head_scripts', 'admin.settings.field.body_scripts', " +
			"'admin.settings.hint.head_scripts', 'admin.settings.hint.head_scripts.republish', " +
			"'admin.settings.hint.head_scripts.tail', 'admin.settings.hint.body_scripts', " +
			"'admin.settings.ph.head_scripts', 'admin.settings.ph.body_scripts', " +
			// PIPE-6：工作台「访问权限」面板（9 条，
			// 真源 internal/templates/fragments/settings_panel.html）。
			"'workbench.ui.settings.access', 'workbench.ui.settings.accessPublic', " +
			"'workbench.ui.settings.accessPassword', 'workbench.ui.settings.accessMembers', " +
			"'workbench.ui.settings.accessPasswordLabel', 'workbench.ui.settings.accessPasswordSet', " +
			"'workbench.ui.settings.accessPasswordUnset', 'workbench.ui.settings.accessPasswordPlaceholder', " +
			"'workbench.ui.settings.accessApply'" +
			")",
		SQL: mustSQL("461_site_scripts_access_guard_i18n.sql"),
	})
}
