package routers

import (
	"context"
	"errors"
	"fmt"
	publish "go_wp/pkg/publish"
	"strings"
	"time"

	"gorm.io/gorm"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildmodel "go_wp/internal/module/build/model"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

var (
	errMenuStaleJobNil = errors.New("menu_stale 任务为空")

	errMenuStaleSourceID = errors.New("menu_stale 任务的 source_id 形状非法")

	errMenuStaleProjectMismatch = errors.New("menu_stale 任务的工程与 source_id 不一致")

	errMenuStaleKindMissing = errors.New("menu_stale 任务缺少菜单位置")
)

type pageMenuStalePort struct {
	svc pagecontract.PageService
}

func (p pageMenuStalePort) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string, at time.Time) (int64, error) {
	marker, ok := p.svc.(pagecontract.PageMenuStalePort)
	if !ok {

		return 0, errors.New("页面模块未实现同事务菜单标记（PageMenuStalePort）")
	}
	return marker.MarkMenuStaleTx(ctx, tx, projectID, kind, at)
}

type presentationMenuStalePort struct {
	svc presentationcontract.PresentationService
}

func (p presentationMenuStalePort) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string, at time.Time) (int64, error) {
	marker, ok := p.svc.(presentationcontract.PresentationStalePort)
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务菜单标记（PresentationStalePort）")
	}
	return marker.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at)
}

type presentationNavigationStalePort struct {
	svc presentationcontract.PresentationService
}

func (p presentationNavigationStalePort) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, navigationID string, at time.Time) (int64, error) {
	marker, ok := p.svc.(presentationcontract.PresentationStalePort)
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务按项菜单标记（PresentationStalePort）")
	}
	return marker.MarkInstanceNavigationStaleTx(ctx, tx, projectID, navigationID, at)
}

type buildMenuStalePort struct {
	svc buildcontract.BuildService
}

func (b buildMenuStalePort) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string) (int64, error) {
	return b.svc.EnqueueMenuStaleTx(ctx, tx, projectID, kind)
}

func navigationProjectLister(projects projectcontract.ProjectService) navigationservice.ProjectListerFunc {
	return func(ctx context.Context) ([]navigationservice.ProjectEntry, error) {
		rows, err := projects.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]navigationservice.ProjectEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, navigationservice.ProjectEntry{ID: row.ID})
		}
		return out, nil
	}
}

func navigationWritePorts(pageSvc pagecontract.PageService,
	presentationSvc presentationcontract.PresentationService,
	buildSvc buildcontract.BuildService) navigationservice.NavigationWriteDeps {
	return navigationservice.NavigationWriteDeps{
		Pages:     pageMenuStalePort{svc: pageSvc},
		Instances: presentationMenuStalePort{svc: presentationSvc},
		Items:     pageNavigationStalePort{svc: pageSvc},

		ItemInstances: presentationNavigationStalePort{svc: presentationSvc},
		Queue:         buildMenuStalePort{svc: buildSvc},
	}
}

type navigationMenuItemStale interface {
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error)
}

type pageNavigationStalePort struct {
	svc pagecontract.PageService
}

func (p pageNavigationStalePort) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, navigationID string) (int64, error) {
	marker, ok := p.svc.(navigationMenuItemStale)
	if !ok {
		return 0, errors.New("页面模块未实现同事务按项菜单标记（navigation 契约 MenuItemStalePort）")
	}
	return marker.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

func newNavigationTranslationWriter(db *gorm.DB, navigationSvc navigationcontract.NavigationService,
	projects projectcontract.ProjectService, pageSvc pagecontract.PageService,
	presentationSvc presentationcontract.PresentationService, buildSvc buildcontract.BuildService,
	content navigationContentWriter) *navigationservice.TranslationWriter {
	model := navigationmodel.NewModel(db)

	sourceOpener, ok := navigationSvc.(navigationSourceIndexTx)
	if !ok {
		panic("导航模块未实现事务内来源索引（navigationSourceIndexTx）—— 译文写服务无法解析可写目标")
	}
	return navigationservice.NewTranslationWriter(navigationservice.TranslationWriterDeps{
		Rows:      navigationservice.NewNavigationRowsTx(model),
		Sources:   sourceOpener,
		Projects:  navigationProjectLister(projects),
		Content:   content,
		Pages:     pageMenuStalePort{svc: pageSvc},
		Instances: presentationMenuStalePort{svc: presentationSvc},
		Queue:     buildMenuStalePort{svc: buildSvc},
		Tx:        model,
	})
}

type navigationSourceIndexTx interface {
	OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error)
}

