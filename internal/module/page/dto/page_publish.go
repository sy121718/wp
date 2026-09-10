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

// RebuildArtifactReq 按产物元数据重建丢失的产物文件（灾难恢复）。
type RebuildArtifactReq struct {
	ArtifactID string `json:"artifactId" binding:"required"`
}

// RebuildArtifactResp 产物重建结果。
type RebuildArtifactResp struct {
	ArtifactID   string `json:"artifactId"`
	Lang         string `json:"lang"`
	Path         string `json:"path"`
	ExpectedHash string `json:"expectedHash"`
	ActualHash   string `json:"actualHash"`
	// Restored 产物文件现已可用（文件本就存在，或重建后 hash 一致）。
	Restored bool `json:"restored"`
	// HashMatched 重建结果与元数据记录的 hash 一致（hash 相同才算真的恢复）。
	HashMatched bool `json:"hashMatched"`
	// AlreadyThere 文件原本就在，未执行编译（幂等短路）。
	AlreadyThere bool `json:"alreadyThere"`
	// Reason 重建产物与元数据不一致时的差异说明。
	Reason string `json:"reason"`
}

// PublicationIssue 一条异常激活链接（巡检报告项）。
type PublicationIssue struct {
	URLPath string `json:"urlPath"`
	Link    string `json:"link"`
	Reason  string `json:"reason"`
}

// PublicationAuditResp 激活面巡检报告。
type PublicationAuditResp struct {
	Checked int                `json:"checked"`
	Issues  []PublicationIssue `json:"issues"`
	Healthy bool               `json:"healthy"`
}

// GCArtifactsReq 产物回收请求。
type GCArtifactsReq struct {
	// RetentionDays 保留窗口（天）：早于 now-retentionDays 创建、且不再被任何指针引用的
	// 产物才可回收。留空或 <=0 按默认 30 天。
	RetentionDays int `json:"retentionDays"`
	// DryRun 只列出候选、不实际删除。留空视为 true（安全默认：必须显式传 false 才真删）。
	DryRun *bool `json:"dryRun"`
}

// GCRecoveredArtifact 单条回收结果。
type GCRecoveredArtifact struct {
	ID           string `json:"id"`
	ArtifactHash string `json:"artifactHash"`
	Lang         string `json:"lang"`
	// Action: would_delete / deleted / kept_shared / delete_failed / state_failed
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// GCArtifactsResp 产物回收报告。
type GCArtifactsResp struct {
	RetentionDays int                   `json:"retentionDays"`
	DryRun        bool                  `json:"dryRun"`
	Scanned       int                   `json:"scanned"`
	Deleted       int                   `json:"deleted"`
	SkippedShared int                   `json:"skippedShared"`
	Failed        int                   `json:"failed"`
	Items         []GCRecoveredArtifact `json:"items"`
}
