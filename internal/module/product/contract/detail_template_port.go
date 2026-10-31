package productcontract

import (
	"context"
	"time"
)

type DetailTemplateCandidateView struct {
	ID           string
	Name         string
	DraftVersion int64
	UpdatedAt    time.Time
}

type BoundTemplateView struct {
	Exists bool

	TemplateID string

	Status string

	URLPath                 string
	InstanceID              string
	RenderMode              string
	SourceTemplateVersionID string
}

type DetailTemplatePanelFacts struct {
	Available                bool
	Bound                    BoundTemplateView
	CurrentTemplateVersionID string
	AffectedTemplateCount    int64
	HasAffectedTemplateCount bool
	Snapshots                []DetailTemplateSnapshotFacts
}

type DetailTemplateSnapshotFacts struct {
	ID        string
	CreatedAt string
}

type DetailTemplatePort interface {
	DetailTemplateCandidates(ctx context.Context, projectID, entityType string) ([]DetailTemplateCandidateView, error)
	DefaultDetailTemplate(ctx context.Context, projectID, entityType string) (string, error)
	BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) (BoundTemplateView, error)
	DetailTemplatePanel(ctx context.Context, projectID, entityType, entityID string) (DetailTemplatePanelFacts, error)
}
