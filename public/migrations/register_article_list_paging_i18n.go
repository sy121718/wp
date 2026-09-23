package migrations

import "sync"

// register_article_list_paging_i18n.go — 文章列表页分页落地后的词条修正（420）。
//
// 见 420_fix_article_list_paging_i18n.sql 的头部：/admin/articles 此前一次取 50 条、
// 无分页参数，页头说明也写着「一次最多列出 50 篇（超过这个量级再谈分页）」。本批把列表
// 改成真源分页（总数来自内容契约的 Count、页码走 shell.PageParams），因此**修正文案的库值** ——
// 只改模板等于没改（模板里的中文只是 t(key, 兜底) 的 fallback，词条命中时显示库里的值）。
//
// 本批不新增词条：分页信息行与翻页按钮复用 shell.pagination.*（迁移 059 已登记中英），
// 列表工具栏的「全部文章（N）」也已存在（admin.article.list.allHeading / allHeadingClose）。
//
// 注册方式：本文件自带 init()（与 register_trade_pages_i18n.go 同形），不需要改 register.go。
// init 的注册顺序不影响执行顺序 —— 种子按版本号排序，RunSeeds 在所有结构迁移之后执行。
func registerArticleListPagingI18n() {
	registerArticleListPagingI18nOnce.Do(registerArticleListPagingI18nSeed)
}

// registerArticleListPagingI18nOnce 让重复调用成为空操作。
var registerArticleListPagingI18nOnce sync.Once

// registerArticleListPagingI18nSeed 注册 420（真正干活的那一半）。
func registerArticleListPagingI18nSeed() {
	// 门槛 = 2 条：两个 lang 行的值都已不是旧值。
	//
	// 判据用 `item_value <> '<旧值>'` 而不是 `= '<新值>'`（与 417 同口径）：运营在后台
	// 手工改过文案时也算本批已落地（seed 是默认值来源、后台是真相来源），不该每次启动重跑。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 与文案
	// 只能写成 SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "420-fix-article-list-paging-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key = 'admin.article.list.footHint' AND lang = 'zh-CN' AND item_value <> '按更新时间倒序排列，一次最多列出 50 篇（超过这个量级再谈分页）。「未发布」表示这篇文章还没有线上详情页 —— 不一定是问题（草稿就该是未发布），编辑页的「发布」区块里能看到它当前缺什么。') OR " +
			"(item_key = 'admin.article.list.footHint' AND lang = 'en-US' AND item_value <> 'Sorted by update time, newest first; at most 50 are listed at a time (pagination is a conversation for a larger scale). \"Unpublished\" means the article has no public detail page yet — that is not necessarily a problem (a draft should be unpublished), and the publishing section of the edit page shows what it is still missing.')",
		SQL: mustSQL("420_fix_article_list_paging_i18n.sql"),
	})
}

func init() { registerArticleListPagingI18n() }
