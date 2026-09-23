package migrations

import "sync"

// register_misc_page_err_i18n.go — 三个后台页面降级渲染所需的新增词条（408）。
//
// 见 408_i18n_misc_page_err.sql 的头部：masterdata / contenttemplate / analytics 三域各有一处
// `c.String(500, shell.MsgInternalError)`（工程列表装载失败时直写响应），页面上显示的是
// **未翻译的裸 key**，且脱掉了整个页壳。本批把三处改成降级渲染（空列表 + 归口提示 + 完整页面），
// 并让模板区分「空数据」与「装载失败」—— 而页面提示是**直接渲染的文本**
// （模板 t() 与 {{.Err}} 都不经过 response 的 translate），没有词条就只能硬编码中文，
// 英文页面上会整块回落中文。
//
// 本批新增 8 个 key × 2 语言（含 analytics 模块的归口 key ErrAnalyticsInternal：
// enums 里早有这个常量、JSON 出口也在用，只是从未登记过词条）。
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
func registerMiscPageErrI18n() {
	registerMiscPageErrI18nOnce.Do(registerMiscPageErrI18nSeed)
}

// registerMiscPageErrI18nOnce 让重复调用成为空操作。
var registerMiscPageErrI18nOnce sync.Once

// registerMiscPageErrI18nSeed 注册 408（真正干活的那一半）。
func registerMiscPageErrI18nSeed() {
	// 门槛 = 本批 8 个 key 的 **en-US** 行都在。
	//
	// 挑 en-US 而不是 zh-CN：本批 8 个 key 都是新的（两侧都应缺失），但按 en-US 计数与
	// 上游 403 的写法保持一致 —— 英文行缺失时页面在英文界面下会回落中文，是更难发现的那一半。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "408-i18n-misc-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.masterdata.loadFailed.lead','admin.masterdata.loadFailed.title'," +
			"'admin.masterdata.loadFailed.desc','admin.analytics.no_project.loadFailed'," +
			"'admin.content.templates.loadFailed.title','admin.content.templates.loadFailed'," +
			"'admin.content.templates.impact.loadFailed','ErrAnalyticsInternal')",
		SQL: mustSQL("408_i18n_misc_page_err.sql"),
	})
}

func init() { registerMiscPageErrI18n() }
