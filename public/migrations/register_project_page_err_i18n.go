package migrations

import "sync"

// register_project_page_err_i18n.go — 项目域页面错误出口收口的新增词条（403）。
//
// 见 403_i18n_project_page_err.sql 的头部：本批把 project 域 20 处 `c.String(4xx, "…")`
// 改成「303 + ?err=」/「回渲染表单页 + 错误槽」，而页面提示是**直接渲染的文本**
// （模板 {{.Err}} 不经过 response 的 translate）—— 没有词条就只能硬编码中文，
// 英文页面上会原样显示中文。
//
// 本批新增 8 个 key × 2 语言，并为既有的 MsgThemeSettingsInvalid 补 en-US（此前只有 zh-CN）：
//   ErrProjectRequired             —— 页面入口没有工程作用域（存量 key，此前从未登记词条）
//   ErrThemeIDRequired             —— 主题设置页缺 id
//   MsgThemeSettingsInvalid        —— 主题设置未过 CSS 值白名单（补 en-US）
//   MsgThemeSettingsRefreshFailed  —— 保存成功但整站刷新失败（部分成功）
//   ErrSiteSettingsNameRequired    —— 站点设置缺工程或站点名
//   ErrGA4IDInvalid                —— GA4 测量 ID 非法
//   ErrGSCVerificationInvalid      —— GSC 验证 token 非法
//   ErrNotFoundHTMLTooLong         —— 自定义 404 页超长
//   ErrLangURLModeInvalid          —— 语言 URL 方案非法
//
// 注册方式：本文件自带 init()（与 register_product_inventory_empty_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerProjectPageErrI18n() {
	registerProjectPageErrI18nOnce.Do(registerProjectPageErrI18nSeed)
}

// registerProjectPageErrI18nOnce 让重复调用成为空操作。
var registerProjectPageErrI18nOnce sync.Once

// registerProjectPageErrI18nSeed 注册 403（真正干活的那一半）。
func registerProjectPageErrI18nSeed() {
	// 门槛 = 本批 9 个 key 的 **en-US** 行都在 —— 挑 en-US 而不是 zh-CN 是有意的：
	// 这 9 条里有 5 条（zh-CN）在别的迁移里已经存在（MsgThemeSettingsInvalid 早就有了），
	// 按 zh-CN 计数会让门槛在「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "403-i18n-project-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 9 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'ErrProjectRequired','ErrThemeIDRequired','MsgThemeSettingsInvalid','MsgThemeSettingsRefreshFailed'," +
			"'ErrSiteSettingsNameRequired','ErrGA4IDInvalid','ErrGSCVerificationInvalid'," +
			"'ErrNotFoundHTMLTooLong','ErrLangURLModeInvalid')",
		SQL: mustSQL("403_i18n_project_page_err.sql"),
	})
}

func init() { registerProjectPageErrI18n() }
