package contentdto

// Package contentdto content 模块请求/响应结构。

// CreateReq 新建内容实体。
type CreateReq struct {
	EntityType string         `json:"entityType" form:"entityType" binding:"required"`
	Slug       string         `json:"slug" form:"slug" binding:"required"`
	Data       map[string]any `json:"data" form:"data"`
}

// UpdateReq 更新内容实体（revision 递增）。
type UpdateReq struct {
	ID   string         `json:"id" form:"id" binding:"required"`
	Data map[string]any `json:"data" form:"data"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型分页列表。
type ListReq struct {
	EntityType string `form:"entityType"`
	Keyword    string `form:"keyword"`
	Limit      int    `form:"limit"`
	Offset     int    `form:"offset"`
}

// DeleteReq 删除。
type DeleteReq struct {
	ID string `form:"id" binding:"required"`
}
