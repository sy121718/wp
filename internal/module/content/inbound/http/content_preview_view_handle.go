package contenthttp

// content_preview_view_handle.go — 预览 iframe 替代页的渲染视图。
//
// 只被页面出口消费（admin/preview_notice.html 经 pageview.RenderTemplate 渲染），
// 所以与其它页面视图一样住在控制器层。

// PreviewNoticeResp 预览 iframe 里的替代页（还没保存 / 还没有详情页模板）。
//
// 只有两个键：Text 是已按请求语言取好的一句话，Lang 落 html 的 lang 属性。
// 条件与样式在 admin/preview_notice.html —— 这里只是数据。
type PreviewNoticeResp struct {
	Text string
	Lang string
}
