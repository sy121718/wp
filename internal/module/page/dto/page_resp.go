package pagedto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type PageResp struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"projectId"`
	ThemeID           string          `json:"themeId,omitempty"`
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
	CreatedAt         utils.JSONTime  `json:"createdAt"`
	UpdatedAt         utils.JSONTime  `json:"updatedAt"`

	Publications []PagePublicationResp `json:"publications,omitempty"`

	LangStatus string `json:"langStatus,omitempty"`
}

type PagePublicationResp struct {
	Lang         string          `json:"lang"`
	ActivePath   string          `json:"activePath"`
	ArtifactID   *string         `json:"artifactId,omitempty"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	PublishedAt  *utils.JSONTime `json:"publishedAt,omitempty"`
}

type PageDraftResp struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	DraftPath     string          `json:"draftPath"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     utils.JSONTime  `json:"updatedAt"`
}

type PageTitleResp struct {
	ID string `json:"id"`

	Kind      string `json:"kind"`
	DraftPath string `json:"draftPath"`

	ActivePath *string `json:"activePath,omitempty"`
	SEOTitle   string  `json:"seoTitle"`
}

type RevisionResp struct {
	ID            string          `json:"id"`
	PageID        string          `json:"pageId"`
	Version       int64           `json:"version"`
	DraftPath     string          `json:"draftPath"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	SourceHash    string          `json:"sourceHash"`
	CreatedAt     utils.JSONTime  `json:"createdAt"`
}
