package pubdto

import (
	"go_wp/pkg/utils"
)

type RouteResp struct {
	ProjectID string  `json:"projectId"`
	Path      string  `json:"path"`
	PageID    *string `json:"pageId,omitempty"`

	PresentationID *string        `json:"presentationId,omitempty"`
	RouteKind      string         `json:"routeKind"`
	ArtifactID     *string        `json:"artifactId,omitempty"`
	UpdatedAt      utils.JSONTime `json:"updatedAt"`
}

type RouteOwnerResp struct {
	ProjectID      string
	Path           string
	RouteKind      string
	PageID         *string
	PresentationID *string
	ArtifactID     *string
}

const (
	ReceiptActionSwitchActive = "switch_active"

	ReceiptActionUpdateURL = "update_url"

	ReceiptActionRollback = "rollback"

	ReceiptActionDeactivate = "deactivate"
)

const (
	ReceiptSourcePage = "page"

	ReceiptSourcePresentation = "presentation"

	ReceiptSourceProject = "project"
)

type PendingReceiptResp struct {
	ID         string `json:"id"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	ProjectID  string `json:"projectId"`
	Path       string `json:"path"`
	Lang       string `json:"lang"`

	FromArtifactID string `json:"fromArtifactId"`
	ToArtifactID   string `json:"toArtifactId"`
	Action         string `json:"action"`

	OldPath  string `json:"oldPath"`
	Redirect bool   `json:"redirect"`

	CapturedInputVersion int64 `json:"capturedInputVersion"`

	OldPaths []string `json:"oldPaths,omitempty"`

	SuccessfulReceipts map[string]string `json:"successfulReceipts,omitempty"`

	FromReceiptID string `json:"fromReceiptId"`

	Owner ReceiptOwner `json:"owner"`

	PathArtifacts map[string]ReceiptPathIdentity `json:"pathArtifacts,omitempty"`

	BusinessCommitted bool   `json:"businessCommitted"`
	CreatedAt         string `json:"createdAt"`
}
