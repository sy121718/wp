package unit

// navigation_translation_stale_test.go — 改公开站点导航的**译文**后，用了该导航的自动发布实例
// 必须被标 stale。
//
// 缺口（与刚修好的 menu 依赖同源）：导航译文写的是 sys_translation，保存成功后只标了
// 手工页面（i18n:content 全站标记）—— presentation 实例没有任何落点，会永远停在旧菜单
// 标签上，而线上与库里都看不出异常。修复口径与「改导航项」完全一致：派发
// menu:{projectID}:{kind}，由依赖扇出按依赖表反查受影响来源（键构造与扇出都在发布内核）。
//
// 两条断言成对：用了该导航位置的实例 stale=true；**没用**该位置的实例不受影响 ——
// 只断言前者时，一个「把全站实例都标一遍」的实现也能通过。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// presPlainDocument 不引用任何导航位置的模板文档（对照组用）。
const presPlainDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`

// TestNavigationTranslationSaveMarksInstancesStale 保存导航译文 → 绑该位置的实例 stale。
func TestNavigationTranslationSaveMarksInstancesStale(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	navSvc := newNavDispatch(t, f)
	createNavItem(t, f, navSvc, "首页", "header", "/")

	// 两套模板：一套绑页眉导航（受影响），一套完全不碰导航（对照）。
	navInst := createInstanceWithTemplate(t, f, "nav-tr-instance", "/nav-tr-instance",
		createNavTemplate(t, f, "绑导航模板", "header"))
	plainInst := createInstanceWithTemplate(t, f, "plain-tr-instance", "/plain-tr-instance",
		createPlainTemplate(t, f, "无导航模板"))

	// 前置：只有绑导航的实例登记了 menu:{pid}:header 依赖（对照实例一条都没有）。
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

	// 动作：保存导航译文（真实 handler + 真实写入器 + 真实导航服务）。
	saveNavTranslation(t, f, navSvc, "首页", "Home")

	// 断言：绑该导航位置的实例被标 stale；没用的那个纹丝不动。
	if _, stale := instancePointer(t, f, navInst); !stale {
		t.Fatal("保存导航译文后，绑该导航位置的自动发布实例应被标记待重建")
	}
	if _, stale := instancePointer(t, f, plainInst); stale {
		t.Fatal("未使用该导航位置的实例不该被标记待重建")
	}
}

// TestNavigationTranslationSaveMarksAllPositionsUsingTheText 同一段菜单文字同时用在页眉与
// 页脚时，**两个位置**的实例都要被标 stale。
//
// 工作台按原文去重（译文键是 (原文, 语境)，页眉页脚共用同一个键），所以提交的表单里只有
// 一行 —— 只看这一行的位置会漏掉另一个位置，表现为「页脚实例的菜单标签没被重建」。
func TestNavigationTranslationSaveMarksAllPositionsUsingTheText(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	navSvc := newNavDispatch(t, f)
	// 页眉与页脚用同一段文字；先建页眉，保证列表里留下的那一行属于 header。
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

// countMenuDependency 该实例登记的某条 menu 依赖条目数。
func countMenuDependency(t *testing.T, f *presFixture, instanceID, key string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies
		WHERE presentation_id = ? AND dependency_kind = ? AND dependency_key = ?`,
		instanceID, pipeline.DepKindMenu, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计导航依赖失败: %v", err)
	}
	return n
}

// newNavDispatch 导航服务 + 失效派发链（与真实装配同形），并把导航解析器注入实例侧。
func newNavDispatch(t *testing.T, f *presFixture) *navigationservice.Service {
	t.Helper()
	navSvc := navigationservice.NewService(navigationmodel.NewModel(f.db), nil)
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePresentation, f.pres)
	navSvc.SetMenuStaleDispatcher(pipeline.NewMenuStaleAdapter(fanout))
	f.pres.SetNavigationService(navSvc)
	return navSvc
}

// createNavItem 建一个导航项（标题即菜单文字）。
func createNavItem(t *testing.T, f *presFixture, svc navigationcontract.NavigationService, title, kind, path string) {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: f.projectID, Title: title, Path: path, Kind: kind,
	}); err != nil {
		t.Fatalf("创建导航项 %s/%s 失败: %v", kind, title, err)
	}
}

// createNavTemplate 建一套绑定指定导航位置的模板，返回模板 id。
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

// createPlainTemplate 建一套完全不碰导航的模板，返回模板 id。
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

// createInstanceWithTemplate 建内容实体 + 自动发布实例，返回实例 id。
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

// saveNavTranslation 走真实 handler 保存一条导航标签译文（表单只有一行）。
//
// navSvc 要具体类型：失效端口（InvalidateMenuLabels）是模块 Service 上的能力，
// 不在导航契约里 —— 与装配入口的断言同一口径。
func saveNavTranslation(t *testing.T, f *presFixture, navSvc *navigationservice.Service,
	sourceText, target string) {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	handle := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
	handle.SetContentWriter(i18n.NewContentWriter(f.db))
	handle.SetMenuInvalidator(navSvc)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.POST("/admin/navigations/translations/save", handle.SaveNavigationTranslations)

	form := url.Values{}
	form.Set("project", f.projectID)
	form.Set("lang", "en-US")
	form.Add("rowContext", i18n.ContentContext("navigation", "label"))
	form.Add("rowHash", i18n.ContentHash(sourceText))
	form.Add("rowTarget", target)
	req := httptest.NewRequest(http.MethodPost, "/admin/navigations/translations/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回工作台，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
}
