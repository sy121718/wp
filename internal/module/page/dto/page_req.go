package pagedto

import "encoding/json"

// CreateReq 创建手工 Page。
type CreateReq struct {
	ProjectID         string          `json:"projectId" binding:"required"`
	Kind              string          `json:"kind" binding:"required"`
	ContentTargetType string          `json:"contentTargetType"`
	ContentTargetID   *string         `json:"contentTargetId"`
	DraftPath         string          `json:"draftPath" binding:"required"`
	DraftDocument     json.RawMessage `json:"draftDocument" binding:"required"`
}

// SaveDraftReq 保存 Page 草稿，使用 draftVersion 做乐观锁。
type SaveDraftReq struct {
	ID              string          `json:"id" binding:"required"`
	ExpectedVersion int64           `json:"expectedVersion"`
	DraftPath       string          `json:"draftPath" binding:"required"`
	DraftDocument   json.RawMessage `json:"draftDocument" binding:"required"`
}

// DetailReq 查询 Page。
type DetailReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}

// RevisionReq 查询 Page 修订历史。
type RevisionReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
}

// DeleteReq 软删 Page（释放其路径占用，同路径可被新页面重新占用）。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}
