package migrations

import "sync"

// register_navigation_write_ui_i18n.go — 导航菜单页「JSON 写表单」的界面文案（迁移 600）。
//
// 单独成一个主题文件而不是挤进既有的 navigation 批次（290 是菜单树/译文语境、451 是页面提示页
// 时代的硬编码文案）：本批的两个 key 是**页面写动作从前端直调 JSON API**带出来的
// （无 JS 提示 + 结果区刷新入口），混进别的主题会让「这批词条为什么存在」在 review 时看不出来
// —— 与 593（区块页）/ 595（词条页）/ 598（联系人页）单独成文件的理由同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerNavigationWriteUII18n() {
	registerNavigationWriteUII18nOnce.Do(registerNavigationWriteUII18nSeed)
}

var registerNavigationWriteUII18nOnce sync.Once

func registerNavigationWriteUII18nSeed() {
	// 600：2 个 key × 中英 = 4 行。
	//
	// 判定逐条枚举本批**全部 2 个 key 的两种语言**（= 4 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	//   · 全库/前缀判据会被同期其它批次的行满足而静默跳过（本仓库踩过，见 226/277/283/592）；
	//   · **只看中文那 2 行**同样会静默跳过 —— 只要运营库里任意一条 en-US 缺失
	//     （历史库缺行、有人手工删过英文、或本批 SQL 曾半途失败），中文计数就已经满足，
	//     于是 RunSeeds 跳过 SQL，缺的英文**永远补不上**，英文界面回退到组件包内的中文兜底
	//     （错得安静：没有异常、没有日志）。review-sol 的 task-33 复验抓到这条 P2。
	// 定则来自 AGENTS.md §数据库：「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
	// 4 行齐全时判据为 1（跳过）；缺任意一 (key, lang) 即为 0（重跑）——
	// 而 SQL 是 ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值，
	// 所以这个偏差方向没有副作用。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言。
	registerSeed(Seed{
		Version:   "600-navigation-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.navigations.write.noJs', " +
			"'admin.navigations.write.refresh')",
		SQL: mustSQL("600_navigation_write_ui_i18n.sql"),
	})
}
