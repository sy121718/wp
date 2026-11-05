package unit

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"
)

const presPlainDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`

func TestNavigationTranslationSaveMarksInstancesStale(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	navSvc := newNavDispatch(t, f)
	createNavItem(t, f, navSvc, "首页", "header", "/")

	navInst := createInstanceWithTemplate(t, f, "nav-tr-instance", "/nav-tr-instance",
		createNavTemplate(t, f, "绑导航模板", "header"))
	plainInst := createInstanceWithTemplate(t, f, "plain-tr-instance", "/plain-tr-instance",
		createPlainTemplate(t, f, "无导航模板"))

	menuKey := "menu:" + f.projectID + ":header"
	if n := countMenuDependency(t, f, navInst, menuKey); n != 1 {
		t.Fatalf("绑页眉导航的实例应登记 1 条 %s 依赖，实际 %d", menuKey, n)
	}
	if n := countMenuDependency(t, f, plainInst, menuKey); n != 0 {
		t.Fatalf("不绑导航的实例不该有 %s 依赖，实际 %d", menuKey, n)
	}
	if _, stale := instancePointer(t, f, navInst); stale {
		t.Fatal("保存译文之前实例不应是 stale（否则后面的断言证明不了任何事）")
	}

	saveNavTranslation(t, f, navSvc, "首页", "Home")

	if _, stale := instancePointer(t, f, navInst); !stale {
		t.Fatal("保存导航译文后，绑该导航位置的自动发布实例应被标记待重建")
	}
	if _, stale := instancePointer(t, f, plainInst); stale {
		t.Fatal("未使用该导航位置的实例不该被标记待重建")
	}
}

func TestNavigationTranslationSaveMarksAllPositionsUsingTheText(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	navSvc := newNavDispatch(t, f)

	createNavItem(t, f, navSvc, "首页", "header", "/")
	createNavItem(t, f, navSvc, "首页", "footer", "/home")

	headerInst := createInstanceWithTemplate(t, f, "nav-tr-header", "/nav-tr-header",
		createNavTemplate(t, f, "页眉导航模板", "header"))
	footerInst := createInstanceWithTemplate(t, f, "nav-tr-footer", "/nav-tr-footer",
		createNavTemplate(t, f, "页脚导航模板", "footer"))

	if n := countMenuDependency(t, f, footerInst, "menu:"+f.projectID+":footer"); n != 1 {
		t.Fatalf("页脚实例应登记 1 条页脚依赖，实际 %d", n)
	}

	saveNavTranslation(t, f, navSvc, "首页", "Home")

	if _, stale := instancePointer(t, f, headerInst); !stale {
		t.Fatal("页眉实例应被标记待重建")
	}
	if _, stale := instancePointer(t, f, footerInst); !stale {
		t.Fatal("同一段文字也用在页脚，页脚实例同样应被标记待重建（列表去重不该吞掉这个位置）")
	}
}

func countMenuDependency(t *testing.T, f *presFixture, instanceID, key string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies
		WHERE presentation_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		instanceID, publish.DepKindMenu, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计导航依赖失败: %v", err)
	}
	return n
}

func newNavDispatch(t *testing.T, f *presFixture) *navigationservice.Service {
	t.Helper()

	navSvc := navigationservice.NewService(navigationmodel.NewModel(f.db), nil)

	navSvc.SetWritePorts(navWriteDeps(t, f, navSvc))
	f.pres.SetNavigationService(navSvc)
	return navSvc
}

func navWriteDeps(t *testing.T, f *presFixture, navSvc *navigationservice.Service) navigationservice.NavigationWriteDeps {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(f.db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(f.db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(f.db), artifacts, f.routes, projects, blocks,
		nil, nil, navSvc, nil)
	return navigationservice.NavigationWriteDeps{
		Pages:         pages,
		Instances:     f.pres,
		Items:         pages,
		ItemInstances: f.pres,
		Queue:         buildservice.NewService(buildmodel.NewModel(f.db)),
	}
}

func createNavItem(t *testing.T, f *presFixture, svc navigationcontract.NavigationService, title, kind, path string) {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: f.projectID, Title: title, Path: path, Kind: kind,
	}); err != nil {
		t.Fatalf("创建导航项 %s/%s 失败: %v", kind, title, err)
	}
}

func createNavTemplate(t *testing.T, f *presFixture, name, kind string) string {
	t.Helper()
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav1","type":"core.nav","props":{"menu":"%s"}}]}`, kind)
	tpl, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "article", Name: name, DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建模板 %s 失败: %v", name, err)
	}
	return tpl.ID
}

func createPlainTemplate(t *testing.T, f *presFixture, name string) string {
	t.Helper()
	tpl, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "article", Name: name, DraftDocument: []byte(presPlainDocument),
	})
	if err != nil {
		t.Fatalf("创建模板 %s 失败: %v", name, err)
	}
	return tpl.ID
}

func createInstanceWithTemplate(t *testing.T, f *presFixture, slug, urlPath, templateID string) string {
	t.Helper()
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: slug, Data: map[string]any{"title": slug},
	})
	if err != nil {
		t.Fatalf("创建实体 %s 失败: %v", slug, err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: urlPath, TemplateID: templateID,
	})
	if err != nil {
		t.Fatalf("创建实例 %s 失败: %v", slug, err)
	}
	return inst.ID
}

func saveNavTranslation(t *testing.T, f *presFixture, navSvc *navigationservice.Service,
	sourceText, target string) {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))

	handle := navigationhttp.NewHandle(navSvc)
	wirePresentationTranslationWriter(t, f, handle, navSvc, projects)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", handle.Update)

	body := fmt.Sprintf(
		`{"action":"translations","project":%q,"lang":"en-US","rows":[{"context":%q,"hash":%q,"target":%q}]}`,
		f.projectID, i18n.ContentContext("navigation", "label"), i18n.ContentHash(sourceText), target)
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("保存译文应 200（JSON 写接口），实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Written int `json:"written"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("保存响应不是 JSON 信封: %v，body=%s", err, recorder.Body.String())
	}
	if envelope.Code != http.StatusOK || envelope.Data.Written == 0 {
		t.Fatalf("保存必须真的写入译文，实际 code=%d written=%d body=%s",
			envelope.Code, envelope.Data.Written, recorder.Body.String())
	}
}
