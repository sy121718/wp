package pagedto

import (
	"encoding/json"
	"go_wp/pkg/utils"
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
	CreatedAt         utils.JSONTime  `json:"createdAt"`
	UpdatedAt         utils.JSONTime  `json:"updatedAt"`
	// Publications 每语言激活状态（多语言 P3，docs/06-D §15.5 第 2 条）。
	// ActivePath/ActiveArtifactID 是「最近发布语言」的单值镜像，多语言真源在此列表。
	Publications []PagePublicationResp `json:"publications,omitempty"`
}

// PagePublicationResp 页面某语言的激活状态投影。
type PagePublicationResp struct {
	Lang         string          `json:"lang"`
	ActivePath   string          `json:"activePath"`
	ArtifactID   *string         `json:"artifactId,omitempty"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	PublishedAt  *utils.JSONTime `json:"publishedAt,omitempty"`
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
	UpdatedAt     utils.JSONTime  `json:"updatedAt"`
}

// PageTitleResp 页面标题投影（id / 路径 / SEO 标题），**不含 draft_document 大字段**。
//
// 为什么要有它：列表投影 PageResp 走 model.ListAll，那条路径刻意 Omit("draft_document")
// （大字段不进列表，见 AGENTS.md 未列但 model 注释写明的原因），于是调用方拿到的是空文档、
// 读不出标题。而「页面叫什么」与「页面在哪」是两件事，只要标题和路径的消费方
// （如导航来源候选）不该为了一行标题把整份 JSONB 拉到 Go 侧再解析。
//
// SEOTitle 在 **SQL 侧**取自 draft_document->'settings'->'seo'->>'title'（见
// model.ListPageTitles），语义与 resolver 解析单个页面来源时的标题口径一致：
// 空串表示作者没在文档 SEO 段填标题，**读侧不替它编一个名字**，由调用方决定回退（通常是路径）。
type PageTitleResp struct {
	ID string `json:"id"`
	// Kind 页面类型（home / archive / search / notFound / page / article / tag）。
	// 列表要带它才能回答「站点有哪些功能页」—— 只给标题与路径分不出来，
	// 而建功能页之前正需要先确认有没有同用途的那一张。
	Kind      string `json:"kind"`
	DraftPath string `json:"draftPath"`
	// ActivePath 最近发布语言的线上路径；未发布为 nil（调用方回退草稿路径）。
	ActivePath *string `json:"activePath,omitempty"`
	SEOTitle   string  `json:"seoTitle"`
}

// RevisionResp Page 草稿修订快照，用于历史记录/Undo/Redo/版本对比。
type RevisionResp struct {
	ID            string          `json:"id"`
	PageID        string          `json:"pageId"`
	Version       int64           `json:"version"`
	DraftPath     string          `json:"draftPath"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	SourceHash    string          `json:"sourceHash"`
	CreatedAt     utils.JSONTime  `json:"createdAt"`
}
