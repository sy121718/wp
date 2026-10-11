package migrations

import "sync"

// register_navigation_sort_tied_i18n.go — 导航同级排序并列的业务文案（迁移 602）。
//
// 单独成一个主题文件而不是塞进既有 navigation 批次（290 菜单树/译文语境、451 页面提示页
// 时代的硬编码文案、600 JSON 写表单、601 来源批量加入）：本批只有一句，但它是**一个新的
// 业务错误口径**（ErrSortTied，同级排序并列 → 零写），与上面几批都不是同一件事 ——
// 混在一起会让「这条词条为什么存在」在 review 时看不出来。
//
// 新增主题要在 register.go 的 init() 里加一行调用。
func registerNavigationSortTiedI18n() {
	registerNavigationSortTiedI18nOnce.Do(registerNavigationSortTiedI18nSeed)
}

var registerNavigationSortTiedI18nOnce sync.Once

func registerNavigationSortTiedI18nSeed() {
	// 602：1 个 key × 中英 = 2 行。
	//
	// 判定逐条枚举本批**全部 1 个 key 的两种语言**（= 2 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	//   · 全库/前缀判据会被同期其它批次的行满足而静默跳过（本仓库踩过，见 226/277/283/592）；
	//   · **只看中文那行**同样会静默跳过 —— 只要运营库里 en-US 缺失（历史库缺行、有人手工删过
	//     英文、或本批 SQL 曾半途失败），中文计数就已满足，RunSeeds 跳过 SQL，缺的英文
	//     **永远补不上**，英文界面回退到组件包内的中文兜底（错得安静：没有异常、没有日志）。
	// 定则来自 AGENTS.md §数据库：「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
	// 2 行齐全时判据为 1（跳过）；缺任意一 (key, lang) 即为 0（重跑）——
	// 而 SQL 是 ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值。
	registerSeed(Seed{
		Version:   "602-navigation-sort-tied-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('ErrSortTied')",
		SQL: mustSQL("602_navigation_sort_tied_i18n.sql"),
	})
}
