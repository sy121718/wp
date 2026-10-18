package contentdto

// Package contentdto content 模块请求/响应结构。

// ContentResp 内容实体响应。
type ContentResp struct {
	ID         string         `json:"id"`
	EntityType string         `json:"entityType"`
	Slug       string         `json:"slug"`
	Revision   int64          `json:"revision"`
	Data       map[string]any `json:"data"`
	UpdatedAt  string         `json:"updatedAt"`
}

// PreviewNoticeResp 预览 iframe 里的替代页（还没保存 / 还没有详情页模板）。
//
// 只有两个键：Text 是已按请求语言取好的一句话，Lang 落 html 的 lang 属性。
// 条件与样式在 admin/preview_notice.html —— 这里只是数据。
type PreviewNoticeResp struct {
	Text string
	Lang string
}
