// Package unit navigation 模块 feature 测试（真实 PostgreSQL，0-C）：
// 导航项创建 → 列表排序、同 kind 同 path 冲突拒绝、Render HTML 转义与层级、
// 与后台权限菜单 sys_menus 严格隔离。
package unit

import (
	"context"
	"html"
	"strings"
	"testing"

	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"
)

// newNavigationService 建生产迁移库 + 装配 service（五端口真实实现）。
// 返回 service、projects（供建工程）、projectID、db（供直接 SQL 断言）。
func newNavigationService(t *testing.T) (*navigationservice.Service, *projectservice.Service, string, *gorm.DB) {
	t.Helper()
	// 建库走生产迁移（AutoMigrate 只建 projects / navigations 两张表，而写入口的
	// menuPages / menuInstances 端口要读写 pages、presentation_instances —— 精简库会撞
	// SQLSTATE 42P01 relation "pages" does not exist）。判据：端口是真实实现，就得给它
	// 真实的表；不手抄 CREATE TABLE（AGENTS.md 测试约定）。
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, "", nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	attachNavigationWritePorts(t, svc, db, projects)
	return svc, projects, project.ID, db
}

// attachNavigationWritePorts 注入菜单 CRUD 的同事务失效端口（五端口全齐）。
//
// 为什么必须用真实实现而不是空桩：service 侧 writePortsReady 是**全齐才放行**（缺任一即
// ErrTranslationWriteUnavailableError，也就是本文件此前四条用例报的
// navigation.err.translation.unavailable）。这个 fail closed 是对的 —— 缺端口时
// 「保存成功、线上停在旧菜单」是真实出现过的形态，所以修的是夹具不是生产判据。
//
// 而空桩只满足「放行」这一层，证明不了依赖键拼对、命中口径选对。这里装的是测试库里
// 的真实 page / presentation / build 服务，写入口照常往同一事务里落库：
//   - Pages / Items：page 模块按位置键与按项键标手工页面 stale；
//   - Instances / ItemInstances：presentation 模块标实例 stale 并推进 input_version
//     （两条独立命中口径：menu:{project}:{kind} 与 navigation:{itemID}）；
//   - Queue：build 模块写一条 menu_stale 任务。
//
// 本文件只断言导航项本身的读写与渲染（不查 stale 列与队列），真实端口的作用是让写入口
// 走它生产里的那条路径，而不是被一层直通桩改写。
func attachNavigationWritePorts(t *testing.T, svc *navigationservice.Service,
	db *gorm.DB, projects projectcontract.ProjectService) {
	t.Helper()
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
		nil, nil, nil, nil)
	instances := presentationservice.NewService(presentationmodel.NewModel(db),
		nil, nil, projects, blocks, routes)
	queue := buildservice.NewService(buildmodel.NewModel(db))
	svc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         pages,
		Instances:     instances,
		Items:         pages,
		ItemInstances: instances,
		Queue:         queue,
	})
}

// createNav 创建一条导航项并返回响应（测试失败即中止）。
func createNav(t *testing.T, svc *navigationservice.Service, req *navigationdto.CreateReq) *navigationdto.NavigationResp {
	t.Helper()
	res, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("创建导航项(%s)失败: %v", req.Title, err)
	}
	return res
}

// TestNavigationCreateListOrder 创建多条 → 列表可见且按 sort_order 升序。
func TestNavigationCreateListOrder(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 故意乱序创建，验证 List 按 sort_order 升序。
	createNav(t, svc, &navigationdto.CreateReq{ProjectID: projectID, Title: "关于我们", Path: "/about", Kind: "header", SortOrder: 30})
	createNav(t, svc, &navigationdto.CreateReq{ProjectID: projectID, Title: "首页", Path: "/", Kind: "header", SortOrder: 10})
	createNav(t, svc, &navigationdto.CreateReq{ProjectID: projectID, Title: "产品", Path: "/products", Kind: "header", SortOrder: 20})

	list, err := svc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header"})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("应列出 3 条导航: %d", len(list))
	}
	want := []string{"首页", "产品", "关于我们"}
	for i, item := range list {
		if item.Title != want[i] {
			t.Fatalf("第 %d 条应为 %q，got %q", i, want[i], item.Title)
		}
	}
}

