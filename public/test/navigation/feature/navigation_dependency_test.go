package feature

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/pipeline"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func newNavDependencyEnv(t *testing.T) (db *gorm.DB, pages pagecontract.PageService,
	navSvc navigationcontract.NavigationService, projects projectcontract.ProjectService, projectID string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db = support.NewMigratedPGTestDB(t)
	projects = projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "导航失效测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	nav := navigationservice.NewService(navigationmodel.NewModel(db), projects)

	attachNavWritePorts(t, nav, db, projects)
	navSvc = nav
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages = pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks, nil, nil, navSvc, nil)

	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, pages)
	nav.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	return db, pages, navSvc, projects, project.ID
}

func createNavPage(t *testing.T, svc pagecontract.PageService, projectID, kind, path, doc string) string {
	t.Helper()
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: kind, ContentTargetType: "none",
		DraftPath: path, DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建页面(%s)失败: %v", path, err)
	}
	return created.ID
}

func buildNavPage(t *testing.T, svc pagecontract.PageService, pageID string) string {
	t.Helper()
	built, err := svc.Build(context.Background(), &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建页面 %s 失败: %v", pageID, err)
	}
	return built.StagedHash
}

func readStagedHTML(t *testing.T, hash string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", hash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物 %s 失败: %v", hash, err)
	}
	return string(b)
}

func navMenuDependencyCount(t *testing.T, db *gorm.DB, pageID, projectID, kind string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_dependencies
		WHERE page_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		pageID, pipeline.DepKindMenu, "menu:"+projectID+":"+kind).Scan(&n).Error; err != nil {
		t.Fatalf("统计导航依赖失败: %v", err)
	}
	return n
}

func markPageActivated(t *testing.T, db *gorm.DB, pageID string) {
	t.Helper()
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("把页面 %s 摆成已收敛状态失败: %v", pageID, err)
	}
}

func navPageStale(t *testing.T, db *gorm.DB, pageID string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", pageID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	return stale
}

func TestNavigationMenuDependencyRegistered(t *testing.T) {
	db, pages, navSvc, _, projectID := newNavDependencyEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	bound := createNavPage(t, pages, projectID, "home", "/nav-bound", navDocument)
	plain := createNavPage(t, pages, projectID, "search", "/nav-plain", customNavDocument)
	buildNavPage(t, pages, bound)
	buildNavPage(t, pages, plain)

	if n := navMenuDependencyCount(t, db, bound, projectID, "header"); n != 1 {
		t.Fatalf("绑定页眉导航的页面应声明恰好 1 条 menu:{pid}:header 依赖，实际 %d 条", n)
	}

	if n := navMenuDependencyCount(t, db, plain, projectID, "header"); n != 0 {
		t.Fatalf("手写菜单的页面不应声明导航依赖，实际 %d 条", n)
	}

	if n := navMenuDependencyCount(t, db, bound, projectID, "footer"); n != 0 {
		t.Fatalf("未绑定的 footer 位置不应被登记，实际 %d 条", n)
	}
}

func TestNavigationChangeMarksOnlyBoundPages(t *testing.T) {
	db, pages, navSvc, _, projectID := newNavDependencyEnv(t)
	itemID := addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	bound := createNavPage(t, pages, projectID, "home", "/dep-bound", navDocument)
	plain := createNavPage(t, pages, projectID, "search", "/dep-plain", customNavDocument)
	buildNavPage(t, pages, bound)
	buildNavPage(t, pages, plain)
	for _, id := range []string{bound, plain} {
		markPageActivated(t, db, id)
	}
	for _, id := range []string{bound, plain} {
		if navPageStale(t, db, id) {
			t.Fatalf("前置条件不成立：构建后页面 %s 仍为待重建", id)
		}
	}

	newPath := "/changed"
	if _, err := navSvc.Update(context.Background(), &navigationdto.UpdateReq{ID: itemID, Path: &newPath}); err != nil {
		t.Fatalf("更新导航项失败: %v", err)
	}

	if !navPageStale(t, db, bound) {
		t.Fatalf("把页眉导航烘进产物的页面应被标记待重建")
	}
	if navPageStale(t, db, plain) {
		t.Fatalf("手写菜单的页面不应被标记待重建（这正是全站标记要消除的误伤）")
	}
}

