package pagedto

import (
	"encoding/json"
	"time"
)

// PageResp Page 当前草稿与发布投影。
type PageResp struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"projectId"`
	Kind              string          `json:"kind"`
	ContentTargetType string          `json:"contentTargetType"`
	ContentTargetID   *string         `json:"contentTargetId,omitempty"`
	DraftPath         string          `json:"draftPath"`
	ActivePath        *string         `json:"activePath,omitempty"`
	StagedArtifactID  *string         `json:"stagedArtifactId,omitempty"`
	ActiveArtifactID  *string         `json:"activeArtifactId,omitempty"`
	DraftDocument     json.RawMessage `json:"draftDocument"`
	DraftVersion      int64           `json:"draftVersion"`
	Stale             bool            `json:"stale"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

// RevisionResp Page 草稿修订快照，用于历史记录/Undo/Redo/版本对比。
type RevisionResp struct {
	ID            string          `json:"id"`
	PageID        string          `json:"pageId"`
	Version       int64           `json:"version"`
	DraftPath     string          `json:"draftPath"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	SourceHash    string          `json:"sourceHash"`
	CreatedAt     time.Time       `json:"createdAt"`
}
