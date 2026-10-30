package builddto

// Package builddto build 模块请求/响应结构（构建任务队列，审计 DB-007 / DB-01）。

import (
	"strings"

	"github.com/google/uuid"

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
	Intent         string `gorm:"column:intent" json:"intent,omitempty"`
	DraftVersion   int64  `gorm:"column:draft_version" json:"draftVersion"`
	BuildInputHash string `gorm:"column:build_input_hash" json:"buildInputHash"`
	// InputVersion menu_stale 的输入版本号（迁移 698）；其余来源为 0。
	// 消费侧用它确认「这条任务要重建的是哪一次改动」，也是 build_input_hash 的分量之一。
	InputVersion int64           `gorm:"column:input_version" json:"inputVersion,omitempty"`
	Status       string          `gorm:"column:status" json:"status"`
	ArtifactID   *string         `gorm:"column:artifact_id" json:"artifactId,omitempty"`
	ErrorMessage string          `gorm:"column:error_message" json:"errorMessage,omitempty"`
	Attempt      int             `gorm:"column:attempt" json:"attempt"`
	CreatedAt    utils.JSONTime  `gorm:"column:create_time" json:"createdAt"`
	StartedAt    *utils.JSONTime `gorm:"column:started_at" json:"startedAt,omitempty"`
	CompletedAt  *utils.JSONTime `gorm:"column:completed_at" json:"completedAt,omitempty"`
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

// ---- menu_stale 的来源标识（工程 + 菜单位置）----
//
// source_id 的取值口径属于**返回值形状**：worker 拿到任务后必须能凭它精确还原工程与位置，
// 入队侧也按同一口径拼装，两侧共用一个编解码才不会漂移。按 dto_shape_test.go 的命名判据
// （一个域只有 _req.go / _resp.go），这两段编解码并进 build_resp.go：它们既不是入参
// （没有绑定 tag），也不构成独立业务域，单开 *_menu_stale.go 会被判成不合规命名。

// MenuStaleSourceID 拼 menu_stale 的 source_id：规范 UUID + ":" + 位置。
//
// 入参的工程 id 不要求已经是规范形态：这里用 uuid.Parse(...).String() 归一，大小写变体
// 落到同一个 source_id（否则同一工程在队列里有两个来源，互斥与去重双双失效）。
// 非法 UUID 或空位置返回 ok=false，由 service 翻成参数错误。
func MenuStaleSourceID(projectID, kind string) (sourceID string, ok bool) {
	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return "", false
	}
	k := strings.TrimSpace(kind)
	if k == "" || strings.Contains(k, ":") {
		return "", false
	}
	return id.String() + ":" + k, true
}

// SplitMenuStaleSourceID 拆 menu_stale 的 source_id（worker 消费侧用这个还原工程与位置）。
//
// 只认「恰好两段、第一段是 UUID、第二段非空且不含 ':'」；任何偏差返回 ok=false ——
// 消费侧据此把任务判失败，而不是猜一个工程去读别人的数据。
func SplitMenuStaleSourceID(sourceID string) (projectID, kind string, ok bool) {
	raw := strings.TrimSpace(sourceID)
	if raw == "" {
		return "", "", false
	}
	idx := strings.Index(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return "", "", false
	}
	head, tail := raw[:idx], raw[idx+1:]
	if strings.Contains(tail, ":") {
		return "", "", false
	}
	id, err := uuid.Parse(head)
	if err != nil {
		return "", "", false
	}
	return id.String(), tail, true
}
