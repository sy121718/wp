package builddto

import (
	"strings"

	"github.com/google/uuid"

	"go_wp/pkg/utils"
)

type Job struct {
	ID         string `gorm:"column:id" json:"id"`
	SourceType string `gorm:"column:source_type" json:"sourceType"`
	SourceID   string `gorm:"column:source_id" json:"sourceId"`

	ProjectID string `gorm:"column:project_id" json:"projectId,omitempty"`

	Lang string `gorm:"column:lang" json:"lang,omitempty"`

	Intent         string `gorm:"column:intent" json:"intent,omitempty"`
	DraftVersion   int64  `gorm:"column:draft_version" json:"draftVersion"`
	BuildInputHash string `gorm:"column:build_input_hash" json:"buildInputHash"`

	InputVersion int64           `gorm:"column:input_version" json:"inputVersion,omitempty"`
	Status       string          `gorm:"column:status" json:"status"`
	ArtifactID   *string         `gorm:"column:artifact_id" json:"artifactId,omitempty"`
	ErrorMessage string          `gorm:"column:error_message" json:"errorMessage,omitempty"`
	Attempt      int             `gorm:"column:attempt" json:"attempt"`
	CreatedAt    utils.JSONTime  `gorm:"column:create_time" json:"createdAt"`
	StartedAt    *utils.JSONTime `gorm:"column:started_at" json:"startedAt,omitempty"`
	CompletedAt  *utils.JSONTime `gorm:"column:completed_at" json:"completedAt,omitempty"`
}

type QueueStatsResp struct {
	Pending    int64 `json:"pending"`
	Running    int64 `json:"running"`
	Failed     int64 `json:"failed"`
	Succeeded  int64 `json:"succeeded"`
	Superseded int64 `json:"superseded"`
	Total      int64 `json:"total"`

	StaleReclaimed int `json:"staleReclaimed"`

	StaleMerged  int   `json:"staleMerged"`
	RecentFailed []Job `json:"recentFailed"`
}

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
