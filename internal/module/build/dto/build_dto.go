// Package builddto build 模块请求/响应结构（构建任务队列，审计 DB-007 / DB-01）。
package builddto

import (
	"go_wp/pkg/utils"
)

// Job 构建任务投影。
type Job struct {
	ID         string `gorm:"column:id" json:"id"`
	SourceType string `gorm:"column:source_type" json:"sourceType"`
	SourceID   string `gorm:"column:source_id" json:"sourceId"`
	// ProjectID 任务所属工程（迁移 295 起显式入库）；来源模块拿不到工程时为空。
	ProjectID string `gorm:"column:project_id" json:"projectId,omitempty"`
	// Lang 构建语言；空串 = 默认语言（ARCH-04 之前的生产者一律为空串）。
	Lang string `gorm:"column:lang" json:"lang,omitempty"`
	// Intent 构建意图（manual / dependency），后台据此区分人工构建与依赖重建。
	Intent         string          `gorm:"column:intent" json:"intent,omitempty"`
	DraftVersion   int64           `gorm:"column:draft_version" json:"draftVersion"`
	BuildInputHash string          `gorm:"column:build_input_hash" json:"buildInputHash"`
	Status         string          `gorm:"column:status" json:"status"`
	ArtifactID     *string         `gorm:"column:artifact_id" json:"artifactId,omitempty"`
	ErrorMessage   string          `gorm:"column:error_message" json:"errorMessage,omitempty"`
	Attempt        int             `gorm:"column:attempt" json:"attempt"`
	CreatedAt      utils.JSONTime  `gorm:"column:create_time" json:"createdAt"`
	StartedAt      *utils.JSONTime `gorm:"column:started_at" json:"startedAt,omitempty"`
	CompletedAt    *utils.JSONTime `gorm:"column:completed_at" json:"completedAt,omitempty"`
}

// EnqueueReq 入队请求。
//
// Intent 留空时按 dependency（依赖重建）入队：当前唯一的入队来源是依赖失效扇出，
// 人工构建要显式传 manual —— 默认值必须站在**多数且更安全**的一侧，
// 依赖重建走的是「有就排上、没有就算了」的幂等语义，误判成人工构建会让后台列表失真。
type EnqueueReq struct {
	SourceType string `json:"sourceType" binding:"required"`
	SourceID   string `json:"sourceId" binding:"required"`
	// ProjectID 任务所属工程（显式入库，消费侧不必再反推来源行）。
	ProjectID string `json:"projectId"`
	// Lang 构建语言；留空 = 默认语言。
	Lang           string `json:"lang"`
	Intent         string `json:"intent"`
	DraftVersion   int64  `json:"draftVersion"`
	BuildInputHash string `json:"buildInputHash"`
}

// ListReq 任务列表请求。
type ListReq struct {
	// ProjectID 只列该工程的任务（空 = 全队列）。build_jobs **没有** RLS 策略，
	// 工程过滤只能靠这个条件，见 build/model 包注释。
	ProjectID string `form:"project" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Limit     int    `form:"limit" json:"limit"`
}

// QueueStatsResp 队列状态（后台可见性）。
type QueueStatsResp struct {
	Pending    int64 `json:"pending"`
	Running    int64 `json:"running"`
	Failed     int64 `json:"failed"`
	Succeeded  int64 `json:"succeeded"`
	Superseded int64 `json:"superseded"`
	Total      int64 `json:"total"`
	// StaleReclaimed 本次查询顺手回收的僵尸任务数（租约到期被退回 pending）。
	StaleReclaimed int `json:"staleReclaimed"`
	// StaleMerged 本次顺手合并的重复陈旧任务数（同键已有待办，被标 superseded）。
	StaleMerged  int   `json:"staleMerged"`
	RecentFailed []Job `json:"recentFailed"`
}
