package pagecontract

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	blockcontract "go_wp/internal/module/block/contract"
	pagedto "go_wp/internal/module/page/dto"
)

type (
	CreateReq     = pagedto.CreateReq
	SaveDraftReq  = pagedto.SaveDraftReq
	ListReq       = pagedto.ListReq
	DetailReq     = pagedto.DetailReq
	RevisionReq   = pagedto.RevisionReq
	RevisionResp  = pagedto.RevisionResp
	DeleteReq     = pagedto.DeleteReq
	PageResp      = pagedto.PageResp
	PageDraftResp = pagedto.PageDraftResp

	PageLangStateResp = pagedto.PageLangStateResp

	TranslationMissResp = pagedto.TranslationMissResp

	PageTitleResp = pagedto.PageTitleResp
	BuildReq      = pagedto.BuildReq
	PublishReq    = pagedto.PublishReq

	PageBuildJobReq = pagedto.PageBuildJobReq
	RollbackReq     = pagedto.RollbackReq
	UpdateURLReq    = pagedto.UpdateURLReq
	PublishResp     = pagedto.PublishResp

	StalePageListReq       = pagedto.StalePageListReq
	StalePageListResp      = pagedto.StalePageListResp
	StalePageResp          = pagedto.StalePageResp
	StaleImpactSummaryResp = pagedto.StaleImpactSummaryResp

	SEOPatrolReq          = pagedto.SEOPatrolReq
	SEOPatrolResp         = pagedto.SEOPatrolResp
	SEOArtifactItemResp   = pagedto.SEOArtifactItemResp
	SEOFindingItemResp    = pagedto.SEOFindingItemResp
	SEOUncheckedRouteResp = pagedto.SEOUncheckedRouteResp

	RedirectListReq           = pagedto.RedirectListReq
	RedirectListResp          = pagedto.RedirectListResp
	RedirectItemResp          = pagedto.RedirectItemResp
	RedirectCreateReq         = pagedto.RedirectCreateReq
	RedirectDeleteReq         = pagedto.RedirectDeleteReq
	RedirectMergeReq          = pagedto.RedirectMergeReq
	RedirectProjectOptionResp = pagedto.RedirectProjectOptionResp
)

type PreviewProblem struct {
	Msg string

	cause error
}

func NewPreviewProblem(msg string, cause error) *PreviewProblem {
	return &PreviewProblem{Msg: msg, cause: cause}
}

func (p *PreviewProblem) Error() string {
	if p == nil {
		return ""
	}
	return p.Msg
}

func (p *PreviewProblem) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.cause
}

var (
	ErrPreviewInvalidDocument = errors.New("预览文档解析失败")

	ErrPreviewCompileFailed = errors.New("预览编译失败")
)

type DependencyPageRef struct {
	ID string

	Title string
}

type PageDependencyLookup interface {
	FindPagesByDependency(ctx context.Context, projectID, dependencyKind, dependencyKey string) (
		refs []DependencyPageRef, err error)
}

type PagePathKindLookup interface {
	KindsOfPaths(ctx context.Context, projectID string, paths []string) (map[string]string, error)
}

type PageMenuStalePort interface {
	MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}

type BuildQueueEnqueuer interface {
	EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error
}

const (
	BuildIntentManual = "manual"

	BuildIntentDependency = "dependency"
)

const RebuildBatchCapacity = 20

type PageService interface {
	PageDependencyLookup

	PagePathKindLookup

	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)

	List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error)

	UntranslatedPageLangs(ctx context.Context, projectID string) (rows []TranslationMissResp, err error)

	PageLangStates(ctx context.Context, pageID string) (rows []PageLangStateResp, err error)

	ExcludePageLang(ctx context.Context, pageID, lang string) (retired int, err error)

	RestorePageLang(ctx context.Context, pageID, lang string) error

	ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error)
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)

	ProjectOfPage(ctx context.Context, pageID string) (projectID string, err error)

	ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error)
	SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error)
	ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error)

	CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string, canvasFrames bool) (html []byte, err error)

	Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error)

	RunPageBuildJob(ctx context.Context, req *pagedto.PageBuildJobReq) (err error)

	Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error)

	PublishAllLanguages(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishAllResp, err error)

	Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error)

	UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error)

	RebuildArtifact(ctx context.Context, req *pagedto.RebuildArtifactReq) (res *pagedto.RebuildArtifactResp, err error)

	GarbageCollectArtifacts(ctx context.Context, req *pagedto.GCArtifactsReq) (res *pagedto.GCArtifactsResp, err error)

	PurgeRetention(ctx context.Context) (deletedRevisions int64, err error)

	SEOPatrol(ctx context.Context, req *pagedto.SEOPatrolReq) (res *pagedto.SEOPatrolResp, err error)

	AuditPublication(ctx context.Context) (res *pagedto.PublicationAuditResp, err error)

	Delete(ctx context.Context, req *pagedto.DeleteReq) (err error)

	RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error

	RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error

	MarkStaleForTheme(ctx context.Context, themeID string) (ids []string, err error)

	MarkStaleForBlock(ctx context.Context, blockID string) (ids []string, err error)

	MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error)

	MarkStaleForI18n(ctx context.Context) error

	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)

	RebuildStale(ctx context.Context, ids []string) error

	RebuildStaleStrict(ctx context.Context, ids []string) error

	CountBlockReference(ctx context.Context, blockID string) (int64, error)

	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)

	ListStalePages(ctx context.Context, req *pagedto.StalePageListReq) (res *pagedto.StalePageListResp, err error)

	AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error

	ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) error

	ReskinProjectForTheme(ctx context.Context, projectID, themeID string, theme, structure json.RawMessage) error

	ListRedirects(ctx context.Context, req *pagedto.RedirectListReq) (res *pagedto.RedirectListResp, err error)

	CreateRedirect(ctx context.Context, req *pagedto.RedirectCreateReq) (res *pagedto.RedirectItemResp, err error)

	DeleteRedirect(ctx context.Context, req *pagedto.RedirectDeleteReq) (err error)

	MergeRedirectChain(ctx context.Context, req *pagedto.RedirectMergeReq) (res *pagedto.RedirectItemResp, err error)
}

type PageQueryReader interface {
	ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error)
}
