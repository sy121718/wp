package pagedto

import "encoding/json"

// CreateReq 创建手工 Page。
type CreateReq struct {
	ProjectID         string  `json:"projectId" binding:"required"`
	Kind              string  `json:"kind" binding:"required"`
	ContentTargetType string  `json:"contentTargetType"`
	ContentTargetID   *string `json:"contentTargetId"`
	DraftPath         string  `json:"draftPath" binding:"required"`
	// DraftDocument 初始文档。与 BlueprintID 二选一：给了蓝图就以蓝图为准
	//（蓝图是「用完即弃」的初始化输入，AST 会被完整复制并重生成节点 ID）。
	// 因此这里不再是 required —— 从蓝图建页时前端不需要先造一份空文档。
	DraftDocument json.RawMessage `json:"draftDocument"`
	// BlueprintID 从哪份蓝图初始化（可选，审计 VIS-010）。
	BlueprintID string `json:"blueprintId"`
}

// SaveDraftReq 保存 Page 草稿，使用 draftVersion 做乐观锁。
type SaveDraftReq struct {
	ID              string          `json:"id" binding:"required"`
	ExpectedVersion int64           `json:"expectedVersion"`
	DraftPath       string          `json:"draftPath" binding:"required"`
	DraftDocument   json.RawMessage `json:"draftDocument" binding:"required"`
}

// ListReq 列出 Page 摘要（必须带工程 scope，防跨工程越权）。
type ListReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ThemeID   string `form:"themeId" json:"themeId"`
}

// DetailReq 查询 Page。
type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

// RevisionReq 查询 Page 修订历史。
type RevisionReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
}

// DeleteReq 软删 Page（释放其路径占用，同路径可被新页面重新占用）。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}
