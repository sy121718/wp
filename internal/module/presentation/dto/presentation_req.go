package presentationdto

import (
	"strings"

	json "go_wp/pkg/jsonx"
)

type CreateInstanceReq struct {
	EntityType string `json:"entityType" form:"entityType" binding:"required"`
	EntityID   string `json:"entityId" form:"entityId" binding:"required"`
	URLPath    string `json:"urlPath" form:"urlPath" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	TemplateID string `json:"templateId" form:"templateId"`

	InstanceRole string `json:"instanceRole" form:"instanceRole"`
}

type EnsureArchiveReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	EntityType string `json:"entityType" form:"entityType" required:"true"`
	EntityID   string `json:"entityId" form:"entityId" required:"true"`

	Slug string `json:"slug" form:"slug"`
}

type RebuildReq struct {
	EntityID string `json:"entityId" form:"entityId" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	TemplateID string `json:"templateId" form:"templateId"`
}

type SaveOverrideReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`

	Document json.RawMessage `json:"document" form:"document" binding:"required"`

	ConfirmDetach bool `json:"confirmDetach" form:"confirmDetach"`
}

type WorkbenchSaveReq struct {
	ID            string          `json:"id" form:"id"`
	ProjectID     string          `json:"projectId" form:"projectId"`
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument"`
	ConfirmDetach bool            `json:"confirmDetach" form:"confirmDetach"`
}

func (r *WorkbenchSaveReq) Override() *SaveOverrideReq {
	return &SaveOverrideReq{
		InstanceID:    strings.TrimSpace(r.ID),
		ProjectID:     r.ProjectID,
		Document:      r.DraftDocument,
		ConfirmDetach: r.ConfirmDetach,
	}
}

type ClearOverrideReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`
	TemplateID string `json:"templateId" form:"templateId"`
}

type ReapplyPresetReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`

	TemplateID string `json:"templateId" form:"templateId"`
}

type RollbackArtifactReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`

	TargetHash string `json:"targetHash" form:"targetHash" binding:"required"`
}

type RollbackDocumentReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`
	SnapshotID string `json:"snapshotId" form:"snapshotId" binding:"required"`
}

type ListSnapshotsReq struct {
	InstanceID string `form:"instanceId" json:"instanceId" binding:"required"`
	ProjectID  string `form:"projectId" json:"projectId"`
	Limit      int    `form:"limit" json:"limit"`
}

type CountByTemplateReq struct {
	TemplateID string `json:"templateId" form:"templateId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`
}

type UpdateURLReq struct {
	ID         string `json:"id" form:"id"`
	EntityType string `json:"entityType" form:"entityType"`
	EntityID   string `json:"entityId" form:"entityId"`

	ProjectID string `json:"projectId" form:"projectId"`

	NewPath string `json:"newPath" form:"newPath"`

	WithRedirect bool `json:"withRedirect" form:"withRedirect"`
}

type GetByEntityReq struct {
	EntityType string `form:"entityType" binding:"required"`
	EntityID   string `form:"entityId" binding:"required"`

	ProjectID string `form:"projectId" json:"projectId"`
}

type PreviewInstanceReq struct {
	InstanceID string `json:"instanceId" form:"instanceId"`
	EntityType string `json:"entityType" form:"entityType" binding:"required_without=InstanceID"`
	EntityID   string `json:"entityId" form:"entityId" binding:"required_without=InstanceID"`

	TemplateID string `json:"templateId" form:"templateId"`

	ProjectID string `json:"projectId" form:"projectId"`

	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument"`
}

type GetReq struct {
	ID string `form:"id" binding:"required"`

	ProjectID string `form:"projectId" json:"projectId"`
}

type DeleteReq struct {
	ID string `form:"id" binding:"required"`

	ProjectID string `form:"projectId" json:"projectId"`
}

type ListReq struct {
	Lang string `form:"lang" json:"lang"`

	ActionTargetsOnly bool   `form:"actionTargetsOnly" json:"actionTargetsOnly"`
	EntityType        string `form:"entityType"`

	ProjectID string `form:"projectId" json:"projectId"`
}
