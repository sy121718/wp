// Package unit page 模块 service 层单元测试。
//
// 覆盖 Page Document 生命周期：创建/保存草稿、查询、主题合入、
// 发布链路与软删语义。依赖按 NewService 签名注入真实实现
// （project/artifact/block/publication service，同一 PG schema）。
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

// pageDocument 最小合法页面文档（空 root）。
const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// headingDocument 含一个 heading 节点的合法文档。
const headingDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

// newPageService 装配 page service 及其全部真实依赖（同一 PG schema）。
// 表结构由生产迁移建立（support.NewMigratedPGTestDB，真实 106 张表），不再手抄 DDL；
// 测试只补真实父行（这里通过 project service 建一个真实工程）。
// 返回 db（供直接 SQL 断言）、svc、projects（供主题用例）、projectID。
func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	return newPageServiceWithRouter(t, nil)
}

// newPageServiceWithRoutes 返回 serving 用的真实路由契约实例（与页面服务装配的是**同一个**
// publication service，不是另起一个）。需要直接登记回执的用例必须走它：
// 另起实例虽然读写同一张表，却会掩盖「服务实际持有的是谁」这一装配事实。
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

// newPageServiceWithRouter 同上，但允许包装路由契约（wrap 为 nil = 用真实 publication service）。
// 故障注入用例要让「回执登记」这一步失败，而登记只在 page 侧调用 —— 只能从契约外部注入。
// 传包装函数而不是整个契约：装饰器嵌在真实实现上，其余方法照常透传。
func newPageServiceWithRouter(t *testing.T, wrap func(pubcontract.PublicationService) pubcontract.PublicationService) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	// 建站会自带一套默认主题（生产语义：先有主题再有页面）；这批用例测的是
	// 「完全没有主题时怎么办」与「自己建的主题怎么走」，前提需要干净，所以先清掉。
	// 「建站即有默认主题」由 public/test/project/feature 的用例守着。
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
	// 导航契约接真实实现（不是 nil）：core.nav 绑定菜单位置时构建期必须能解析菜单，
	// 否则构建显式失败 —— 而「菜单依赖键是否被登记」正是菜单位置失效链路的起点，
	// 用 nil 导航会让所有涉及菜单的用例在另一个地方失败（与用例要验的东西无关）。
	navSvc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	// 用例若要往某位置塞菜单项，用一个读写同一张表的 navigation service 实例即可：
	// 构建期解析菜单走的是同一个契约与同一张表（不存在进程内菜单缓存）。
	// 关键是**不得**把它接成 nil —— 那会让所有挂 core.nav 的页面构建直接失败。

	return db, pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, navSvc, nil), projects, project.ID
}

// newPageServiceWithNavigationWrite 装配 page service，并把 navigation service 的
// **写端口**接成同一测试库上的真实实现（Pages / Items / Instances / ItemInstances / Queue）。
//
// 为什么必需：navigation 的菜单 CRUD 在端口不齐时 fail closed
// （internal/module/navigation/service/navigation_service.go:196 writePortsReady），
// 裸 NewService 建出来的实例只能读、不能写 —— 用它去 Create 菜单项会直接报
// "译文写暂不可用"，而「菜单项改动 → 页面按位置/按项失效」正是本组用例要证明的链路，
// 用 noop 端口等于把被测行为换成桩。
//
// 五个端口全部是**本测试库的真实服务实现**（page / presentation / build），与
// internal/routers/assembly_navigation_translation.go:163 navigationWritePorts 同形；
// 适配用下面的收窄类型在本包内完成，不跨包引用 feature helper（那会把测试支撑
// 反向绑成一个跨模块依赖）。
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
	// 实例与队列端口用同一测试库上的真实服务：失效标记写的是它们自己的表，
	// 桩实现会让「谁被标 stale」变成回答不了的问题。
	// 实例服务只为提供「按位置/按项标记实例 stale」的真实实现：模板与注册表传 nil
	// 不表示简化装配，而是这两个依赖只服务构建入口，本组用例不构建实例产物。
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

// 下面五个适配器只是把真实 service 收窄成 navigation 契约的端口形状，
// 与 internal/routers/assembly_navigation_translation.go:163 的装配一一对应
// （端口未实现时返回错误而不是 0 —— 装配缺陷必须整笔失败）。
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

// createPage 创建指定路径的页面并返回投影。
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

// buildAndPublish 构建并发布页面，返回暂存 hash。
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

// routeKind 读取指定路径的路由占用状态。
func routeKind(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, path).Scan(&kind).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	return kind
}
