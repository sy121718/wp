package contentdto

// content_fragment_resp.go — 内容模块**片段出口**的渲染视图（htmx 局部替换用）。
//
// 与整页视图分开：片段的键集通常只有一个槽位，整页是一屏数据。两者的失败形态不同
// （整页失败要给页壳，片段失败只给一块），混在一个文件里会让这件事看不出来。
//
// **导出字段名就是模板契约**：internal/templates/fragments/*.html 下的 `.Field`。

// SeoScoreFragmentView SEO 评分区片段（fragments/seo_score）。
//
// 这个模板由内容 / 商品 / 工程三处共用 —— 所以视图只有一个槽位，形状必须与那三处一致。
type SeoScoreFragmentView struct {
	Score any
}

// ArticleImportFragmentView 文章导入预览片段（fragments/article_import）。
type ArticleImportFragmentView struct {
	Preview any
	// Err 词条事实（key + 兜底）；模板按当前语言取词。
	Err any
}

// StalePagesDrawerResp 全站「待重建页面」抽屉片段（admin/partials/stale_pages_drawer.html）。
//
// 片段出口的视图：键集只有五个槽位，比整页小得多。
type StalePagesDrawerResp struct {
	Rows  []StalePageRowResp
	Total int
	// Truncated 清单被截断（页面数超过一次取的上限），模板据此在末尾加一句「还有更多」。
	Truncated bool
	// Limit 本次取数上限（模板用它说明截断边界）。
	Limit int
	// MultiProject 工程数 > 1：单工程时「所属工程」列整列都是同一个名字，是纯噪声；
	// 多工程时缺了它又分不清两个工程同名的 /about。判据在 Go 侧算好，模板只读布尔。
	MultiProject bool
}
