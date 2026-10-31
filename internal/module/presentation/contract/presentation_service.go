package presentationcontract

import (
	"context"
	"time"

	"gorm.io/gorm"

	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/internal/module/presentation/dto"
)

type PresentationService interface {
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)

	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)

	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)

	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)

	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)

	DetailTemplateModePort

	PublishRecordPort

	PublishAttemptPort

	ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error)

	MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error)

	MarkStaleForI18n(ctx context.Context) error

	Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error)

	List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error)

	Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error)

	EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error)

	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)

	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)

	ListArtifactHashes(ctx context.Context) (hashes []string, err error)
}

type ArchiveInstanceEnsurer interface {
	EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error)
}

type (
	CreateInstanceReq = presentationdto.CreateInstanceReq
	InstanceResp      = presentationdto.InstanceResp
	RebuildReq        = presentationdto.RebuildReq
	GetByEntityReq    = presentationdto.GetByEntityReq

	PreviewInstanceReq  = presentationdto.PreviewInstanceReq
	PreviewInstanceResp = presentationdto.PreviewInstanceResp

	UpdateURLReq     = presentationdto.UpdateURLReq
	EnsureArchiveReq = presentationdto.EnsureArchiveReq

	ReapplyPresetReq    = presentationdto.ReapplyPresetReq
	RollbackDocumentReq = presentationdto.RollbackDocumentReq
	RollbackArtifactReq = presentationdto.RollbackArtifactReq
	ListSnapshotsReq    = presentationdto.ListSnapshotsReq
	CountByTemplateReq  = presentationdto.CountByTemplateReq
)

const (
	RenderModeTemplate = presentationdto.RenderModeTemplate
	RenderModeDocument = presentationdto.RenderModeDocument
)

type PresentationStalePort interface {
	MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)

	MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (n int64, err error)
}

type StaleRebuildPort interface {
	RebuildStaleSync(ctx context.Context, projectID string) (rebuilt int, err error)
}

type BuildQueueEnqueuer interface {
	EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error
}
