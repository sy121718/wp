package pageservice

import (
	"context"
	"errors"
	publish "go_wp/pkg/publish"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/artifact/contract"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/blueprint/contract"
	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/navigation/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/builder/source"
	"go_wp/pkg/database/retention"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ pagecontract.PageService = (*Service)(nil)

type Service struct {
	actionTargetReferences  source.ActionTargetReferenceReader
	actionTargetInvalidator source.ActionTargetInvalidator
	model                   *pagemodel.Model
	project                 projectcontract.ProjectService
	artifacts               artifactcontract.ArtifactService

	pageArtifacts artifactcontract.PageArtifactReader

	buildQueue pagecontract.BuildQueueEnqueuer
	routes     pubcontract.PublicationService
	blocks     blockcontract.BlockService

	blueprints blueprintcontract.BlueprintService
	plugins    plugincontract.PluginService
	content    core.CollectionResolver

	productDS productcontract.ProductDataSource

	navigation navigationcontract.NavigationService

	media mediacontract.MediaService

	structureTemplates pipeline.StructureTemplatePort
	actionInstances    presentationcontract.PublishedInstanceLocator

	contentStore i18n.ContentStore

	publisher *pipeline.Publisher
	store     *publish.LocalStore

	publication *publish.LocalPublicationStore

	externalArtifactOwners func(ctx context.Context) ([]string, error)

	i18nPeer I18nStalePeer

	publishWindowFault func() error

	postSwitchFault func() error

	convergeWake chan struct{}

	lastConvergeAt atomic.Int64

	pathLock publish.PublicationPathLocker

	checkoutCountries func(ctx context.Context, lang string) []core.CheckoutCountry
}

func NewService(model *pagemodel.Model, artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService, project projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	content core.CollectionResolver, navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) *Service {
	store := &publish.LocalStore{Root: publish.DefaultArtifactRoot()}
	publication := &publish.LocalPublicationStore{ActiveRoot: publish.ActiveRoot()}
	s := &Service{

		pathLock:    publish.DefaultPathLocker(),
		model:       model,
		artifacts:   artifacts,
		routes:      routes,
		project:     project,
		blocks:      blocks,
		plugins:     plugins,
		content:     content,
		navigation:  navigation,
		media:       media,
		store:       store,
		publication: publication,

		convergeWake: make(chan struct{}, 1),
	}

	if r, ok := artifacts.(artifactcontract.PageArtifactReader); ok {
		s.pageArtifacts = r
	}

	s.publisher = pipeline.NewPublisher(store, publication, pipeline.WithDependencies(s.buildDependencies))
	s.publisher.SetCompile(s.assembleCompile)
	return s
}

func (s *Service) SetPageArtifacts(reader artifactcontract.PageArtifactReader) {
	s.pageArtifacts = reader
}

func (s *Service) SetStructureTemplatePort(port pipeline.StructureTemplatePort) {
	s.structureTemplates = port
}

func (s *Service) SetPublicationPathLocker(locker publish.PublicationPathLocker) {
	s.pathLock = locker
}

func (s *Service) withPaths(paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}

	return publish.WithPathLock(s.pathLock, paths, fn)
}

func (s *Service) withPath(path string, fn func() error) error {
	return s.withPaths([]string{path}, fn)
}

func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

