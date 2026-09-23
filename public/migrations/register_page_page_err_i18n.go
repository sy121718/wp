package migrations

import "sync"

// register_page_page_err_i18n.go — page 域后台页面失败出口收口的新增词条（404）。
//
// 见 404_i18n_page_page_err.sql 的头部：pages_handle.go 的两处 `c.String(400, "中文硬编码")`
// 与 site_slot_handle.go 的 `c.String(500, 受控文案)` 脱掉了页壳，本批按「调用方读什么」
// 分别改成「303 回列表页 + ?err=」与「降级渲染」。而 ?err= 上的文案是**直接渲染的文本**
// （admin/pages.html 的 {{.Err}}，不经过 pkg/response 的翻译层）—— 没有词条就只能硬编码中文。
//
// 本批新增 2 个 key × 2 语言：
//
//	page.form.projectNameRequired —— 新建站点工程时名称为空（handler 自造回执）
//	page.form.pathRequired        —— 新建页面时路径为空（handler 自造回执）
//
// 缺工程 id 那条不在本批词条里：它复用既有 ErrProjectRequired（403 已登记中英词条）。
//
// 注册方式：本文件自带 init()（与 register_project_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerPagePageErrI18n() {
	registerPagePageErrI18nOnce.Do(registerPagePageErrI18nSeed)
}

// registerPagePageErrI18nOnce 让重复调用成为空操作。
var registerPagePageErrI18nOnce sync.Once

// registerPagePageErrI18nSeed 注册 404（真正干活的那一半）。
func registerPagePageErrI18nSeed() {
	// 门槛 = 本批 2 个 key 的 **en-US** 行都在（= 2 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "404-i18n-page-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'page.form.projectNameRequired','page.form.pathRequired')",
		SQL: mustSQL("404_i18n_page_page_err.sql"),
	})
}

func init() { registerPagePageErrI18n() }
