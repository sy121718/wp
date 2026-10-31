package unit

// page_menu_stale_port_test.go —— 菜单位置失效端口的**生产链路**（真实 PG + 真实装配）。
//
// 端口（pagecontract.PageMenuStalePort.MarkMenuStaleTx）在生产里只有一个调用方：
// navigation 的译文写服务在**写译文的同一个事务**里逐 kind 调它。这里不手写
// page_dependencies 行 —— 依赖由页面自己的编译链登记：
//
//	页面文档挂一个 core.nav（props.menu = "header"）
//	  → 编译期 jetview.resolveNavMenu 真实解析该位置并 ctx.UseMenu("header")
//	  → page_lang.go 按 pipeline.MenuKey(projectID, kind) 登记依赖
//	  → 端口按同一完整键命中
//
// 为什么不用手写依赖行的夹具：缺陷恰恰是「登记用完整键、标记用裸位置名」。
// 自造夹具（裸 'header'）会把这条缺陷测成绿色，而真实站点的表现是
// 「译文保存成功、页面永远不重建」，且**不报错**。真链路里 n=1 就是判据。
//
// 本文件覆盖：
//  1. 构建期登记的菜单依赖键 = pipeline.MenuKey(projectID, kind)（裸位置名不得入库）；
//  2. 端口在调用方事务内命中该页：stale + input_version 同一条 UPDATE 推进；
//  3. 位置精确（页眉变更不动只挂页脚的页面）、工程精确（别的工程同位置不动）。

import (
	"context"
	"testing"
	"time"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

// navMenuKinds 菜单位置的**生产词汇表**（与 nav.Props.Menu 的 ct 选项一致）。
const (
	menuKindHeader = "header"
	menuKindFooter = "footer"
)

// navMenuDocument 挂一个绑定指定位置的 core.nav 的页面文档。
func navMenuDocument(kind string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav-1","type":"core.nav","props":{"menu":"` + kind + `"}}]}`
}

// seedMenuItem 给某工程某位置建一条菜单项（编译期解析该位置时要有内容，空菜单校验会失败）。
//
// Path 必须给 ASCII：菜单项链接进 core.nav 的白名单（builder/components/nav/nav.go:190
// → core.IsSafeURL → urlSafeRe，controls.go:395 只收 [A-Za-z0-9./:?=&%~#+_@-]），
// 用 "/"+title 拼中文路径会让构建期直接判「菜单项链接非法」。标题仍用中文，
// 本用例只关心依赖键命中，不受影响。
func seedMenuItem(t *testing.T, svc navigationcontract.NavigationService, projectID, kind, title, path string) {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind,
		SourceType: "custom", Target: "self",
	}); err != nil {
		t.Fatalf("创建菜单项(%s/%s)失败: %v", kind, title, err)
	}
}

// menuDependencyExists 该页是否登记了指定依赖键的菜单依赖。
func menuDependencyExists(t *testing.T, db *gorm.DB, pageID, key string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM page_dependencies WHERE page_id = ? AND dependency_kind = ? AND dependency_key = ?",
		pageID, pipeline.DepKindMenu, key).Scan(&n).Error; err != nil {
		t.Fatalf("查询依赖行失败: %v", err)
	}
	return n > 0
}

// inputVersionOf 读页面当前输入代次。
func inputVersionOf(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取 input_version 失败: %v", err)
	}
	return v
}

