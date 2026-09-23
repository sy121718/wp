package migrations

import "sync"

// register_content_list_filter_i18n.go — 内容域四个列表页筛选栏的新增词条（413）。
//
// 见 413_i18n_content_list_filter.sql 的头部：articles / pages / blocks / navigations 四页
// 此前没有任何筛选入口（审计 02-L §2 P1-12）。本轮按「service 的 List 支持什么维度」逐页补：
//   · pages 做服务端筛选（工程下拉，?project= 进 page Service.ListReq.ProjectID）；
//   · articles / blocks / navigations 做客户端筛选（admin.js 的 [data-filter-input]）——
//     三者的 service 侧没有关键词 / 状态维度，为不越界改 service 走既有客户端过滤机制。
//
// 两种机制的文案都必须走 i18n：客户端筛选的「无匹配结果」提示是**直接渲染的文本**
// （不经过 pkg/response 的翻译层），没有词条就只能硬编码中文，英文界面永远显示中文。
//
// 本批新增 14 个 key × 2 语言 = 28 行：
//
//	admin.article.list.filterLabel / filterPlaceholder / filterEmpty
//	admin.pages.filter_project / filter_submit / filter_reset
//	admin.pages.empty_project_title / empty_project_desc
//	admin.blocks.filter_keyword / filter_placeholder / filter_empty
//	admin.navigations.filter_keyword / filter_placeholder / filter_empty
//
// 注册方式：本文件自带 init()（与 register_page_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerContentListFilterI18n() {
	registerContentListFilterI18nOnce.Do(registerContentListFilterI18nSeed)
}

// registerContentListFilterI18nOnce 让重复调用成为空操作。
var registerContentListFilterI18nOnce sync.Once

// registerContentListFilterI18nSeed 注册 413（真正干活的那一半）。
func registerContentListFilterI18nSeed() {
	// 门槛 = 本批 14 个 key 的 **en-US** 行都在（= 14 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "413-i18n-content-list-filter",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 14 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.article.list.filterLabel','admin.article.list.filterPlaceholder','admin.article.list.filterEmpty'," +
			"'admin.pages.filter_project','admin.pages.filter_submit','admin.pages.filter_reset'," +
			"'admin.pages.empty_project_title','admin.pages.empty_project_desc'," +
			"'admin.blocks.filter_keyword','admin.blocks.filter_placeholder','admin.blocks.filter_empty'," +
			"'admin.navigations.filter_keyword','admin.navigations.filter_placeholder','admin.navigations.filter_empty')",
		SQL: mustSQL("413_i18n_content_list_filter.sql"),
	})
}

func init() { registerContentListFilterI18n() }
