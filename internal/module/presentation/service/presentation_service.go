package presentationservice

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

const systemCreator = "00000000-0000-0000-0000-000000000000"

const maxAutoRebuildInstances = 20

const instanceLockStripes = 64

type Service struct {
	actionTargetReferences  source.ActionTargetReferenceReader
	actionTargetInvalidator source.ActionTargetInvalidator
	m                       *presentationmodel.Model
	templates               contenttemplatecontract.ContentTemplateService
	registry                core.EntitySourceRegistry
	project                 projectcontract.ProjectService

	blocks blockcontract.BlockService

	collection core.CollectionResolver

	productDS productcontract.ProductDataSource

	checkoutCountries func(ctx context.Context, lang string) []core.CheckoutCountry

	navigation  navigationcontract.NavigationService
	actionPages pagecontract.PublishedPageLocator

	mediaProbe func(ctx context.Context, url string) []mediacontract.VariantRef

	plugins plugincontract.PluginService

	contentStore i18n.ContentStore
	store        *pipeline.LocalStore
	publication  *pipeline.LocalPublicationStore

	routes pubcontract.PublicationService

	instanceLocks [instanceLockStripes]sync.Mutex

	buildQueue presentationcontract.BuildQueueEnqueuer

	convergeWake chan struct{}

	lastConvergeAt atomic.Int64

	converging atomic.Int32

	lastOldPathPending atomic.Int64

	pathLock pipeline.PublicationPathLocker
}

func (s *Service) SetPublicationPathLocker(locker pipeline.PublicationPathLocker) {
	s.pathLock = locker
}

func (s *Service) withPaths(paths []string, fn func() error) error {
	if fn == nil {
		return nil
	}
	locker := s.pathLock
	if locker == nil {

		return pipeline.ErrPathLockerUnavailable
	}
	return locker.WithPaths(paths, fn)
}

func (s *Service) withPath(path string, fn func() error) error {
	return s.withPaths([]string{path}, fn)
}

func (s *Service) lockInstance(entityType, entityID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entityType + ":" + entityID))
	return &s.instanceLocks[h.Sum32()%instanceLockStripes]
}

func NewService(m *presentationmodel.Model,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	routes pubcontract.PublicationService) *Service {
	return &Service{
		m:           m,
		templates:   templates,
		registry:    registry,
		project:     project,
		blocks:      blocks,
		store:       &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()},
		publication: &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()},
		routes:      routes,
		pathLock:    pipeline.DefaultPathLocker(),

		convergeWake: make(chan struct{}, 1),
	}
}

func (s *Service) SetCollectionResolver(r core.CollectionResolver) { s.collection = r }

func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

func (s *Service) newContentTranslator(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorScoped(ctx, projectID, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
}

var _ presentationcontract.PresentationService = (*Service)(nil)

var _ presentationcontract.DetailTemplateModePort = (*Service)(nil)

var _ presentationcontract.PublishRecordPort = (*Service)(nil)

var _ pipeline.DependencyTarget = (*Service)(nil)
var _ pipeline.StaleRebuilder = (*Service)(nil)

func (s *Service) SourceType() string { return pipeline.SourceTypePresentation }

func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

func (s *Service) SetCheckoutCountries(fn func(ctx context.Context, lang string) []core.CheckoutCountry) {
	s.checkoutCountries = fn
}

func (s *Service) SetNavigationService(nav navigationcontract.NavigationService) { s.navigation = nav }

func (s *Service) SetMediaProbe(probe func(ctx context.Context, url string) []mediacontract.VariantRef) {
	s.mediaProbe = probe
}

func (s *Service) SetPluginService(plugins plugincontract.PluginService) { s.plugins = plugins }

func (s *Service) MarkStaleForI18n(ctx context.Context) (err error) {
	if s.project == nil {
		return nil
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return err
	}
	at := time.Now().UTC()
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		if _, merr := s.m.MarkStaleForI18n(ctx, p.ID, at); merr != nil {
			return merr
		}
	}
	return nil
}

func (s *Service) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) (err error) {
	if s == nil || s.m == nil {
		return errors.New("presentation: 实例仓储未装配，无法标记实例失效")
	}
	if _, merr := s.m.MarkStaleForI18nTx(ctx, tx, projectID, time.Now().UTC()); merr != nil {
		return merr
	}
	return nil
}

var _ presentationcontract.PublishedEntityLocator = (*Service)(nil)

func (s *Service) PublishedEntityPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (map[string]string, error) {
	ids := normalizeLocatorIDs(entityIDs)
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	if strings.TrimSpace(lang) == "" {
		lang = pipeline.DefaultLocale(ctx, s.project, projectID)
	}
	return s.m.ListActiveURLPaths(ctx, projectID, entityType, lang, ids)
}

func normalizeLocatorIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
