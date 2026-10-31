package mediacontract

import (
	"context"
	"mime/multipart"

	mediadto "go_wp/internal/module/media/dto"
)

type VariantRef = mediadto.VariantRef

type SyncRefsInput struct {
	RefKind string

	RefID string

	RefTitle string

	URLs []string
}

type MediaService interface {
	Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediadto.AttachmentResp, error)

	List(ctx context.Context, req *mediadto.ListReq) (*mediadto.ListResp, error)

	Detail(ctx context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error)

	Delete(ctx context.Context, req *mediadto.DeleteReq) error

	Replace(ctx context.Context, id uint64, file *multipart.FileHeader) (*mediadto.AttachmentResp, error)

	References(ctx context.Context, id uint64) ([]mediadto.AttachmentRefResp, error)

	SyncReferences(ctx context.Context, req *SyncRefsInput) (int, error)

	SyncReferencesFromHTML(ctx context.Context, refKind, refID, refTitle, html string) (int, error)

	CreateCategory(ctx context.Context, req *mediadto.CategoryCreateReq) (*mediadto.CategoryTreeNode, error)

	UpdateCategory(ctx context.Context, req *mediadto.CategoryUpdateReq) error

	DeleteCategory(ctx context.Context, req *mediadto.CategoryDeleteReq) error

	UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error

	CategoryTree(ctx context.Context) ([]mediadto.CategoryTreeNode, error)

	GenerateVariants(ctx context.Context, attachmentID uint64) ([]mediadto.VariantResp, error)

	ProbeImageVariants(ctx context.Context, url string) []mediadto.VariantRef

	BuildDownloadPlan(ctx context.Context, attachmentID uint64, langs ...string) (*mediadto.DownloadPlan, error)

	BuildBatchDownloadPlan(ctx context.Context, ids []uint64, langs ...string) (*mediadto.DownloadPlan, error)
}

const (
	RefKindPage = "page"

	RefKindPresentation = "presentation"
)

type MediaRef struct {
	Kind string

	ID string
}

type StaleMarker interface {
	RefKinds() []string

	MarkStaleByMediaRefs(ctx context.Context, refs []MediaRef) (marked []string, err error)
}