// TestNavigationPathConflictRejected 同工程同 kind 同 path 冲突返回 ErrPathTaken。
func TestNavigationPathConflictRejected(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	createNav(t, svc, &navigationdto.CreateReq{ProjectID: projectID, Title: "首页", Path: "/home", Kind: "header"})

	_, err := svc.Create(ctx, &navigationdto.CreateReq{ProjectID: projectID, Title: "重复", Path: "/home", Kind: "header"})
	if err == nil || !strings.Contains(err.Error(), navigationenums.ErrPathTaken) {
		t.Fatalf("同 kind 同 path 应返回 ErrPathTaken: %v", err)
	}

	// 不同 kind 允许相同 path（隔离维度含 kind）。
	if _, err := svc.Create(ctx, &navigationdto.CreateReq{ProjectID: projectID, Title: "页脚首页", Path: "/home", Kind: "footer"}); err != nil {
		t.Fatalf("不同 kind 相同 path 应允许: %v", err)
	}
}

// TestNavigationRejectInvalidKind 非法 kind 拒绝。
func TestNavigationRejectInvalidKind(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: "非法", Path: "/x", Kind: "sidebar",
	}); err == nil || !strings.Contains(err.Error(), navigationenums.ErrInvalidKind) {
		t.Fatalf("非法 kind 应返回 ErrInvalidKind: %v", err)
	}
}

// TestNavigationRenderEscapesAndHierarchy Render 输出转义后的 title/path，并按 parent_id 嵌套。
func TestNavigationRenderEscapesAndHierarchy(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 根节点 title 含 HTML 标签、path 含 & 与引号，验证转义。
	root := createNav(t, svc, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "<b>产品</b>", Path: `/products&id=1"`, Kind: "header", SortOrder: 1,
	})
	// 子节点 title 含标签，验证嵌套 <ul><li> 与转义。
	createNav(t, svc, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "<i>子项</i>", Path: "/products/a", Kind: "header",
		ParentID: &root.ID, SortOrder: 1,
	})

	got, err := svc.Render(ctx, projectID, "header")
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	// title 转义：应含转义后的 <b>，不应含原始 <b> 标签。
	if !strings.Contains(got, html.EscapeString("<b>产品</b>")) {
		t.Fatalf("根节点 title 应转义: %s", got)
	}
	if strings.Contains(got, "<b>产品</b>") {
		t.Fatalf("根节点 title 不应出现未转义标签: %s", got)
	}
	// path 转义：& 与引号应被转义。
	if !strings.Contains(got, html.EscapeString(`/products&id=1"`)) {
		t.Fatalf("根节点 path 应转义: %s", got)
	}
	// 子节点嵌套 <ul><li> 且 title 转义。
	if !strings.Contains(got, "<ul><li>") {
		t.Fatalf("子节点应嵌套 <ul><li>: %s", got)
	}
	if !strings.Contains(got, html.EscapeString("<i>子项</i>")) {
		t.Fatalf("子节点 title 应转义: %s", got)
	}
	// 整体结构以 <nav> 包裹。
	if !strings.HasPrefix(got, "<nav>") || !strings.HasSuffix(got, "</nav>") {
		t.Fatalf("应包裹 <nav></nav>: %s", got)
	}
}

// TestNavigationMenuIsolation navigations 表独立于后台权限菜单 sys_menus。
func TestNavigationMenuIsolation(t *testing.T) {
	svc, _, projectID, db := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 实体绑定表名必须是 navigations，而非 sys_menus。
	if got := (navigationmodel.NavigationEntity{}).TableName(); got != "navigations" {
		t.Fatalf("导航实体应绑定 navigations 表: %q", got)
	}

	// 判据是**行为**而不是「表不存在」：迁移库本来就有 sys_menus（后台权限菜单），
	// 隔离的意思是导航读写一点都不碰它。把表临时改名，导航读写若真的引用它就会直接
	// 报 relation 不存在 —— 这比静态查表名强，也不依赖精简 schema。
	if err := db.Exec("ALTER TABLE sys_menus RENAME TO sys_menus_hidden_for_isolation").Error; err != nil {
		t.Fatalf("临时改名 sys_menus 失败: %v", err)
	}
	t.Cleanup(func() {
		if e := db.Exec("ALTER TABLE sys_menus_hidden_for_isolation RENAME TO sys_menus").Error; e != nil {
			t.Errorf("恢复 sys_menus 表名失败（后续用例会受影响）: %v", e)
		}
	})

	if _, err := svc.Create(ctx, &navigationdto.CreateReq{ProjectID: projectID, Title: "首页", Path: "/", Kind: "header"}); err != nil {
		t.Fatalf("sys_menus 不可见时创建导航应成功（说明写路径不依赖后台菜单）: %v", err)
	}
	if _, err := svc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header"}); err != nil {
		t.Fatalf("sys_menus 不可见时列出导航应成功: %v", err)
	}
}
