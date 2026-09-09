// Package pagedto 发布链路请求/响应结构。
package pagedto

// BuildReq 基于当前草稿构建产物（暂存，不激活）。
type BuildReq struct {
	ID              string `json:"id" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion"`
	// Lang 构建语言（多语言 P2）：空 = 站点默认语言（i18n.default_lang）。
	Lang string `json:"lang"`
}

// PublishReq 激活暂存产物。
type PublishReq struct {
	ID string `json:"id" binding:"required"`
	// Lang 发布语言：必须与构建语言一致，否则暂存产物与激活路径不匹配。
	Lang string `json:"lang"`
}

// RollbackReq 回滚到指定历史产物。
type RollbackReq struct {
	ID         string `json:"id" binding:"required"`
	TargetHash string `json:"targetHash" binding:"required"`
	// Lang 回滚语言（多语言 P3）：留空时取目标产物冻结的 Manifest.lang，
	// 二者都为空才回退站点默认语言。回滚只作用于该语言的激活状态与路由。
	Lang string `json:"lang"`
}

// UpdateURLReq 修改访问路径并按策略处理旧路径。
type UpdateURLReq struct {
	ID           string `json:"id" binding:"required"`
	NewPath      string `json:"newPath" binding:"required"`
	WithRedirect bool   `json:"withRedirect"`
	// Lang 目标语言（多语言 P2）：决定新路径的实际访问前缀，空 = 站点默认语言。
	Lang string `json:"lang"`
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
