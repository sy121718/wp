// Package pagedto 发布链路请求/响应结构。
package pagedto

// BuildReq 基于当前草稿构建产物（暂存，不激活）。
type BuildReq struct {
	ID              string `json:"id" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

// PublishReq 激活暂存产物。
type PublishReq struct {
	ID string `json:"id" binding:"required"`
}

// RollbackReq 回滚到指定历史产物。
type RollbackReq struct {
	ID         string `json:"id" binding:"required"`
	TargetHash string `json:"targetHash" binding:"required"`
}

// UpdateURLReq 修改访问路径并按策略处理旧路径。
type UpdateURLReq struct {
	ID           string `json:"id" binding:"required"`
	NewPath      string `json:"newPath" binding:"required"`
	WithRedirect bool   `json:"withRedirect"`
}

// PublishResp 发布链路操作结果。
type PublishResp struct {
	PageID      string `json:"pageId"`
	Status      string `json:"status"`
	StagedHash  string `json:"stagedHash,omitempty"`
	ActiveHash  string `json:"activeHash,omitempty"`
	OldPath     string `json:"oldPath,omitempty"`
	DraftPath   string `json:"draftPath"`
	PublishedAt string `json:"publishedAt,omitempty"`
}
