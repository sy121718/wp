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

func newNavigationService(t *testing.T) (*navigationservice.Service, *projectservice.Service, string, *gorm.DB) {
	t.Helper()

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

func createNav(t *testing.T, svc *navigationservice.Service, req *navigationdto.CreateReq) *navigationdto.NavigationResp {
	t.Helper()
	res, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("创建导航项(%s)失败: %v", req.Title, err)
	}
	return res
}

func TestNavigationCreateListOrder(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

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

	if _, err := svc.Create(ctx, &navigationdto.CreateReq{ProjectID: projectID, Title: "页脚首页", Path: "/home", Kind: "footer"}); err != nil {
		t.Fatalf("不同 kind 相同 path 应允许: %v", err)
	}
}

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

func TestNavigationRenderEscapesAndHierarchy(t *testing.T) {
	svc, _, projectID, _ := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	root := createNav(t, svc, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "<b>产品</b>", Path: `/products&id=1"`, Kind: "header", SortOrder: 1,
	})

	createNav(t, svc, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "<i>子项</i>", Path: "/products/a", Kind: "header",
		ParentID: &root.ID, SortOrder: 1,
	})

	got, err := svc.Render(ctx, projectID, "header")
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	if !strings.Contains(got, html.EscapeString("<b>产品</b>")) {
		t.Fatalf("根节点 title 应转义: %s", got)
	}
	if strings.Contains(got, "<b>产品</b>") {
		t.Fatalf("根节点 title 不应出现未转义标签: %s", got)
	}

	if !strings.Contains(got, html.EscapeString(`/products&id=1"`)) {
		t.Fatalf("根节点 path 应转义: %s", got)
	}

	if !strings.Contains(got, "<ul><li>") {
		t.Fatalf("子节点应嵌套 <ul><li>: %s", got)
	}
	if !strings.Contains(got, html.EscapeString("<i>子项</i>")) {
		t.Fatalf("子节点 title 应转义: %s", got)
	}

	if !strings.HasPrefix(got, "<nav>") || !strings.HasSuffix(got, "</nav>") {
		t.Fatalf("应包裹 <nav></nav>: %s", got)
	}
}

func TestNavigationMenuIsolation(t *testing.T) {
	svc, _, projectID, db := newNavigationService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if got := (navigationmodel.NavigationEntity{}).TableName(); got != "navigations" {
		t.Fatalf("导航实体应绑定 navigations 表: %q", got)
	}

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
