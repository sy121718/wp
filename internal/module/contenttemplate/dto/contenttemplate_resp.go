package contenttemplatedto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

const (
	ReferenceKindPage = "page"

	ReferenceKindInstance = "instance"
)

type TemplateReference struct {
	TemplateID string `json:"templateId"`

	Kind string `json:"kind"`

	Slots []string `json:"slots,omitempty"`

	PageID    string `json:"pageId,omitempty"`
	PagePath  string `json:"pagePath,omitempty"`
	PageTitle string `json:"pageTitle,omitempty"`

	InstanceID string `json:"instanceId,omitempty"`
	EntityType string `json:"entityType,omitempty"`
	EntityID   string `json:"entityId,omitempty"`
	URLPath    string `json:"urlPath,omitempty"`
	RenderMode string `json:"renderMode,omitempty"`
}

type ImpactResp struct {
	Available bool `json:"available"`

	References []TemplateReference `json:"references"`

	Unparsable int `json:"unparsable"`
}

type TemplateResp struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`

	TemplateRole string `json:"templateRole,omitempty"`

	IsDefault     bool            `json:"isDefault"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     utils.JSONTime  `json:"updatedAt"`
}
