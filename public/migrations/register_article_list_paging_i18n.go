package migrations

import "sync"

func registerArticleListPagingI18n() {
	registerArticleListPagingI18nOnce.Do(registerArticleListPagingI18nSeed)
}

var registerArticleListPagingI18nOnce sync.Once

func registerArticleListPagingI18nSeed() {

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
