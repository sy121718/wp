package feature

// navigation_write_ports_fixture_test.go — 导航写端口的**共享测试夹具**。
//
// 背景：菜单写路径（Create / Update / Delete / SwapMove）与译文整表提交共用同一组同事务
// 端口：标手工页面 stale（page）、标自动发布实例 stale 并推进 input_version（presentation）、
// 可靠入队 menu_stale（build）。service 侧 writePortsReady 对这四者（含按项端口）是
// **全齐才放行**，缺任一即 ErrTranslationWriteUnavailableError —— 这是刻意的 fail closed：
// 缺端口时「保存成功、线上停在旧菜单」是此前真实出现过的形态。
//
// 于是任何「裸 NewService + 只挂 SetMenuStaleDispatcher」的旧夹具都会让 CRUD 整批失败。
// 本文件提供**一个**装配点 attachNavWritePorts：它接的是真实实现（真实迁移库句柄上的
// page / presentation / build 服务），不是探针也不是空桩。
//
// 为什么真实实现优于探针：
//   - 探针只能证明「端口被调用了」，证明不了依赖键拼对、命中口径选对、事务真的同进同出；
//   - 真实实现让库自己说话：pages.stale 真的变 true、presentation 的 input_version 真的前进、
//     build_jobs 真的多出一条 menu_stale 任务。用例断言这些**可观测结果**，而不是调用次数。
//
// 与生产装配同构的部分：internal/routers/assembly_navigation_translation.go 的
// navigationWritePorts 也是把同一批模块的 **…Tx 能力**交给导航服务，唯一差别是
// 生产装配拿的是进程级单例、这里拿的是测试库句柄。
//
// 与另一份夹具（navigation_translation_ports_test.go 的 wireNavigationTranslationWriter）
// 的关系：那份装的是**译文写服务**（TranslationWriter，走 HTTP 句柄），本份装的是
// **菜单 CRUD** 的四端口。两者的端口取值必须同源 —— 生产装配里它们确实共用同一批适配器，
// 所以本文件另导出 navWritePortsFor 给译文夹具复用，避免「两条写链各装一套」的口径漂移。

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
)

// navWritePortFixture 一次装配得到的四端口 + 用来断言的两件真实服务。
//
// Pages / Instances / Queue 三字段就是塞进 NavigationWriteDeps 的那些端口；
// Build 与 PageSvc 另外留出来，方便用例直接查队列或跑构建（不必再构造一遍）。
type navWritePortFixture struct {
	// Pages 手工页面的按位置 + 按项同事务标记（page 模块，**真实**实现）。
	Pages navigationcontract.PageStalePort
	// Items 手工页面的按菜单项同事务标记（page 模块，**真实**实现）。
	Items navigationcontract.MenuItemStalePort
	// Instances 自动发布实例的按位置同事务标记（presentation 模块，**真实**实现）。
	Instances navigationcontract.PresentationStalePort
	// ItemInstances 自动发布实例的按**菜单项**同事务标记（presentation 模块，**真实**实现）。
	// 键是 navigation:{itemID} 而不是 menu:{project}:{kind}：只引用某一支的实例既不被位置
	// 键命中，也拿不到位置版的版本屏障，所以这是另一个端口而不是 Instances 的重载。
	ItemInstances navigationcontract.MenuItemPresentationStalePort
	// Queue menu_stale 的可靠入队（build 模块，**真实**实现：写 build_jobs 行）。
	Queue navigationcontract.MenuStaleEnqueuePort
	// PageSvc / BuildSvc / InstancesSvc 是上面端口背后的具体服务（断言与后续构建用）。
	PageSvc      *pageservice.Service
	BuildSvc     *buildservice.Service
	InstancesSvc *presentationservice.Service
}

// navWritePortsFor 用测试库句柄构造四端口（**唯一**构造点）。
//
// projects 需要是同一个工程服务实例：page / presentation 的标记都按工程作用域走，
// 用另一个实例不会错（它们读的是库），但保持一致能少一个「为什么这里传了两个」的问题。
func navWritePortsFor(db *gorm.DB, projects projectcontract.ProjectService) navWritePortFixture {
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
		nil, nil, nil, nil)
	// Instances / ItemInstances 都走同一个真实实例服务：位置键与项键是这个模块的两条
	// 独立命中口径（menu:{project}:{kind} vs navigation:{itemID}），不是同一方法的两个参数。
	// 导航侧依赖传 nil 而不是造假实现：presentation 的构造器参数本来就是可选的。
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

// attachNavWritePorts 把四端口装到真实导航服务上（**唯一**注入点）。
//
// 装配缺陷的可观测后果：不调用它时 Create / Update / Delete / SwapMove 全部返回
// ErrTranslationWriteUnavailableError（500），译文写服务构造出来也会报 instances 缺失。
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

// navWritePortsBypass 给**跨域**用例用的直通端口：只满足「写入口 fail closed」这一层，
// 不做任何真实失效。
//
// 为什么需要它：public/test/presentation/**、public/test/page/** 里有些用例只用导航
// Create 造前置数据（菜单栅栏、依赖登记），它们要断言的是**自己模块**的失效语义。
// 但在那里调 feature 包的 attachNavWritePorts 会引入反向依赖（presentation/page 的
// 测试去 import navigation 的 feature 夹具），而导航写端口又确实是 Create 的必需依赖。
//
// 使用约束（写在使用处，不是写在这里）：
//   - 只用在与导航失效无关的**前置数据构造**；
//   - 凡断言「导航变更该标记什么」的用例一律用 attachNavWritePorts 的真实实现 ——
//     直通端口返回 0 行且不入队，拿它断言失效等于自己把判据删掉。
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

// 编译期断言：夹具给出的实现必须满足导航契约声明的四个端口形状。
// 签名漂移（例如给端口加参数）会在编译期断掉，而不是等某条用例跑到写分支才 500。
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

// navStaleCounter 计数**同事务端口**被调用的次数与键。
//
// 为什么计数点在这里而不是 dispatcher：菜单写的失效已改为与写同事务的端口调用
// （navigation_service.go 的 markMenuStaleInTx / markNavigationStaleInTx），
// MenuStaleDispatcher 不再参与菜单 CRUD，挂它只会得到恒 0。
//
// 计数条件只看「端口被调用且没报错」，不看命中行数：零命中是合法结果（没有任何页面
// 声明该依赖），但「必须调用」是契约 —— 有依赖时才有版本屏障，这是要守住的那条。
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

// counts 返回 (菜单位置调用数, 菜单项调用数)。
func (c *navStaleCounter) counts() (menu, nav int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.menu), len(c.nav)
}

// snapshot 返回两份键的快照（断言键内容时用）。
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

// navStaleCountDeps 用计数装饰器包住 Pages/Items，其余三端口仍用真实实现（端口必须全齐）。
func navStaleCountDeps(fx navWritePortFixture, c *navStaleCounter) navigationservice.NavigationWriteDeps {
	return navigationservice.NavigationWriteDeps{
		Pages:         navStaleCountPages{c: c, nav: fx.Pages},
		Instances:     fx.Instances,
		Items:         navStaleCountItems{c: c, nav: fx.Items},
		ItemInstances: fx.ItemInstances,
		Queue:         fx.Queue,
	}
}
