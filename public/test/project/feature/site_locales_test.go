package feature

// site_locales_test.go — 站点语言清单管理页（多语言 P3，docs/06-D §14 D10）。
//
// 走真实链路：gin 路由 → dashboard handler → project 契约（ListLocales / SaveLocales）
// → 真实 PostgreSQL（project_locales 表）→ Jet 模板片段。
//
// 覆盖：
//  1. 站点设置页「语言」分组渲染现有清单（默认/启用勾选态 + 禁用语言提示）；
//  2. HTMX 行片段增/删一行（服务端渲染，未落库）；
//  3. 全量保存成功（303 回跳 + 落库）；
//  4. 校验失败不落库：空清单 / 默认语言未启用 / 语言码非法。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// newSiteLocalesEnv 装配站点设置页测试环境（真实 PG + project 契约）。
// 返回 router、project 服务、工程 ID、db。
func newSiteLocalesEnv(t *testing.T) (*gin.Engine, *projectservice.Service, string, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "语言测试站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	handle := projecthttp.NewSiteSettingsAdminHandle(projects, nil, nil)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/settings", handle.SiteSettings)
	router.POST("/admin/settings/locales/rows", handle.LocaleRowsFragment)
	router.POST("/admin/settings/locales/save", handle.SaveSiteLocales)
	return router, projects, project.ID, db
}

// postForm 提交表单并返回响应。
func postForm(t *testing.T, router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	return recorder
}

// localeLangs 读取工程语言清单（语言码 + 默认/启用态，按契约顺序）。
func localeLangs(t *testing.T, svc *projectservice.Service, projectID string) []projectdto.LocaleResp {
	t.Helper()
	rows, err := svc.ListLocales(context.Background(), projectID)
	if err != nil {
		t.Fatalf("读取语言清单失败: %v", err)
	}
	return rows
}

