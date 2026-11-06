package contenttemplatecontract

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

const (
	TemplateRoleDetail = "detail"

	TemplateRoleArchive = "archive"
)

const (
	EntityTypeHeader = "header"

	EntityTypeFooter = "footer"
)

func IsStructureTemplateType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case EntityTypeHeader, EntityTypeFooter:
		return true
	default:
		return false
	}
}

type ContentTemplateService interface {
	Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error)

	Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error)

	Delete(ctx context.Context, req *contenttemplatedto.DeleteReq) (err error)

	// BulkDelete 在同一事务里删除整批模板：任一失败整批拒绝，不逐条回执。
	BulkDelete(ctx context.Context, req *contenttemplatedto.BulkDeleteReq) error

	Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error)

	GetScoped(ctx context.Context, projectID, id string) (res *contenttemplatedto.TemplateResp, err error)

	List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error)

	Activate(ctx context.Context, req *contenttemplatedto.ActivateReq) (res *contenttemplatedto.TemplateResp, err error)

	ResolveTemplate(ctx context.Context, entityType string) (res *ResolvedTemplate, err error)

	ResolveTemplateByID(ctx context.Context, templateID string) (res *ResolvedTemplate, err error)

	ResolveTemplateByRole(ctx context.Context, entityType, role string) (res *ResolvedTemplate, err error)

	ResolveTemplateScoped(ctx context.Context, projectID, entityType string) (res *ResolvedTemplate, err error)

	ResolveTemplateByRoleScoped(ctx context.Context, projectID, entityType, role string) (res *ResolvedTemplate, err error)

	ResolveTemplateByIDScoped(ctx context.Context, projectID, templateID string) (res *ResolvedTemplate, err error)

	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)

	Impact(ctx context.Context, req *contenttemplatedto.ImpactReq) (res *contenttemplatedto.ImpactResp, err error)
}

type (
	CreateReq = contenttemplatedto.CreateReq
	GetReq    = contenttemplatedto.GetReq
	ListReq   = contenttemplatedto.ListReq

	TemplateResp = contenttemplatedto.TemplateResp
)

type TemplateImpactPort interface {
	ListTemplateReferences(ctx context.Context, projectID string, templateIDs []string) (
		refs []contenttemplatedto.TemplateReference, unparsable int, err error)
}

type ResolvedTemplate struct {
	TemplateID string

	VersionID string

	Version int64

	TemplateName string

	EntityType string

	TemplateRole string

	Document json.RawMessage
}
