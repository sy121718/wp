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
