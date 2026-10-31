package pubdto

import "strings"

type ActivateReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId"`

	PresentationID string `json:"presentationId" form:"presentationId"`
	ArtifactID     string `json:"artifactId" form:"artifactId" binding:"required"`
	Action         string `json:"action" form:"action"`
}

type DeactivateReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId"`

	PresentationID string `json:"presentationId" form:"presentationId"`
}

type RedirectReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	OldPath        string `json:"oldPath" form:"oldPath" binding:"required"`
	PageID         string `json:"pageId" form:"pageId"`
	PresentationID string `json:"presentationId" form:"presentationId"`
	ArtifactID     string `json:"artifactId" form:"artifactId"`
}

type RenameReservedReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
	OldPath   string `json:"oldPath" form:"oldPath" binding:"required"`
	NewPath   string `json:"newPath" form:"newPath" binding:"required"`

	OnlyReserved bool `json:"onlyReserved" form:"onlyReserved"`
}

type ReserveReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

type DeleteRoutesReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

type ListActivePathsReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

type IsOccupiedReq struct {
	ProjectID     string `json:"projectId" form:"projectId" binding:"required"`
	Path          string `json:"path" form:"path" binding:"required"`
	ExcludePageID string `json:"excludePageId" form:"excludePageId"`

	ExcludePresentationID string `json:"excludePresentationId" form:"excludePresentationId"`
}

type ListActivePathsByPresentationReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" form:"presentationId" binding:"required"`
}

type DeleteRoutesByPresentationReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" form:"presentationId" binding:"required"`
}

type ReceiptAttemptVerdict int

const (
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = iota

	ReceiptAttemptInProgress

	ReceiptAttemptSucceeded

	ReceiptAttemptFailed
)

type BeginPublishReceiptReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`

	PageID         string `json:"pageId" form:"pageId"`
	PresentationID string `json:"presentationId" form:"presentationId"`

	Owner          *ReceiptOwner `json:"owner,omitempty" gorm:"-"`
	FromArtifactID string        `json:"fromArtifactId" form:"fromArtifactId"`

	ToArtifactID string `json:"toArtifactId" form:"toArtifactId"`

	Lang   string `json:"lang" form:"lang"`
	Action string `json:"action" form:"action"`

	OldPath string `json:"oldPath" form:"oldPath"`

	Redirect bool `json:"redirect" form:"redirect"`

	CapturedInputVersion int64 `json:"capturedInputVersion" form:"capturedInputVersion"`

	OldPaths []string `json:"oldPaths" form:"oldPaths"`

	SuccessfulReceipts map[string]string `json:"successfulReceipts" form:"-" gorm:"-"`

	FromReceiptID string `json:"fromReceiptId" form:"fromReceiptId"`

	PathArtifacts map[string]ReceiptPathIdentity `json:"pathArtifacts,omitempty" gorm:"-"`
}

type ReceiptOwner struct {
	SourceType string `json:"sourceType"`

	SourceID string `json:"sourceId"`
}

func (o ReceiptOwner) IsZero() bool {
	return o.SourceType == "" || o.SourceID == ""
}

func (o ReceiptOwner) IsProject() bool {
	return strings.TrimSpace(o.SourceType) == ReceiptSourceProject
}

type ReceiptPathIdentity struct {
	ArtifactID string `json:"artifactId,omitempty"`

	ReceiptID string `json:"receiptId,omitempty"`

	Locator string `json:"locator,omitempty"`
}

type ReceiptsQueryReq struct {
	SourceType string `json:"sourceType" form:"sourceType"`

	Actions []string `json:"actions" form:"actions"`

	Limit int `json:"limit" form:"limit"`
}

type SeoAuditReq struct {
	Project string `json:"project" form:"project"`
}

func (r *SeoAuditReq) ResolveProject(query *SeoAuditReq) string {
	if r != nil {
		if v := strings.TrimSpace(r.Project); v != "" {
			return v
		}
	}
	if query != nil {
		return strings.TrimSpace(query.Project)
	}
	return ""
}