type navigationContentWriter interface {
	LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string,
		hashes []string) (map[string]i18n.ContentTargetInfo, error)
	UpsertTx(ctx context.Context, tx *gorm.DB, items []i18n.ContentWriteItem) (int, error)
}

type menuStaleJob struct {
	ProjectID string
	Kind      string
}

func decodeMenuStaleJob(job *builddto.Job) (menuStaleJob, error) {
	if job == nil {
		return menuStaleJob{}, errMenuStaleJobNil
	}
	projectID, kind, ok := buildcontract.SplitMenuStaleSourceID(job.SourceID)
	if !ok {
		return menuStaleJob{}, fmt.Errorf("%w: %q", errMenuStaleSourceID, job.SourceID)
	}
	if !strings.EqualFold(strings.TrimSpace(job.ProjectID), projectID) {
		return menuStaleJob{}, fmt.Errorf("%w: 任务 %q / source_id %q", errMenuStaleProjectMismatch, job.ProjectID, projectID)
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return menuStaleJob{}, errMenuStaleKindMissing
	}
	return menuStaleJob{ProjectID: projectID, Kind: kind}, nil
}

func executeMenuStaleJob(ctx context.Context, job *builddto.Job,
	lookup pagecontract.PageDependencyLookup, rebuild menuStalePageRebuilder,
	instances presentationcontract.StaleRebuildPort) error {
	decoded, err := decodeMenuStaleJob(job)
	if err != nil {
		return err
	}

	key := publish.MenuKey(decoded.ProjectID, decoded.Kind)
	refs, err := lookup.FindPagesByDependency(ctx, decoded.ProjectID, key.Kind, key.Key)
	if err != nil {

		return err
	}
	directIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id := strings.TrimSpace(ref.ID); id != "" {
			directIDs = append(directIDs, id)
		}
	}

	staleIDs, serr := stalePageIDs(ctx, rebuild, decoded.ProjectID)
	if serr != nil {
		return serr
	}

	merged := mergePageIDs(directIDs, staleIDs)
	if err := rebuildStaleInChunks(ctx, rebuild, merged); err != nil {
		return err
	}
	if _, err := instances.RebuildStaleSync(ctx, decoded.ProjectID); err != nil {
		return err
	}
	return nil
}

func mergePageIDs(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, raw := range group {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

const menuStaleRebuildChunk = pagecontract.RebuildBatchCapacity

func rebuildStaleInChunks(ctx context.Context, rebuild menuStalePageRebuilder, ids []string) error {
	for start := 0; start < len(ids); start += menuStaleRebuildChunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + menuStaleRebuildChunk
		if end > len(ids) {
			end = len(ids)
		}
		if err := rebuild.RebuildStaleStrict(ctx, ids[start:end]); err != nil {
			return fmt.Errorf("菜单失效重建第 %d-%d 页（共 %d）: %w", start+1, end, len(ids), err)
		}
	}
	return nil
}

type menuStalePageRebuilder interface {
	RebuildStaleStrict(ctx context.Context, ids []string) error
	ListStalePageIDs(ctx context.Context, projectID string) (ids []string, err error)
}

type menuStaleStalePageLister interface {
	ListStalePageIDs(ctx context.Context, projectID string) (ids []string, err error)
}

func stalePageIDs(ctx context.Context, rebuild menuStalePageRebuilder, projectID string) ([]string, error) {
	lister, ok := rebuild.(menuStaleStalePageLister)
	if !ok {

		return nil, errors.New("页面模块未暴露待重建页面 id 清单（ListStalePageIDs）")
	}
	ids, err := lister.ListStalePageIDs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

func registerMenuStaleExecutor(buildSvc buildcontract.BuildService,
	pageSvc pagecontract.PageService, presentationSvc presentationcontract.PresentationService) {
	lookup, ok := pageSvc.(pagecontract.PageDependencyLookup)
	if !ok {
		panic("页面模块未实现按依赖只读反查（PageDependencyLookup）")
	}
	instances, ok := presentationSvc.(presentationcontract.StaleRebuildPort)
	if !ok {
		panic("自动发布实例模块未实现同步重建端口（StaleRebuildPort）")
	}

	rebuilder, ok := pageSvc.(menuStalePageRebuilder)
	if !ok {
		panic("页面模块未同时提供严格重建与待重建 id 清单（RebuildStaleStrict + ListStalePageIDs）")
	}
	buildSvc.RegisterExecutor(buildmodel.SourceTypeMenuStale,
		func(ctx context.Context, job *builddto.Job) error {
			return executeMenuStaleJob(ctx, job, lookup, rebuilder, instances)
		})
}
