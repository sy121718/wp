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
