package pagedto

import "go_wp/pkg/utils"

type StalePageResp struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName,omitempty"`
	Title       string `json:"title,omitempty"`

	Path string `json:"path"`

	Published bool `json:"published"`

	Stale bool `json:"stale"`

	UpdatedAt utils.JSONTime `json:"updatedAt"`

	RebuildFailedAt    *utils.JSONTime `json:"rebuildFailedAt,omitempty"`
	RebuildFailedStage string          `json:"rebuildFailedStage,omitempty"`
}

type StalePageListResp struct {
	Pages     []StalePageResp `json:"pages"`
	Total     int             `json:"total"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
}

type StaleImpactSummaryResp struct {
	Total     int             `json:"total"`
	Pages     []StalePageResp `json:"pages"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
}
