package feature

// navigation_byid_panel_test.go — 批 1 / 批 2 的依赖闭环与位置扩展。
//
// 两条独立引用方式各有自己的依赖键，缺任一条的表现都是静默失效：
//   · core.nav 按具体菜单项引用（Props.Navigation）→ navigation:{itemID}
//   · 菜单项的悬浮面板引用全局块（超级菜单）→ block:{blockID}
// 位置取值同时扩到四个（含移动端）：桌面与移动端是两份独立菜单数据。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/pipeline"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// newNavPanelEnv 与 newNavDependencyEnv 同形，额外返回块服务：
// 面板用例要自己造块，并把块的失效派发接到同一扇出上（生产装配同形）。
func newNavPanelEnv(t *testing.T) (db *gorm.DB, pages pagecontract.PageService,
	navSvc navigationcontract.NavigationService, blocks *blockservice.Service, projectID string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db = support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil, nil, ""
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "菜单面板测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	nav := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	// 写端口（标页面 / 标实例 / 入队）是 Create / Update / Delete 的必需依赖：
	// 缺任一即 fail closed。本用例断言的正是「按项引用 → 改菜单项 → 引用页被标 stale」，
	// 所以装配真实实现（本页也算真实命中：标记落在 pages.stale 上）。
	attachNavWritePorts(t, nav, db, projects)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks = blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages = pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks, nil, nil, nav, nil)

	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, pages)
	nav.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	// 块失效 → 反查 block:{id} → 页面 stale（生产装配同形）。
	blocks.SetStalePropagator(func(ctx context.Context, blockID string) error {
		fanout.InvalidateKeys(ctx, pipeline.BlockKey(blockID))
		return nil
	})
	return db, pages, nav, blocks, project.ID
}

// navByItemDoc 绑定具体菜单项（Props.Navigation）的页面文档。
func navByItemDoc(itemID, title string) string {
	return "{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"" + title + "\"}}," +
		"\"root\":[{\"id\":\"nav1\",\"type\":\"core.nav\",\"props\":{\"navigation\":\"" + itemID + "\"}}]}"
}

// navDepCount 统计页面声明的某条依赖。
func navDepCount(t *testing.T, db *gorm.DB, pageID, kind, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_dependencies WHERE page_id = ? AND dependency_kind = ? AND dependency_key = ?",
		pageID, kind, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计依赖失败: %v", err)
	}
	return n
}

// TestNavigationByItemDependencyAndInvalidation 按项引用：登记 navigation:{itemID}，
// 菜单项改名后引用页面被标 stale。
func TestNavigationByItemDependencyAndInvalidation(t *testing.T) {
	db, pages, navSvc, _, projectID := newNavPanelEnv(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	itemID := addNavItem(t, navSvc, projectID, "产品", "/products", nil, "self")

	pageID := createNavPage(t, pages, projectID, "home", "/by-item", navByItemDoc(itemID, "按项引用"))
	buildNavPage(t, pages, pageID)

	if n := navDepCount(t, db, pageID, pipeline.DepKindNavigation, "navigation:"+itemID); n != 1 {
		t.Fatalf("按项引用应登记恰好 1 条 navigation:{itemID} 依赖，实际 %d 条", n)
	}
	if n := navDepCount(t, db, pageID, pipeline.DepKindMenu, "menu:"+projectID+":header"); n != 0 {
		t.Fatalf("按项引用的页面不应登记位置键，实际 %d 条", n)
	}
	markPageActivated(t, db, pageID)
	if navPageStale(t, db, pageID) {
		t.Fatalf("前置条件不成立：页面应为已收敛（stale=false）")
	}

	newTitle := "产品中心"
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, Title: &newTitle}); err != nil {
		t.Fatalf("更新菜单项失败: %v", err)
	}
	if !navPageStale(t, db, pageID) {
		t.Fatalf("菜单项改名后，按项引用它的页面未被标记待重建（依赖闭环断了）")
	}
}

// TestNavigationPanelBlockDependencyAndInvalidation 悬浮面板（超级菜单）：
// 面板内容渲染进产物、登记 block:{blockID}、改块后引用页面被标 stale。
func TestNavigationPanelBlockDependencyAndInvalidation(t *testing.T) {
	db, pages, navSvc, blocks, projectID := newNavPanelEnv(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	panelDoc := json.RawMessage("{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"pn1\",\"type\":\"core.heading\",\"props\":{\"text\":\"面板标记文本\",\"tag\":\"div\"}}]}")
	block, err := blocks.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: "页眉菜单·产品面板", Document: panelDoc})
	if err != nil {
		t.Fatalf("创建面板块失败: %v", err)
	}

	itemID := addNavItem(t, navSvc, projectID, "产品", "/products", nil, "self")
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, PanelBlockID: &block.ID}); err != nil {
		t.Fatalf("给菜单项挂面板失败: %v", err)
	}

	pageID := createNavPage(t, pages, projectID, "home", "/panel", navByItemDoc(itemID, "面板页面"))
	hash := buildNavPage(t, pages, pageID)

	if html := readStagedHTML(t, hash); !strings.Contains(html, "面板标记文本") {
		t.Fatalf("面板块内容未出现在产物中（面板渲染链断了）")
	}
	if n := navDepCount(t, db, pageID, pipeline.DepKindBlock, "block:"+block.ID); n != 1 {
		t.Fatalf("面板引用的块应登记恰好 1 条 block:{id} 依赖，实际 %d 条", n)
	}
	changed := json.RawMessage("{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"pn1\",\"type\":\"core.heading\",\"props\":{\"text\":\"面板改后文本\",\"tag\":\"div\"}}]}")
	if _, err := blocks.Update(ctx, &blockdto.UpdateReq{ID: block.ID, ExpectedVersion: uint64(block.Version), Name: "页眉菜单·产品面板", Document: changed}); err != nil {
		t.Fatalf("更新面板块失败: %v", err)
	}
	if !navPageStale(t, db, pageID) {
		t.Fatalf("修改面板块后，引用它的页面未被标记待重建（依赖闭环断了）")
	}
}

