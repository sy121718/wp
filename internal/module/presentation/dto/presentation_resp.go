package presentationdto

import json "go_wp/pkg/jsonx"

type EnsureArchiveResp struct {
	Skipped    string `json:"skipped,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Created    bool   `json:"created"`
}

const (
	RenderModeTemplate = "template"

	RenderModeDocument = "document"
)

type CountByTemplateResp struct {
	TemplateMode int64 `json:"templateMode"`
	DocumentMode int64 `json:"documentMode"`
	Total        int64 `json:"total"`
}

type SnapshotSummary struct {
	ID                      string `json:"id"`
	SourceTemplateVersionID string `json:"sourceTemplateVersionId"`
	CreatedAt               string `json:"createdAt"`
}

type PreviewInstanceResp struct {
	HTML              string `json:"html"`
	EntityType        string `json:"entityType"`
	EntityID          string `json:"entityId"`
	TemplateID        string `json:"templateId"`
	TemplateName      string `json:"templateName"`
	TemplateVersionID string `json:"templateVersionId"`
	TemplateVersion   int64  `json:"templateVersion"`
}

type InstancePublicationResp struct {
	Lang         string  `json:"lang"`
	ActivePath   string  `json:"activePath"`
	ArtifactID   *string `json:"artifactId,omitempty"`
	ArtifactHash string  `json:"artifactHash"`
}

type InstanceResp struct {
	Publications []InstancePublicationResp `json:"publications,omitempty"`
	ID           string                    `json:"id"`
	ProjectID    string                    `json:"projectId"`
	EntityType   string                    `json:"entityType"`
	EntityID     string                    `json:"entityId"`

	InstanceRole string `json:"instanceRole,omitempty"`
	URLPath      string `json:"urlPath"`
	TemplateID   string `json:"templateId"`

	RenderMode string `json:"renderMode,omitempty"`

	SourceTemplateVersionID string          `json:"sourceTemplateVersionId,omitempty"`
	Status                  string          `json:"status"`
	Stale                   bool            `json:"stale"`
	ArtifactID              string          `json:"artifactId,omitempty"`
	ArtifactHash            string          `json:"artifactHash,omitempty"`
	SnapshotID              string          `json:"snapshotId,omitempty"`
	Document                json.RawMessage `json:"document,omitempty"`
	UpdatedAt               string          `json:"updatedAt"`
}