func TestNavigationChangeIsolatedByProject(t *testing.T) {
	db, pages, navSvc, projects, projectA := newNavDependencyEnv(t)
	ctx := context.Background()

	itemA := addNavItem(t, navSvc, projectA, "首页", "/", nil, "self")
	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一个站点工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	addNavItem(t, navSvc, other.ID, "首页", "/", nil, "self")

	boundA := createNavPage(t, pages, projectA, "home", "/proj-a", navDocument)
	boundB := createNavPage(t, pages, other.ID, "home", "/proj-b", navDocument)
	buildNavPage(t, pages, boundA)
	buildNavPage(t, pages, boundB)
	markPageActivated(t, db, boundA)
	markPageActivated(t, db, boundB)

	if n := navMenuDependencyCount(t, db, boundA, projectA, "header"); n != 1 {
		t.Fatalf("A 工程页面应声明本工程的导航依赖，实际 %d 条", n)
	}
	if n := navMenuDependencyCount(t, db, boundB, other.ID, "header"); n != 1 {
		t.Fatalf("B 工程页面应声明本工程的导航依赖，实际 %d 条", n)
	}

	newPath := "/a-changed"
	if _, err = navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemA, Path: &newPath}); err != nil {
		t.Fatalf("更新 A 工程导航项失败: %v", err)
	}

	if !navPageStale(t, db, boundA) {
		t.Fatalf("A 工程绑定页眉导航的页面应被标记待重建")
	}
	if navPageStale(t, db, boundB) {
		t.Fatalf("B 工程的页面被跨工程误标（导航键必须带工程 ID）")
	}
}

func TestNavigationRebuildReflectsMenuChange(t *testing.T) {
	db, pages, navSvc, _, projectID := newNavDependencyEnv(t)
	ctx := context.Background()

	itemID := addNavItem(t, navSvc, projectID, "旧菜单", "/old-menu", nil, "self")
	pageID := createNavPage(t, pages, projectID, "home", "/nav-e2e", navDocument)

	before := buildNavPage(t, pages, pageID)
	beforeHTML := readStagedHTML(t, before)
	if !strings.Contains(beforeHTML, "/old-menu") {
		t.Fatalf("重建前产物里应含旧菜单链接 /old-menu: %s", beforeHTML)
	}

	newPath := "/new-menu"
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, Path: &newPath}); err != nil {
		t.Fatalf("更新导航项失败: %v", err)
	}

	if !navPageStale(t, db, pageID) {
		t.Fatalf("改导航后页面应被标记待重建")
	}

	after := buildNavPage(t, pages, pageID)
	if after == before {
		t.Fatalf("导航内容变了，重建后的产物 hash 不应相同（%s）", before)
	}
	afterHTML := readStagedHTML(t, after)
	if !strings.Contains(afterHTML, "/new-menu") {
		t.Fatalf("重建后产物应含新菜单链接 /new-menu: %s", afterHTML)
	}
	if strings.Contains(afterHTML, "/old-menu") {
		t.Fatalf("重建后产物不应再含旧菜单链接 /old-menu: %s", afterHTML)
	}

	if !navPageStale(t, db, pageID) {
		t.Fatalf("构建后 stale 不应被清掉：清 stale 只发生在成功激活（MarkPublishedLangTx）")
	}

	if _, err := pages.Publish(ctx, &pagedto.PublishReq{ID: pageID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	activeBytes, aerr := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "public", "active", "nav-e2e", "index.html"))
	if aerr != nil {
		t.Fatalf("读取激活产物失败: %v", aerr)
	}
	if navPageStale(t, db, pageID) {
		t.Fatalf("激活成功后页面不应仍为待重建")
	}
	if !strings.Contains(string(activeBytes), "/new-menu") {
		t.Fatalf("激活产物应含新菜单链接 /new-menu: %s", string(activeBytes))
	}
	if strings.Contains(string(activeBytes), "/old-menu") {
		t.Fatalf("激活产物不应再含旧菜单链接 /old-menu: %s", string(activeBytes))
	}
}