// TestNavigationPanelBlockIDUpdateContract 面板引用三态（未传保留 / 空串清 NULL / 非空写值）。
//
// 三态是同一条归一链上的三个分支，任何一支走错都不会报错、只表现为"面板没清掉"或
// "没改动的字段被清空"。空串清除尤其容易退化：归一函数把空白变成 nil，若调用方再把
// nil 当成"本次不改动"，显式清除就永久失效。
func TestNavigationPanelBlockIDUpdateContract(t *testing.T) {
	db, _, navSvc, blocks, projectID := newNavPanelEnv(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	first, err := blocks.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: "面板一",
		Document: json.RawMessage("{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"p1\",\"type\":\"core.heading\",\"props\":{\"text\":\"面板一\",\"tag\":\"div\"}}]}")})
	if err != nil {
		t.Fatalf("创建面板块失败: %v", err)
	}
	second, err := blocks.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: "面板二",
		Document: json.RawMessage("{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"p2\",\"type\":\"core.heading\",\"props\":{\"text\":\"面板二\",\"tag\":\"div\"}}]}")})
	if err != nil {
		t.Fatalf("创建第二个面板块失败: %v", err)
	}

	itemID := addNavItem(t, navSvc, projectID, "产品", "/products", nil, "self")
	// 未传 PanelBlockID（nil 指针）→ 保留原值。
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, PanelBlockID: &first.ID}); err != nil {
		t.Fatalf("挂面板失败: %v", err)
	}
	renamed := "产品中心"
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, Title: &renamed}); err != nil {
		t.Fatalf("未传 PanelBlockID 的更新失败: %v", err)
	}
	if got := navPanelBlockID(t, db, itemID); got == nil || *got != first.ID {
		t.Fatalf("未传 PanelBlockID 应保留原面板 %s，实际 %v", first.ID, got)
	}
	// 空串 → 归一为 NULL 落库（显式清除）。
	empty := "   "
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, PanelBlockID: &empty}); err != nil {
		t.Fatalf("清除面板失败: %v", err)
	}
	if got := navPanelBlockID(t, db, itemID); got != nil {
		t.Fatalf("空串应把面板清成 NULL，实际仍为 %q", *got)
	}
	// 非空 → 写入新值。
	if _, err := navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, PanelBlockID: &second.ID}); err != nil {
		t.Fatalf("改挂面板失败: %v", err)
	}
	if got := navPanelBlockID(t, db, itemID); got == nil || *got != second.ID {
		t.Fatalf("非空值应写入面板 %s，实际 %v", second.ID, got)
	}
}

// navPanelBlockID 直接读库取菜单项当前面板引用（nil = NULL 列）。
func navPanelBlockID(t *testing.T, db *gorm.DB, itemID string) *string {
	t.Helper()
	var row struct{ PanelBlockID *string }
	if err := db.Raw("SELECT panel_block_id FROM navigations WHERE id = ?", itemID).Scan(&row).Error; err != nil {
		t.Fatalf("读面板引用失败: %v", err)
	}
	return row.PanelBlockID
}

// TestNavigationMobileKindPositions 位置扩展为四个：移动端可用、两端数据独立、非法位置被拒。
func TestNavigationMobileKindPositions(t *testing.T) {
	_, _, navSvc, _, projectID := newNavPanelEnv(t)
	if navSvc == nil {
		return
	}
	ctx := context.Background()
	created, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "移动端菜单项", Path: "/m", Kind: "header_mobile",
	})
	if err != nil {
		t.Fatalf("移动端位置应被接受，实际报错: %v", err)
	}
	if created.Kind != "header_mobile" {
		t.Fatalf("位置应原样保留，实际 %q", created.Kind)
	}
	list, err := navSvc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header_mobile"})
	if err != nil {
		t.Fatalf("按移动端位置列举失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("移动端位置应有 1 条菜单项，实际 %d 条", len(list))
	}
	desktop, err := navSvc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header"})
	if err != nil {
		t.Fatalf("按桌面位置列举失败: %v", err)
	}
	if len(desktop) != 0 {
		t.Fatalf("桌面位置不应看到移动端的菜单项，实际 %d 条", len(desktop))
	}
	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "非法位置", Path: "/x", Kind: "sidebar",
	}); err == nil {
		t.Fatalf("非法位置 sidebar 应被拒绝")
	}
}