func (s *Service) newContentTranslator(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorScoped(ctx, projectID, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
}

func (s *Service) getExistingPage(ctx context.Context, id string) (page *pagemodel.PageEntity, err error) {
	page, err = s.locatePageInProjects(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

func (s *Service) SetCheckoutCountries(fn func(ctx context.Context, lang string) []core.CheckoutCountry) {
	s.checkoutCountries = fn
}

func (s *Service) SetBlueprints(bp blueprintcontract.BlueprintService) { s.blueprints = bp }

func (s *Service) SetPublishWindowFault(fn func() error) {
	if s == nil {
		return
	}
	s.publishWindowFault = fn
}

func (s *Service) SetPostSwitchFault(fn func() error) {
	if s == nil {
		return
	}
	s.postSwitchFault = fn
}

func (s *Service) fanoutProjectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}

	if s.project == nil {
		return nil, ErrProjectRequired
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {

		return nil, ErrProjectRequired
	}
	return ids, nil
}

func (s *Service) locatePageInProjects(ctx context.Context, id string) (*pagemodel.PageEntity, error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error = gorm.ErrRecordNotFound
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		page, gerr := s.model.GetByID(ctx, id, projectID)
		if gerr == nil {
			return page, nil
		}
		lastErr = gerr
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, lastErr
}

var (
	ErrInvalidParam    = pageenums.Err(pageenums.ErrInvalidParam)
	ErrPageNotFound    = pageenums.Err(pageenums.ErrPageNotFound)
	ErrProjectNotFound = pageenums.Err(pageenums.ErrProjectNotFound)

	ErrProjectRequired      = pageenums.Err(pageenums.ErrProjectRequired)
	ErrInvalidKind          = pageenums.Err(pageenums.ErrInvalidKind)
	ErrInvalidDocument      = pageenums.Err(pageenums.ErrInvalidDocument)
	ErrInvalidPath          = pageenums.Err(pageenums.ErrInvalidPath)
	ErrDraftVersionConflict = pageenums.Err(pageenums.ErrDraftVersionConflict)
	ErrPathOccupied         = pageenums.Err(pageenums.ErrPathOccupied)
	ErrNoStagedArtifact     = pageenums.Err(pageenums.ErrNoStagedArtifact)
	ErrRollbackTargetMiss   = pageenums.Err(pageenums.ErrRollbackTargetMiss)
	ErrRebuildRequired      = pageenums.Err(pageenums.ErrRebuildRequired)

	ErrInputMoved = pageenums.Err(pageenums.ErrInputMoved)

	ErrRebuildOverloaded = pageenums.Err(pageenums.ErrRebuildOverloaded)

	ErrRedirectUnavailable = pageenums.Err(pageenums.ErrRedirectUnavailable)
	ErrRedirectNotFound    = pageenums.Err(pageenums.ErrRedirectNotFound)
	ErrRedirectOccupied    = pageenums.Err(pageenums.ErrRedirectOccupied)
	ErrRedirectTargetMiss  = pageenums.Err(pageenums.ErrRedirectTargetMiss)
	ErrRedirectLoop        = pageenums.Err(pageenums.ErrRedirectLoop)
	ErrRedirectConflict    = pageenums.Err(pageenums.ErrRedirectConflict)
)

const (
	pageRevisionKeep = 20

	pageRevisionRetainDays = 90

	artifactRetentionDays = 30

	pageRetentionInterval = 24 * time.Hour

	pageRetentionBatch = 500
)

func (s *Service) pruneRevisions(ctx context.Context, projectID, pageID string) {
	if s == nil || s.model == nil {
		return
	}
	n, err := s.model.PruneRevisions(ctx, projectID, pageID, pageRevisionKeep)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "收敛页面历史快照失败")
		return
	}
	if n > 0 {
		logger.Scene("page").With("pageId", pageID).With("deleted", n).Info("已收敛页面历史快照")
	}
}

func (s *Service) PurgeRetention(ctx context.Context) (deletedRevisions int64, err error) {
	if s == nil || s.model == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	revisions := retention.Task{
		Name: "page_revisions", Table: "page_revisions", TimeColumn: "create_time",
		Retain: pageRevisionRetainDays * 24 * time.Hour, BatchSize: pageRetentionBatch,
		Note: "每页保留最近若干版本；只有既超出条数、又早于保留期的才删（回退需要近期版本）",
		Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {

			projects, perr := s.fanoutProjectIDs(ctx)
			if perr != nil {
				return 0, perr
			}
			var total int64
			for _, pid := range projects {
				if ctx.Err() != nil {
					break
				}
				n, derr := s.model.DeleteStaleRevisions(ctx, pid, pageRevisionKeep, cutoff, limit)
				if derr != nil {
					return total, derr
				}
				total += n
			}
			return total, nil
		},
	}
	outcomes := retention.RunAll(ctx, []retention.Task{revisions}, now)
	total, failed := retention.Summary(outcomes)
	if len(failed) > 0 {
		logger.Scene("page").With("failed", failed).Warn("保留期任务部分失败")
	}
	if total > 0 {
		logger.Scene("page").With("deleted", total).Info("已清理超期页面历史快照")
	}

	notDryRun := false
	res, err := s.GarbageCollectArtifacts(ctx, &pagedto.GCArtifactsReq{
		RetentionDays: artifactRetentionDays, DryRun: &notDryRun,
	})
	if err != nil {
		logger.Scene("page").Error(err, "定时回收产物失败")

		return total, err
	}
	if res != nil && (res.Deleted > 0 || res.Failed > 0) {
		logger.Scene("page").
			With("scanned", res.Scanned).With("deleted", res.Deleted).With("failed", res.Failed).
			Info("已回收超期产物")
	}
	return total, nil
}

func StartPageRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = svc.PurgeRetention(ctx)
		}
		run()
		ticker := time.NewTicker(pageRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
