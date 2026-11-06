package feature

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/pipeline"
)

// registerNavigationBusinessViews 走生产同一装配入口登记业务组件视图。
// 组件自身的 init 只登记组件与模板，ViewBuilder 由应用装配显式注册；
// 测试必须在首次编译/组件 schema/缓存读取之前完成这一步。
func registerNavigationBusinessViews(t *testing.T) {
	t.Helper()
	pipeline.RegisterBusinessViews()
}

type navWritePortFixture struct {
	Pages navigationcontract.PageStalePort

	Items navigationcontract.MenuItemStalePort

	Instances navigationcontract.PresentationStalePort

	ItemInstances navigationcontract.MenuItemPresentationStalePort

	Queue navigationcontract.MenuStaleEnqueuePort

	PageSvc      *pageservice.Service
	BuildSvc     *buildservice.Service
	InstancesSvc *presentationservice.Service
}

func navWritePortsFor(db *gorm.DB, projects projectcontract.ProjectService) navWritePortFixture {
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
		nil, nil, nil, nil)

	instances := presentationservice.NewService(presentationmodel.NewModel(db),
		nil, nil, projects, blocks, routes)
	queue := buildservice.NewService(buildmodel.NewModel(db))
	return navWritePortFixture{
		Pages:         pages,
		Items:         pages,
		Instances:     instances,
		ItemInstances: instances,
		Queue:         queue,
		PageSvc:       pages,
		BuildSvc:      queue,
		InstancesSvc:  instances,
	}
}

func attachNavWritePorts(t *testing.T, svc *navigationservice.Service,
	db *gorm.DB, projects projectcontract.ProjectService) navWritePortFixture {
	t.Helper()
	if svc == nil {
		t.Fatal("attachNavWritePorts 需要真实导航服务")
	}
	if db == nil {
		t.Fatal("attachNavWritePorts 需要真实迁移库（端口要往同一事务里写）")
	}
	fx := navWritePortsFor(db, projects)
	svc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         fx.Pages,
		Instances:     fx.Instances,
		Items:         fx.Items,
		ItemInstances: fx.ItemInstances,
		Queue:         fx.Queue,
	})
	return fx
}

type navWritePortsBypass struct{}

func (navWritePortsBypass) MarkMenuStaleTx(context.Context, *gorm.DB, string, string, time.Time) (int64, error) {
	return 0, nil
}

func (navWritePortsBypass) MarkInstanceMenuStaleTx(context.Context, *gorm.DB, string, string, time.Time) (int64, error) {
	return 0, nil
}

func (navWritePortsBypass) MarkNavigationStaleTx(context.Context, *gorm.DB, string, string) (int64, error) {
	return 0, nil
}

func (navWritePortsBypass) MarkInstanceNavigationStaleTx(context.Context, *gorm.DB,
	string, string, time.Time) (int64, error) {
	return 0, nil
}

func (navWritePortsBypass) EnqueueMenuStaleTx(context.Context, *gorm.DB, string, string) (int64, error) {
	return 0, nil
}

var (
	_ navigationcontract.PageStalePort                 = (*pageservice.Service)(nil)
	_ navigationcontract.MenuItemStalePort             = (*pageservice.Service)(nil)
	_ navigationcontract.PresentationStalePort         = (*presentationservice.Service)(nil)
	_ navigationcontract.MenuItemPresentationStalePort = (*presentationservice.Service)(nil)
	_ navigationcontract.MenuStaleEnqueuePort          = (*buildservice.Service)(nil)

	_ navigationcontract.PageStalePort                 = navWritePortsBypass{}
	_ navigationcontract.MenuItemStalePort             = navWritePortsBypass{}
	_ navigationcontract.PresentationStalePort         = navWritePortsBypass{}
	_ navigationcontract.MenuItemPresentationStalePort = navWritePortsBypass{}
	_ navigationcontract.MenuStaleEnqueuePort          = navWritePortsBypass{}
)

type navStaleCounter struct {
	mu   sync.Mutex
	menu []string
	nav  []string
}

func (c *navStaleCounter) recordMenu(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.menu = append(c.menu, key)
}

func (c *navStaleCounter) recordNav(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nav = append(c.nav, key)
}

func (c *navStaleCounter) counts() (menu, nav int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.menu), len(c.nav)
}

func (c *navStaleCounter) snapshot() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.menu...), append([]string(nil), c.nav...)
}

type navStaleCountPages struct {
	c   *navStaleCounter
	nav navigationcontract.PageStalePort
}

func (w navStaleCountPages) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (int64, error) {
	n, err := w.nav.MarkMenuStaleTx(ctx, tx, projectID, kind, at)
	if err == nil {
		w.c.recordMenu(projectID + "|" + kind)
	}
	return n, err
}

type navStaleCountItems struct {
	c   *navStaleCounter
	nav navigationcontract.MenuItemStalePort
}

func (w navStaleCountItems) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error) {
	n, err := w.nav.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
	if err == nil {
		w.c.recordNav(projectID + "|" + navigationID)
	}
	return n, err
}

func navStaleCountDeps(fx navWritePortFixture, c *navStaleCounter) navigationservice.NavigationWriteDeps {
	return navigationservice.NavigationWriteDeps{
		Pages:         navStaleCountPages{c: c, nav: fx.Pages},
		Instances:     fx.Instances,
		Items:         navStaleCountItems{c: c, nav: fx.Items},
		ItemInstances: fx.ItemInstances,
		Queue:         fx.Queue,
	}
}
