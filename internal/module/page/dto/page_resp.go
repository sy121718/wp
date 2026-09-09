package pagedto

import (
	"encoding/json"
	"time"
)

// PageResp Page 当前草稿与发布投影。
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
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	// Publications 每语言激活状态（多语言 P3，docs/06-D §15.5 第 2 条）。
	// ActivePath/ActiveArtifactID 是「最近发布语言」的单值镜像，多语言真源在此列表。
	Publications []PagePublicationResp `json:"publications,omitempty"`
}

// PagePublicationResp 页面某语言的激活状态投影。
type PagePublicationResp struct {
	Lang         string     `json:"lang"`
	ActivePath   string     `json:"activePath"`
	ArtifactID   *string    `json:"artifactId,omitempty"`
	ArtifactHash string     `json:"artifactHash,omitempty"`
	PublishedAt  *time.Time `json:"publishedAt,omitempty"`
}

// PageDraftResp 页面草稿文档投影（多语言 P5c 翻译工作台的全站扫描用：只读，不参与编辑）。
//
// 与 PageResp 的区别：只带「扫描可翻译候选」所需的字段，不取发布/暂存指针，
// 且明确包含 DraftDocument（列表页用的 PageResp 会省略大字段）。
type PageDraftResp struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	DraftPath     string          `json:"draftPath"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     time.Time       `json:"updatedAt"`
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
