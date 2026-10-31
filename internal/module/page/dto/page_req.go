package pagedto

import json "go_wp/pkg/jsonx"

type CreateReq struct {
	ProjectID         string  `json:"projectId" form:"projectId" binding:"required"`
	Kind              string  `json:"kind" form:"kind" binding:"required"`
	ContentTargetType string  `json:"contentTargetType" form:"contentTargetType"`
	ContentTargetID   *string `json:"contentTargetId" form:"contentTargetId"`
	DraftPath         string  `json:"draftPath" form:"draftPath" binding:"required"`

	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument"`

	BlueprintID string `json:"blueprintId" form:"blueprintId"`
}

type SaveDraftReq struct {
	ID              string          `json:"id" form:"id" binding:"required"`
	ExpectedVersion int64           `json:"expectedVersion" form:"expectedVersion"`
	DraftPath       string          `json:"draftPath" form:"draftPath" binding:"required"`
	DraftDocument   json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`
}

type ListReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ThemeID   string `form:"themeId" json:"themeId"`

	Lang string `form:"lang" json:"lang"`

	LangState string `form:"state" json:"state"`

	ActionTargetsOnly bool `form:"actionTargetsOnly" json:"actionTargetsOnly"`
}

type PageListQueryReq struct {
	Project string `form:"project"`
	Lang    string `form:"lang"`
	State   string `form:"state"`
}

type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

type RevisionReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
