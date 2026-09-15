// Package builddto build 模块请求/响应结构（构建任务队列，审计 DB-007）。
package builddto

import "time"

// Job 构建任务投影。
type Job struct {
	ID             string     `gorm:"column:id" json:"id"`
	SourceType     string     `gorm:"column:source_type" json:"sourceType"`
	SourceID       string     `gorm:"column:source_id" json:"sourceId"`
	DraftVersion   int64      `gorm:"column:draft_version" json:"draftVersion"`
	BuildInputHash string     `gorm:"column:build_input_hash" json:"buildInputHash"`
	Status         string     `gorm:"column:status" json:"status"`
	ArtifactID     *string    `gorm:"column:artifact_id" json:"artifactId,omitempty"`
	ErrorMessage   string     `gorm:"column:error_message" json:"errorMessage,omitempty"`
	CreatedAt      time.Time  `gorm:"column:create_time" json:"createdAt"`
	StartedAt      *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	CompletedAt    *time.Time `gorm:"column:completed_at" json:"completedAt,omitempty"`
}

// EnqueueReq 入队请求。
type EnqueueReq struct {
	SourceType     string `json:"sourceType" binding:"required"`
	SourceID       string `json:"sourceId" binding:"required"`
	DraftVersion   int64  `json:"draftVersion"`
	BuildInputHash string `json:"buildInputHash"`
}

// ListReq 任务列表请求。
type ListReq struct {
	Status string `form:"status" json:"status"`
	Limit  int    `form:"limit" json:"limit"`
}

// QueueStatsResp 队列状态（后台可见性）。
type QueueStatsResp struct {
	Pending    int64 `json:"pending"`
	Running    int64 `json:"running"`
	Failed     int64 `json:"failed"`
	Succeeded  int64 `json:"succeeded"`
	Superseded int64 `json:"superseded"`
	Total      int64 `json:"total"`
	// StaleReclaimed 本次查询顺手回收的僵尸任务数（running 超时被重置为 pending）。
	StaleReclaimed int   `json:"staleReclaimed"`
	RecentFailed   []Job `json:"recentFailed"`
}
