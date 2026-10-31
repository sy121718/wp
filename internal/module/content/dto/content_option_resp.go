package contentdto

// content_option_resp.go — 内容模块仍留在 dto 的响应类型。
//
// 页面渲染视图（下拉选项 / 列表 / 编辑 / 片段）已搬到 inbound/http 的 *view_handle.go；
// 这里只保留接口契约侧仍在用的形状。

// ArticleStaleImpactResp 文章列表页的「依赖失效影响面」区块。
//
// 与 page.StaleOverviewResp 同形但独立声明：两者回答的是同一件事（哪些页面待重建），
// 但一个是文章列表页的页头区块、一个是页面管理页的页头区块 ——
// 合成一个会让「改这页的区块」牵动那页。
type ArticleStaleImpactResp struct {
	Available bool
	Pages     any
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}