// TestMenuStalePortHitsProductionDependencyKey 端口按生产依赖键命中真实构建登记的依赖。
func TestMenuStalePortHitsProductionDependencyKey(t *testing.T) {
	// 菜单项用**带真实写端口**的装配创建：navigation 的菜单 CRUD 在端口不齐时
	// fail closed（writePortsReady），裸 NewService 建出的实例只能读。
	db, svc, navSvc, projects, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	seedMenuItem(t, navSvc, projectID, menuKindHeader, "首页", "/home")

	// 第二个工程：证明端口不跨工程误命中（键里带工程 id 正是为此）。
	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	// 同一 service 可服务任意工程（端口与工程无关）：换工程只换 projectID。
	seedMenuItem(t, navSvc, other.ID, menuKindHeader, "首页", "/home")

	page := createPage(t, svc, projectID, "/menu-owner", navMenuDocument(menuKindHeader)).ID
	buildAndPublish(t, svc, page)
	foreign := createPage(t, svc, other.ID, "/menu-owner", navMenuDocument(menuKindHeader)).ID
	buildAndPublish(t, svc, foreign)

	fullKey := pipeline.MenuKey(projectID, menuKindHeader).Key
	if !menuDependencyExists(t, db, page, fullKey) {
		t.Fatalf("构建期应登记菜单依赖 %q（kind=%q）", fullKey, pipeline.DepKindMenu)
	}
	if menuDependencyExists(t, db, page, menuKindHeader) {
		t.Fatalf("依赖键不得是裸位置名 %q：登记侧与标记侧必须同一个构造函数", menuKindHeader)
	}

	port, ok := svc.(pagecontract.PageMenuStalePort)
	if !ok {
		t.Fatal("页面 Service 必须实现 pagecontract.PageMenuStalePort")
	}
	before := inputVersionOf(t, db, page)
	foreignBefore := inputVersionOf(t, db, foreign)
	var hit int64
	if err := pagemodel.NewPageModel(db).TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		n, merr := port.MarkMenuStaleTx(ctx, tx, projectID, menuKindHeader, time.Now().UTC())
		hit = n
		return merr
	}); err != nil {
		t.Fatalf("菜单标记失败: %v", err)
	}
	if hit != 1 {
		t.Fatalf("应命中 1 个页面，实际 %d（实现拿裸 kind 做精确等式时这里恒 0）", hit)
	}
	if !pageStale(t, db, page) {
		t.Fatal("端口命中后页面应为 stale")
	}
	if got := inputVersionOf(t, db, page); got != before+1 {
		t.Fatalf("菜单标记应推进代次 %d → %d，实际 %d", before, before+1, got)
	}
	if got := inputVersionOf(t, db, foreign); got != foreignBefore {
		t.Fatalf("别的工程同位置页面不应被推进代次（%d → %d）", foreignBefore, got)
	}
	if pageStale(t, db, foreign) {
		t.Fatal("别的工程同位置页面不应被标记 stale")
	}
}

// TestMenuStalePortIsPositionExact 位置精确：页眉变更不动只挂页脚的页面。
func TestMenuStalePortIsPositionExact(t *testing.T) {
	db, svc, navSvc, _, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	seedMenuItem(t, navSvc, projectID, menuKindHeader, "首页", "/home")
	seedMenuItem(t, navSvc, projectID, menuKindFooter, "关于", "/about")

	headerPage := createPage(t, svc, projectID, "/pos/header", navMenuDocument(menuKindHeader)).ID
	footerPage := createPage(t, svc, projectID, "/pos/footer", navMenuDocument(menuKindFooter)).ID
	buildAndPublish(t, svc, headerPage)
	buildAndPublish(t, svc, footerPage)

	port := svc.(pagecontract.PageMenuStalePort)
	footerBefore := inputVersionOf(t, db, footerPage)
	var hit int64
	if err := pagemodel.NewPageModel(db).TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		n, merr := port.MarkMenuStaleTx(ctx, tx, projectID, menuKindHeader, time.Now().UTC())
		hit = n
		return merr
	}); err != nil {
		t.Fatalf("菜单标记失败: %v", err)
	}
	if hit != 1 {
		t.Fatalf("页眉变更应只命中页眉页，实际命中 %d", hit)
	}
	if !pageStale(t, db, headerPage) {
		t.Fatal("页眉页应被标记 stale")
	}
	if got := inputVersionOf(t, db, footerPage); got != footerBefore {
		t.Fatalf("只挂页脚的页面代次不应变化（%d → %d）", footerBefore, got)
	}
	if pageStale(t, db, footerPage) {
		t.Fatal("只挂页脚的页面不应被标记 stale")
	}
}
