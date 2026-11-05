package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"
	"time"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"

	"gorm.io/gorm"
)

const (
	menuKindHeader = "header"
	menuKindFooter = "footer"
)

func navMenuDocument(kind string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav-1","type":"core.nav","props":{"menu":"` + kind + `"}}]}`
}

func seedMenuItem(t *testing.T, svc navigationcontract.NavigationService, projectID, kind, title, path string) {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind,
		SourceType: "custom", Target: "self",
	}); err != nil {
		t.Fatalf("创建菜单项(%s/%s)失败: %v", kind, title, err)
	}
}

func menuDependencyExists(t *testing.T, db *gorm.DB, pageID, key string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM page_dependencies WHERE page_id = ? AND dependency_kind = ? AND dependency_key = ?",
		pageID, publish.DepKindMenu, key).Scan(&n).Error; err != nil {
		t.Fatalf("查询依赖行失败: %v", err)
	}
	return n > 0
}

func inputVersionOf(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取 input_version 失败: %v", err)
	}
	return v
}

func TestMenuStalePortHitsProductionDependencyKey(t *testing.T) {

	db, svc, navSvc, projects, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	seedMenuItem(t, navSvc, projectID, menuKindHeader, "首页", "/home")

	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}

	seedMenuItem(t, navSvc, other.ID, menuKindHeader, "首页", "/home")

	page := createPage(t, svc, projectID, "/menu-owner", navMenuDocument(menuKindHeader)).ID
	buildAndPublish(t, svc, page)
	foreign := createPage(t, svc, other.ID, "/menu-owner", navMenuDocument(menuKindHeader)).ID
	buildAndPublish(t, svc, foreign)

	fullKey := publish.MenuKey(projectID, menuKindHeader).Key
	if !menuDependencyExists(t, db, page, fullKey) {
		t.Fatalf("构建期应登记菜单依赖 %q（kind=%q）", fullKey, publish.DepKindMenu)
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
