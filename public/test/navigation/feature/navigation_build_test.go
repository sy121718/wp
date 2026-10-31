package feature

import (
	"context"
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
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

const navDocument = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"导航页"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"header"}}]}`

const customNavDocument = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"手写导航页"}},"root":[{"id":"nav1","type":"core.nav","props":{"items":[{"label":"关于我们","url":"/about"}]}}]}`

func newNavigationEnv(t *testing.T) (*gorm.DB, pagecontract.PageService, navigationcontract.NavigationService, string) {
	t.Helper()
	db, pages, navSvc, projectID, _ := newNavigationEnvPorts(t)
	return db, pages, navSvc, projectID
}

func newNavigationEnvPorts(t *testing.T) (*gorm.DB, pagecontract.PageService, navigationcontract.NavigationService, string, navWritePortFixture) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "导航测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	navSvc := navigationservice.NewService(navigationmodel.NewModel(db), projects)

	ports := attachNavWritePorts(t, navSvc, db, projects)
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, navSvc, nil)
	return db, pages, navSvc, project.ID, ports
}

func addNavItem(t *testing.T, svc navigationcontract.NavigationService, projectID, title, path string, parentID *string, target string) string {
	t.Helper()
	res, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: "header",
		ParentID: parentID, Target: target, SortOrder: 0,
	})
	if err != nil {
		t.Fatalf("创建导航项 %s 失败: %v", title, err)
	}
	return res.ID
}

func TestNavigationMenuCompiledIntoArtifact(t *testing.T) {
	_, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()

	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")
	parentID := addNavItem(t, navSvc, projectID, "产品", "/products", nil, "self")
	addNavItem(t, navSvc, projectID, "新品", "/new", &parentID, "blank")

	html, err := pages.CompilePreview(ctx, []byte(navDocument), projectID, "/", "", false)
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	out := string(html)

	for _, want := range []string{`>首页<`, `href="/"`, `href="/products"`, `>新品<`, `target="_blank"`} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestNavigationMenuIsolatedByProject(t *testing.T) {
	_, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	otherProjectID := "00000000-0000-0000-0000-0000000000ff"
	html, err := pages.CompilePreview(ctx, []byte(navDocument), otherProjectID, "/", "", false)
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	if strings.Contains(string(html), ">首页<") {
		t.Errorf("跨工程菜单泄漏\n%s", html)
	}
}

func TestNavigationCustomItemsUntouched(t *testing.T) {
	_, pages, _, projectID := newNavigationEnv(t)
	html, err := pages.CompilePreview(context.Background(), []byte(customNavDocument), projectID, "", "", false)
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	if !strings.Contains(string(html), ">关于我们<") {
		t.Errorf("手写菜单项未渲染\n%s", html)
	}
}

func TestNavigationSourceFieldsPersisted(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	sourceID := "00000000-0000-0000-0000-0000000000ab"
	res, err := navSvc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: "落地页", Path: "/landing", Kind: "header",
		SourceType: "page", SourceID: &sourceID, Target: "blank",
	})
	if err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}
	if res.SourceType != "page" || res.Target != "blank" || res.SourceID == nil || *res.SourceID != sourceID {
		t.Fatalf("响应字段不符: %+v", res)
	}
	var row struct {
		SourceType string `gorm:"column:source_type"`
		Target     string `gorm:"column:target"`
	}
	if err = db.Table("navigations").Where("id = ?", res.ID).Take(&row).Error; err != nil {
		t.Fatalf("查询导航项失败: %v", err)
	}
	if row.SourceType != "page" || row.Target != "blank" {
		t.Fatalf("落库字段不符: %+v", row)
	}

	if _, err = navSvc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: "坏来源", Path: "/bad", Kind: "header", SourceType: "sql",
	}); err == nil {
		t.Fatal("非法来源应被拒绝")
	}
}
