package productcontract

import (
	"context"
	"time"
)

// DetailTemplateCandidateView 模板目录中可复用的身份与版本事实。
type DetailTemplateCandidateView struct {
	ID           string
	Name         string
	DraftVersion int64
	UpdatedAt    time.Time
}

// BoundTemplateView 商品当前绑定的发布实例事实。
type BoundTemplateView struct {
	// Exists 该实体是否已经有发布实例；为假时其余字段均为零值。
	Exists bool
	// TemplateID 实例绑定的模板 id。
	TemplateID string
	// Status 实例状态（未发布 / 已发布 / …，按 presentation 模块的取值）。
	Status string
	// URLPath 实例已激活的路径（未发布时为空）。
	URLPath                 string
	InstanceID              string
	RenderMode              string
	SourceTemplateVersionID string
}

// DetailTemplatePanelFacts 发布实例、模板版本与历史快照的关联事实。
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

// DetailTemplatePort 按工程读取模板目录、默认模板和实体发布实例。
// 未配置模板或未创建实例返回空事实；读取故障返回错误。
type DetailTemplatePort interface {
	DetailTemplateCandidates(ctx context.Context, projectID, entityType string) ([]DetailTemplateCandidateView, error)
	DefaultDetailTemplate(ctx context.Context, projectID, entityType string) (string, error)
	BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) (BoundTemplateView, error)
	DetailTemplatePanel(ctx context.Context, projectID, entityType, entityID string) (DetailTemplatePanelFacts, error)
}
