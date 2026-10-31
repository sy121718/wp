package purchasedto

import (
	json "go_wp/pkg/jsonx"
)

type CreateSourceReq struct {
	ProjectID    string          `json:"projectId" form:"projectId"`
	Code         string          `json:"code" form:"code" binding:"required"`
	Name         string          `json:"name" form:"name" binding:"required"`
	Type         string          `json:"type" form:"type"`
	RelatedParty *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice" form:"settlePrice"`
	Status       string          `json:"status" form:"status"`
	Config       json.RawMessage `json:"config" form:"config"`
	Sort         int             `json:"sort" form:"sort"`
	Metadata     json.RawMessage `json:"metadata" form:"metadata"`

	OperatorID string `json:"-"`
}

type UpdateSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID        string          `json:"projectId" form:"projectId"`
	Code             *string         `json:"code" form:"code"`
	Name             *string         `json:"name" form:"name"`
	Type             *string         `json:"type" form:"type"`
	RelatedParty     *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice      *float64        `json:"settlePrice" form:"settlePrice"`
	ClearSettlePrice bool            `json:"clearSettlePrice" form:"clearSettlePrice"`
	Status           *string         `json:"status" form:"status"`
	Config           json.RawMessage `json:"config" form:"config"`
	Sort             *int            `json:"sort" form:"sort"`
	Metadata         json.RawMessage `json:"metadata" form:"metadata"`

	OperatorID string `json:"-"`
}

type GetSourceReq struct {
	ID string `form:"id" binding:"required"`

	ProjectID string `form:"projectId"`
}

type DeleteSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	OperatorID string `json:"-"`
}

type ListSourceReq struct {
	ProjectID       string `form:"projectId"`
	Type            string `form:"type"`
	RelatedParty    string `form:"relatedParty"`
	Status          string `form:"status"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
	Page            int    `form:"page"`
	Size            int    `form:"size"`
}

type SourceSummaryReq struct {
	ProjectID string `form:"projectId"`
}

type SourceListQueryReq struct {
	ProjectID    string `form:"project"`
	Type         string `form:"type"`
	RelatedParty string `form:"relatedParty"`
	Status       string `form:"status"`
	Keyword      string `form:"keyword"`
	Page         int    `form:"page"`
	Limit        int    `form:"limit"`
}
