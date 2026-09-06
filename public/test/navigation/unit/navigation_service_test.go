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

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// newNavigationService 隔离 PG schema + AutoMigrate projects/navigations + 装配 service。
// 返回 service、projects（供建工程）、projectID、db（供直接 SQL 断言）。
func newNavigationService(t *testing.T) (*navigationservice.Service, *projectservice.Service, string, *gorm.DB) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil, "", nil
	}
	if err := db.AutoMigrate(&projectmodel.ProjectEntity{}, &navigationmodel.NavigationEntity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	return navigationservice.NewService(navigationmodel.NewModel(db)), projects, project.ID, db
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

	// 本 schema 仅迁移 projects + navigations，无 sys_menus 表仍能完成导航读写。
	if _, err := svc.Create(ctx, &navigationdto.CreateReq{ProjectID: projectID, Title: "首页", Path: "/", Kind: "header"}); err != nil {
		t.Fatalf("无 sys_menus 表时创建导航应成功: %v", err)
	}
	if _, err := svc.List(ctx, &navigationdto.ListReq{ProjectID: projectID, Kind: "header"}); err != nil {
		t.Fatalf("无 sys_menus 表时列出导航应成功: %v", err)
	}

	// 确认 schema 中不存在 sys_menus 表（导航读写与之零关联）。
	var menuCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'sys_menus'`).Scan(&menuCount).Error; err != nil {
		t.Fatalf("查询 sys_menus 失败: %v", err)
	}
	if menuCount != 0 {
		t.Fatalf("测试 schema 不应包含 sys_menus 表，导航模块必须与后台菜单隔离")
	}
}
