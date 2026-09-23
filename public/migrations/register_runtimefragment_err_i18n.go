package migrations

import "sync"

// register_runtimefragment_err_i18n.go — 运行时片段端点错误出口的受控文案（迁移 409）。
//
// 见 409_i18n_runtimefragment_err.sql 的头部：本批把 internal/module/runtimefragment/endpoint.go
// 两处 `c.String(400, err.Error())` 收口到 fragment_err.go 的统一出口，响应只出受控文案、
// 原文只进结构化日志。文案面向**访客**（访问面 + 多语言站点），所以必须走 sys_i18n，
// 不能硬编码中文 —— 硬编码会让英文站点的错误响应弹中文（293 修过的正是这一类）。
//
// 本批 5 个 key × 2 语言 = 10 行：
//
//	site.fragment.err.form_parse       —— POST 表单解析失败
//	site.fragment.err.too_many_params  —— 参数名数量超限
//	site.fragment.err.param_invalid    —— 参数名 / 值超长
//	site.fragment.err.context          —— 语义上下文不在枚举内
//	site.fragment.err.internal         —— 归口文案（判定表未命中时使用）
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerRuntimeFragmentErrI18n() {
	registerRuntimeFragmentErrI18nOnce.Do(registerRuntimeFragmentErrI18nSeed)
}

// registerRuntimeFragmentErrI18nOnce 让重复调用成为空操作。
var registerRuntimeFragmentErrI18nOnce sync.Once

// registerRuntimeFragmentErrI18nSeed 注册 409 的 seed（真正干活的那一半）。
func registerRuntimeFragmentErrI18nSeed() {
	// 门槛 = 本批 5 个 key 的 **zh-CN** 行都在（COUNT(DISTINCT item_key) >= 5）。
	//
	// 为什么枚举本批全部 5 个 key：单 key 判定会让「这批只插进去一半」（例如中途失败）
	// 被当成已完成；全库行数会被同期其它批次满足而静默跳过（156/157/158 与 226/277 都踩过）。
	// 挑 zh-CN 而不是 en-US：5 个 key 都是本批新增、两种语言的行同批插入，谁都行 ——
	// 选 zh-CN 与 register_fragment_login_panel_i18n.go（293）的口径对齐。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "409-runtimefragment-err-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'site.fragment.err.form_parse', 'site.fragment.err.too_many_params', " +
			"'site.fragment.err.param_invalid', 'site.fragment.err.context', " +
			"'site.fragment.err.internal')",
		SQL: mustSQL("409_i18n_runtimefragment_err.sql"),
	})
}

func init() { registerRuntimeFragmentErrI18n() }
