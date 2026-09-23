package migrations

import "sync"

// register_block_page_err_i18n.go — block 域「工作台保存块内容」失败出口收口的新增词条（405）。
//
// 见 405_i18n_block_page_err.sql 的头部：本批把 POST /admin/blocks/save-content 的两条失败
// 分支从 `c.String(4xx, "中文硬编码")` 改成模块既有归口出口（paramBindFail /
// blockErrorStatus + blockErrorMessage），响应回到 JSON、文案回到 i18n key。
//
// 注册方式与 register_project_page_err_i18n.go 同形：本文件自带 init()（与
// register_product_inventory_empty_i18n.go 一致）。init 的注册顺序不影响执行顺序 ——
// 种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
func registerBlockPageErrI18n() {
	registerBlockPageErrI18nOnce.Do(registerBlockPageErrI18nSeed)
}

// registerBlockPageErrI18nOnce 让重复调用成为空操作。
var registerBlockPageErrI18nOnce sync.Once

// registerBlockPageErrI18nSeed 注册 405（真正干活的那一半）。
func registerBlockPageErrI18nSeed() {
	// 门槛 = 本批 2 行的 **en-US** 都在 —— 挑 en-US 而不是 zh-CN 是有意的：
	// 这两个 key 的 zh-CN 行早在 058 就存在，按 zh-CN 计数会让门槛在「本批还没跑」时
	// 就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key
	// 只能写成 SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "405-i18n-block-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN ('ErrBlockNotFound','MsgInternalError')",
		SQL: mustSQL("405_i18n_block_page_err.sql"),
	})
}

func init() { registerBlockPageErrI18n() }
