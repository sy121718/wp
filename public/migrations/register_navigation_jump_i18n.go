package migrations

import "sync"

// register_navigation_jump_i18n.go — 导航菜单页写动作提示页的词条（迁移 591）。
//
// 单独成一个主题文件、而不是挤进 register_navigation_menu_i18n.go：后者是导航页的
// 「模板补漏词条」（290），本批是**写动作出口改造**引入的新文案 —— 混在一起会让
// 「这批词条为什么存在」在 review 时看不出来。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerNavigationJumpI18n() {
	registerNavigationJumpI18nOnce.Do(registerNavigationJumpI18nSeed)
}

var registerNavigationJumpI18nOnce sync.Once

func registerNavigationJumpI18nSeed() {
	// 591：1 个 key × 中英 = 2 行。
	//
	// 判定枚举本批**全部 1 个 key 的两种语言**（= 2 个 (item_key, lang) 组合），不用全库行数：
	// 用全库行数会被同期其它批次的行满足而静默跳过（本仓库踩过，理由见 226/277/283）。
	// 语言也不能只数一种：原先只数 zh-CN（>= 1）时，zh 在、en 缺的库上判据仍返回 1 →
	// RunSeeds 跳过 → 缺的英文永远补不上（定则见 AGENTS.md §数据库「宁可重跑，不可静默跳过」）。
	// 阈值取**精确组合数 2**：低于它缺词也能满足，高于它判据永不满足、每次启动重跑。
	// ConditionSQL 里没有占位符 —— 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、
	// 每次启动重跑（178 踩过）。用例：public/migrations/recent_write_ui_seed_guard_test.go。
	registerSeed(Seed{
		Version:   "591-navigation-jump-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('admin.navigations.actionDone')",
		SQL: mustSQL("591_navigation_jump_i18n.sql"),
	})
}
