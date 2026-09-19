package migrations

import "sync"

// register_fragment_login_panel_i18n.go — 运行时片段「登录面板」的访客文案（迁移 293）。
//
// 单独一个主题文件：这批词条只服务于 runtimefragment/capability.go 的 loginPanel 形态
// （登录 / 注册 · 继续购物），与 156（购物车 / 结算 / 库存 / 搜索）是不同主题。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册 —— 注册顺序在 diff 里可见是有意的）。
func registerFragmentLoginPanelI18n() {
	registerFragmentLoginPanelI18nOnce.Do(registerFragmentLoginPanelI18nSeed)
}

// registerFragmentLoginPanelI18nOnce 让重复调用成为空操作（不会重复 registerSeed）。
var registerFragmentLoginPanelI18nOnce sync.Once

// registerFragmentLoginPanelI18nSeed 注册 293 的 seed（真正干活的那一半，被 Once 包一层）。
func registerFragmentLoginPanelI18nSeed() {
	// 293：2 个 key × 中英 = 4 行。
	//
	// 判定枚举本批**全部 2 个 key**（COUNT(DISTINCT item_key) >= 2），不用单 key 也不
	// 用全库行数：单 key 判定会让「这批只插进去一半」（例如中途失败）被当成已完成；
	// 全库行数会被同期其它批次满足而静默跳过（156/157/158 的台账与 226/277 都踩过）。
	// Seed 的 ConditionSQL **不带参数**执行（db.Raw(sql)），不要写 ? 占位符 ——
	// 写了会判定恒为 0，于是每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "293-fragment-login-panel-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'site.fragment.login_panel.login_register', 'site.fragment.login_panel.continue_shopping')",
		SQL: mustSQL("293_fragment_login_panel_i18n.sql"),
	})
}
