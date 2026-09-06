// Package contentdto content 模块请求/响应结构。
package contentdto

// CreateReq 新建内容实体。
type CreateReq struct {
	EntityType string         `json:"entityType" binding:"required"`
	Slug       string         `json:"slug" binding:"required"`
	Data       map[string]any `json:"data"`
}

// UpdateReq 更新内容实体（revision 递增）。
type UpdateReq struct {
	ID   string         `json:"id" binding:"required"`
	Data map[string]any `json:"data"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型分页列表。
type ListReq struct {
	EntityType string `form:"entityType"`
	Limit      int    `form:"limit"`
	Offset     int    `form:"offset"`
}

// DeleteReq 删除。
type DeleteReq struct {
	ID string `form:"id" binding:"required"`
}

// ContentResp 内容实体响应。
type ContentResp struct {
	ID         string         `json:"id"`
	EntityType string         `json:"entityType"`
	Slug       string         `json:"slug"`
	Revision   int64          `json:"revision"`
	Data       map[string]any `json:"data"`
	UpdatedAt  string         `json:"updatedAt"`
}
