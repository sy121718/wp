package migrations

import "sync"

// register_mail_automation_definition_v2_i18n.go — 621a：图定义 v2 校验明细的词条。
//
// 与 621（图定义刷 v2 的 Go 回调迁移）是同一批改动的两半：621 改**形状**，621a 补
// **形状错了怎么说**。放在同一个主题文件族里，是因为下一次改出口名表时两处要一起看。
//
// 单独成一个函数 + Once：init() 里的注册按主题显式列出（见 register.go），
// 重复调用必须是空操作（否则重复 register 会让版本号撞车、ValidateRegistry 报错）。
func registerMailAutomationDefinitionV2I18n() {
	registerMailAutomationDefinitionV2I18nOnce.Do(registerMailAutomationDefinitionV2I18nSeed)
}

var registerMailAutomationDefinitionV2I18nOnce sync.Once

func registerMailAutomationDefinitionV2I18nSeed() {
	// 判据**逐条枚举本批 2 个 item_key × 中英 = 4 行**（上界封闭）。
	//
	// 为什么不用 LIKE 'admin.mail.detail.%'：那个前缀下已经有 19 条别的批次的词条，
	// LIKE 计数会被历史行喂饱 → 判据恒为真 → 本批漏词条也照样绿（迁移 178 踩过同样的坑）。
	registerSeed(Seed{
		Version:   "621a-mail-automation-definition-v2-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('admin.mail.detail.unknownOutlet', 'zh-CN'), ('admin.mail.detail.unknownOutlet', 'en-US'), " +
			"('admin.mail.detail.badCondition', 'zh-CN'), ('admin.mail.detail.badCondition', 'en-US'))",
		SQL: mustSQL("621_mail_automation_definition_v2_i18n.sql"),
	})
}
