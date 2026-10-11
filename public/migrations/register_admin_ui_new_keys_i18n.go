package migrations

import "sync"

// register_admin_ui_new_keys_i18n.go — 后台新增提示文案的词条（迁移 592）。
//
// 单独成一个主题文件而不是挤进相邻的 admin i18n 批次：本批的 4 个 key 是**页面鉴权与
// 菜单码收口**带出来的（提示出口的文案），混进别的主题会让「这批词条为什么存在」在
// review 时看不出来 —— 与 591 单独成文件的理由同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerAdminUINewKeysI18n() {
	registerAdminUINewKeysI18nOnce.Do(registerAdminUINewKeysI18nSeed)
}

var registerAdminUINewKeysI18nOnce sync.Once

func registerAdminUINewKeysI18nSeed() {
	// 592：4 个 key × 中英 = 8 行。
	//
	// 判定逐条枚举本批**全部 4 个 key 的两种语言**（= 8 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	// 前者会被同期其它批次的行满足而静默跳过，后者同理（本仓库踩过，见 226/277/283/592）。
	// 语言不能只数一种：原先只数 zh-CN（>= 4）时，中文齐、英文缺一条的库上判据仍返回 1 →
	// RunSeeds 跳过 → 缺的英文永远补不上（定则见 AGENTS.md §数据库「宁可重跑，不可静默跳过」）。
	// 阈值取**精确组合数 8**：低于它缺词也能满足，高于它判据永不满足、每次启动重跑。
	// 用例：public/migrations/recent_write_ui_seed_guard_test.go。
	registerSeed(Seed{
		Version:   "592-admin-ui-new-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.datarules.back_to_edit', 'admin.i18n.deleted', " +
			"'admin.products.actionDone', 'admin.products.bulk.noneSelected')",
		SQL: mustSQL("592_admin_ui_new_keys_i18n.sql"),
	})
}
