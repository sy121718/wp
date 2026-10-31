package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	buildcontract "go_wp/internal/module/build/contract"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

const headingDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	return newPageServiceWithRouter(t, nil)
}

func newPageServiceWithRoutes(t *testing.T) (*gorm.DB, pagecontract.PageService, pubcontract.PublicationService, string) {
	t.Helper()
	var routes pubcontract.PublicationService
	db, svc, _, projectID := newPageServiceWithRouter(t, func(r pubcontract.PublicationService) pubcontract.PublicationService {
		routes = r
		return r
	})
	if routes == nil {
		t.Fatal("页面服务装配未提供路由契约")
	}
	return db, svc, routes, projectID
}

func newPageServiceWithRouter(t *testing.T, wrap func(pubcontract.PublicationService) pubcontract.PublicationService) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	var routes pubcontract.PublicationService = pubservice.NewService(pubmodel.NewPublicationModel(db))
	if wrap != nil {
		routes = wrap(routes)
	}
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)

	navSvc := navigationservice.NewService(navigationmodel.NewModel(db), projects)

	return db, pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, navSvc, nil), projects, project.ID
}

func newPageServiceWithNavigationWrite(t *testing.T) (*gorm.DB, pagecontract.PageService, navigationcontract.NavigationService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	navSvc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db),
		artifactservice.NewService(artifactmodel.NewArtifactModel(db)),
		routes, projects, blocks, nil, nil, navSvc, nil)

	instances := presentationservice.NewService(
		presentationmodel.NewModel(db), nil, nil, projects, blocks, routes)
	queue := buildservice.NewService(buildmodel.NewModel(db))
	navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         pageMenuStaleAdapter{svc: pages},
		Instances:     presentationMenuStaleAdapter{svc: instances},
		Items:         pageNavigationStaleAdapter{svc: pages},
		ItemInstances: presentationNavigationStaleAdapter{svc: instances},
		Queue:         buildQueueAdapter{svc: queue},
	})
	return db, pages, navSvc, projects, project.ID
}

type pageMenuStaleAdapter struct{ svc pagecontract.PageService }

func (a pageMenuStaleAdapter) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (int64, error) {
	port, ok := a.svc.(pagecontract.PageMenuStalePort)
	if !ok {
		return 0, errors.New("页面模块未实现同事务菜单标记（PageMenuStalePort）")
	}
	return port.MarkMenuStaleTx(ctx, tx, projectID, kind, at)
}

type pageNavigationStaleAdapter struct{ svc pagecontract.PageService }

func (a pageNavigationStaleAdapter) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error) {
	marker, ok := a.svc.(interface {
		MarkNavigationStaleTx(context.Context, *gorm.DB, string, string) (int64, error)
	})
	if !ok {
		return 0, errors.New("页面模块未实现同事务按项菜单标记（navigation 契约 MenuItemStalePort）")
	}
	return marker.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

type presentationMenuStaleAdapter struct {
	svc presentationcontract.PresentationService
}

func (a presentationMenuStaleAdapter) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (int64, error) {
	port, ok := a.svc.(presentationcontract.PresentationStalePort)
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务菜单标记（PresentationStalePort）")
	}
	return port.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at)
}

type presentationNavigationStaleAdapter struct {
	svc presentationcontract.PresentationService
}

func (a presentationNavigationStaleAdapter) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (int64, error) {
	marker, ok := a.svc.(interface {
		MarkInstanceNavigationStaleTx(context.Context, *gorm.DB, string, string, time.Time) (int64, error)
	})
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务按项菜单标记")
	}
	return marker.MarkInstanceNavigationStaleTx(ctx, tx, projectID, navigationID, at)
}

type buildQueueAdapter struct{ svc buildcontract.BuildService }

func (a buildQueueAdapter) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (int64, error) {
	return a.svc.EnqueueMenuStaleTx(ctx, tx, projectID, kind)
}

func createPage(t *testing.T, svc pagecontract.PageService, projectID, path, doc string) *pagedto.PageResp {
	t.Helper()
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建 Page(%s) 失败: %v", path, err)
	}
	return created
}

func buildAndPublish(t *testing.T, svc pagecontract.PageService, pageID string) string {
	t.Helper()
	ctx := context.Background()
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: pageID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	return built.StagedHash
}

func routeKind(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, path).Scan(&kind).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	return kind
}