// TestSiteSettingsRendersLocaleGroup 站点设置页渲染「语言」分组与禁用语言提示。
func TestSiteSettingsRendersLocaleGroup(t *testing.T) {
	router, projects, projectID, _ := newSiteLocalesEnv(t)
	ctx := context.Background()
	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/settings?project="+projectID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin/settings -> %d", recorder.Code)
	}
	body := recorder.Body.String()
	// 「语言」分组已从 <h2> 改成可折叠区块的标题（<details class="section-fold"> 里的
	// <span class="fold-title">）—— 站点设置是长页面，「站点语言清单」这类配置项折叠收纳，
	// 标题层级因此从 h2 变成 .fold-title；分组还是同一个分组。
	for _, want := range []string{
		`<span class="fold-title">语言</span>`,
		`action="/admin/settings/locales/save"`,
		`id="locale-rows"`,
		`name="langs" value="zh-CN"`,
		`name="langs" value="en-US"`,
		`name="defaultIndex" value="0" checked`,
		`name="enabledIndex" value="1" checked`,
		"已激活的站点路由不会自动清理",
		`hx-post="/admin/settings/locales/rows"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("设置页缺少 %q", want)
		}
	}
}

// TestLocaleRowsFragmentAddAndRemove HTMX 行片段：增一行 / 删一行（未落库）。
func TestLocaleRowsFragmentAddAndRemove(t *testing.T) {
	router, projects, projectID, _ := newSiteLocalesEnv(t)
	ctx := context.Background()
	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}

	// 增：newLang=ja 追加为新行（默认不勾选默认语言）。
	added := postForm(t, router, "/admin/settings/locales/rows", url.Values{
		"projectId": {projectID}, "action": {"add"}, "newLang": {"ja"},
		"langs": {"zh-CN", "en-US"}, "defaultIndex": {"0"}, "enabledIndex": {"0", "1"},
	})
	if added.Code != http.StatusOK {
		t.Fatalf("行片段(增) -> %d", added.Code)
	}
	if !strings.Contains(added.Body.String(), `name="langs" value="ja"`) {
		t.Fatalf("增行后片段缺少新语言\n%s", added.Body.String())
	}
	// 未落库：数据库仍是两行。
	if rows := localeLangs(t, projects, projectID); len(rows) != 2 {
		t.Fatalf("行片段不应落库，实际 %d 行", len(rows))
	}

	// 删：删除默认语言行（index=0）后，默认标记顺延给仍启用的第一行。
	removed := postForm(t, router, "/admin/settings/locales/rows", url.Values{
		"projectId": {projectID}, "action": {"remove"}, "removeIndex": {"0"},
		"langs": {"zh-CN", "en-US"}, "defaultIndex": {"0"}, "enabledIndex": {"0", "1"},
	})
	if removed.Code != http.StatusOK {
		t.Fatalf("行片段(删) -> %d", removed.Code)
	}
	body := removed.Body.String()
	if strings.Contains(body, `value="zh-CN"`) {
		t.Fatalf("删行后片段仍含被删语言\n%s", body)
	}
	if !strings.Contains(body, `name="langs" value="en-US"`) || !strings.Contains(body, `name="defaultIndex" value="0" checked`) {
		t.Fatalf("删默认语言后默认标记应顺延\n%s", body)
	}
}

// TestSaveSiteLocalesPersists 全量保存：303 回跳 + 落库 + 回读一致。
func TestSaveSiteLocalesPersists(t *testing.T) {
	router, projects, projectID, _ := newSiteLocalesEnv(t)

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US", "ja"},
		"defaultIndex": {"0"},
		"enabledIndex": {"0", "1"}, // ja 禁用
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	if loc := saved.Header().Get("Location"); !strings.Contains(loc, "locales_saved=1") {
		t.Fatalf("回跳地址应带保存标记，实际 %q", loc)
	}

	rows := localeLangs(t, projects, projectID)
	if len(rows) != 3 {
		t.Fatalf("应落库 3 种语言，实际 %d", len(rows))
	}
	if rows[0].Lang != "zh-CN" || !rows[0].IsDefault || !rows[0].Enabled {
		t.Fatalf("默认语言应为启用的 zh-CN：%+v", rows[0])
	}
	if rows[2].Lang != "ja" || rows[2].Enabled {
		t.Fatalf("ja 应为禁用状态：%+v", rows[2])
	}

	// 页面回显保存成功提示。
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/settings?project="+projectID+"&locales_saved=1", nil))
	if !strings.Contains(recorder.Body.String(), "语言清单已保存") {
		t.Fatal("回跳页应显示保存成功提示")
	}
}

// TestSaveSiteLocalesValidation 校验失败不落库：空清单 / 默认语言未启用 / 语言码非法。
func TestSaveSiteLocalesValidation(t *testing.T) {
	router, projects, projectID, _ := newSiteLocalesEnv(t)
	ctx := context.Background()
	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}

	cases := []struct {
		name string
		form url.Values
	}{
		{"空清单", url.Values{"projectId": {projectID}}},
		{"默认语言未启用", url.Values{
			"projectId": {projectID}, "langs": {"zh-CN", "en-US"}, "defaultIndex": {"1"},
		}},
		{"语言码非法", url.Values{
			"projectId": {projectID}, "langs": {"zh CN"}, "defaultIndex": {"0"}, "enabledIndex": {"0"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := postForm(t, router, "/admin/settings/locales/save", c.form)
			if recorder.Code != http.StatusOK {
				t.Fatalf("校验失败应回渲染设置页（200），实际 %d", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), "语言清单不合法") {
				t.Fatalf("缺少校验失败提示\n%s", recorder.Body.String())
			}
			// 落库内容保持不变（仍是初始化的一种语言）。
			rows := localeLangs(t, projects, projectID)
			if len(rows) != 1 || rows[0].Lang != "zh-CN" {
				t.Fatalf("校验失败不应落库，实际 %+v", rows)
			}
		})
	}
}
