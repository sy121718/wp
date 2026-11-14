package navigationhttp

import (
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/middleware/adminctx"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

type pagePresentationNavigationStub struct {
	navigationcontract.NavigationService
	tree   []*navigationdto.NavigationNode
	groups []navigationcontract.SourceGroup
}

func (s *pagePresentationNavigationStub) Tree(_ context.Context, _, _ string) ([]*navigationdto.NavigationNode, error) {
	return s.tree, nil
}

func (s *pagePresentationNavigationStub) SourceGroups(_ context.Context, _ string) ([]navigationcontract.SourceGroup, error) {
	return s.groups, nil
}

type pagePresentationProjectStub struct {
	projectcontract.ProjectService
	listErr error
}

func (s *pagePresentationProjectStub) List(_ context.Context) ([]projectcontract.ProjectResp, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []projectcontract.ProjectResp{{ID: "project-1", Name: "示例工程"}}, nil
}

func TestNavigationsPageRowPresentationComesFromTemplate(t *testing.T) {
	svc := &pagePresentationNavigationStub{

		tree: []*navigationdto.NavigationNode{
			{ID: "row-self", Title: "首页", Path: "/", Target: "self"},
			{ID: "row-blank", Title: "新品", Path: "/new", Target: "blank", Children: []*navigationdto.NavigationNode{
				{ID: "row-child", Title: "子项", Path: "/new/a", Target: "blank"},
			}},
			{ID: "row-unknown", Title: "历史项", Path: "/legacy", Target: "_self"},
		},
		groups: []navigationcontract.SourceGroup{
			{Type: "page", Title: "admin.navigations.source.page", Items: []navigationcontract.SourceCandidate{
				{ID: "page-1", Title: "关于我们", URL: "/about"},
			}},
			{Type: "category", Title: "admin.navigations.source.category", Items: []navigationcontract.SourceCandidate{
				{ID: "cat-1", Title: "新闻", URL: "/news"},
			}},

			{Type: "block", Title: "admin.navigations.source.block", Items: []navigationcontract.SourceCandidate{
				{ID: "blk-1", Title: "页脚块", URL: "/block/1"},
			}},
		},
	}
	body := renderNavigationsPageBody(t, svc)

	for _, tc := range []struct{ rowID, title, path, wantLabel string }{
		{"row-self", "首页", "/", "当前窗口"},
		{"row-blank", "新品", "/new", "新标签页"},
		{"row-unknown", "历史项", "/legacy", "当前窗口"},
	} {
		row := rowHTML(t, body, tc.rowID)
		if !strings.Contains(row, "<td>"+tc.wantLabel+"</td>") {
			t.Errorf("行 %s 的打开方式列不是「%s」：%s", tc.rowID, tc.wantLabel, row)
		}

		want := `data-filter-text="` + tc.title + " " + tc.path + " " + tc.wantLabel + `"`
		if !strings.Contains(row, want) {
			t.Errorf("行 %s 的可筛文本未用列内同一份文案，期望含 %s：%s", tc.rowID, want, row)
		}
	}

	blankCell := "<td>" + optionLabel(t, body, "blank") + "</td>"
	if !strings.Contains(rowHTML(t, body, "row-blank"), blankCell) {
		t.Errorf("行内打开方式与抽屉下拉不同源：抽屉 blank=%q，行=%s",
			optionLabel(t, body, "blank"), rowHTML(t, body, "row-blank"))
	}

	child := rowHTML(t, body, "row-child")
	if !strings.Contains(child, `class="tree-cell" style="--tree-depth:1"`) {
		t.Errorf("子行未把层级放进 CSS 变量：%s", child)
	}
	if !strings.Contains(child, "↳ 子项") {
		t.Errorf("缩进之外仍需保留层级线索（↳ 前缀）：%s", child)
	}
	if strings.Contains(child, "padding-left") {
		t.Errorf("行内不再产出缩进像素（间距归 theme.css 的 .tree-cell）：%s", child)
	}
	if !strings.Contains(rowHTML(t, body, "row-self"), `class="tree-cell" style="--tree-depth:0"`) {
		t.Errorf("顶级行层级应为 0：%s", rowHTML(t, body, "row-self"))
	}

	for _, g := range []struct{ key, fallback string }{
		{"admin.navigations.source.page", "页面"},
		{"admin.navigations.source.category", "分类"},
		{"admin.navigations.source.block", "block"},
	} {
		want := i18n.Translate(g.key, g.fallback, "zh-CN") + "（1）"
		if !strings.Contains(body, want) {
			t.Errorf("来源分组标题未按 %s 取词，期望含 %q", g.key, want)
		}
	}
	if strings.Contains(body, "admin.navigations.source.") {
		t.Error("页面上漏出了分组标题的 i18n key")
	}
}

func newNavigationsPageTestRouter(t *testing.T,
	nav navigationcontract.NavigationService, projects projectcontract.ProjectService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	conf := viper.New()
	conf.Set("log.base_dir", "/tmp/navigation-page-presentation-test-logs")
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	store := cookie.NewStore([]byte("navigation-page-presentation-test-secret-32b"))
	router.Use(sessions.Sessions("nav-test", store))
	router.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("csrf_token", "test-csrf-token")
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}

		c.Set(adminctx.PermSetKey, map[string]bool{"navigation.update": true, "navigation.delete": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"navigation.update": true, "navigation.delete": true})
		c.Next()
	})
	h := NewNavigationPageHandle(nav, projects)
	router.GET("/admin/navigations", pageview.Slot(NavigationsPageTemplate, pageview.RenderPage), h.NavigationsPage)
	return router
}

func renderNavigationsPageBody(t *testing.T, nav navigationcontract.NavigationService) string {
	t.Helper()
	router := newNavigationsPageTestRouter(t, nav, &pagePresentationProjectStub{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/navigations?project=project-1&kind=header", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestNavigationsPageLoadFailureReturnsJSON(t *testing.T) {

	rawErr := errors.New(`pq: relation "navigations" does not exist`)

	for _, tc := range []struct {
		name       string
		acceptLang string
		entries    map[string]map[string]string
		wantMsg    string
	}{
		{
			name:       "zh-CN",
			acceptLang: "zh-CN",
			entries: map[string]map[string]string{
				"uiInternalErr": {"zh-CN": "操作失败，请稍后重试"},
			},
			wantMsg: "操作失败，请稍后重试",
		},
		{
			name:       "en-US",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				"uiInternalErr": {"en-US": "Operation failed, please try again later"},
			},
			wantMsg: "Operation failed, please try again later",
		},
		{

			name:       "词条缺席回落兜底",
			acceptLang: "en-US",
			entries:    nil,
			wantMsg:    response.InternalFallback,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(tc.entries, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newNavigationsPageTestRouter(t, &pagePresentationNavigationStub{},
				&pagePresentationProjectStub{listErr: rawErr})
			req := httptest.NewRequest(http.MethodGet, "/admin/navigations?project=project-1&kind=header", nil)
			req.Header.Set("Accept-Language", tc.acceptLang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("失败出口应为 JSON，得到 %q", ct)
			}

			for _, banned := range []string{"<!DOCTYPE", "</body>", "</html>", `id="adminRail"`} {
				if strings.Contains(body, banned) {
					t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
				}
			}
			if !strings.Contains(body, tc.wantMsg) {
				t.Errorf("失败响应应含归口文案 %q，实际：%s", tc.wantMsg, body)
			}

			if strings.Contains(body, rawErr.Error()) {
				t.Errorf("内部错误原文漏进了响应：%s", body)
			}
		})
	}
}

func TestNavigationsPageFacingErrorTextResolvesPerRequestLanguage(t *testing.T) {

	for _, tc := range []struct {
		name       string
		acceptLang string
		entries    map[string]map[string]string
		err        error
		want       string
		absent     []string
	}{
		{
			name:       "纯 key / 词条在场 zh-CN",
			acceptLang: "zh-CN",
			entries: map[string]map[string]string{
				navigationenums.ErrPathTaken: {"zh-CN": "同工程同类型下已存在相同路径的导航项", "en-US": "A navigation item with the same path already exists in this project and type."},
			},
			err:    errors.New(navigationenums.ErrPathTaken),
			want:   "同工程同类型下已存在相同路径的导航项",
			absent: []string{"MsgInternalError"},
		},
		{
			name:       "纯 key / 词条在场 en-US",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				navigationenums.ErrPathTaken: {"zh-CN": "同工程同类型下已存在相同路径的导航项", "en-US": "A navigation item with the same path already exists in this project and type."},
			},
			err:    errors.New(navigationenums.ErrPathTaken),
			want:   "A navigation item with the same path already exists in this project and type.",
			absent: []string{"MsgInternalError"},
		},
		{
			// 词条缺席时不能把裸 key 交给前端（AGENTS.md 取词规则），给归口文案。
			name:       "纯 key / 词条缺席 → 归口文案",
			acceptLang: "en-US",
			entries:    nil,
			err:        errors.New(navigationenums.ErrPathTaken),
			want:       response.InternalFallback,
			absent:     []string{navigationenums.ErrPathTaken, "uiInternalErr"},
		},
		{
			// 词条缺席时同样给归口文案；定位细节不上屏。
			name:       "key+定位 / 词条缺席 → 归口文案",
			acceptLang: "zh-CN",
			entries:    nil,
			err:        navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "首页"),

			want:   response.InternalFallback,
			absent: []string{navigationenums.ErrStaleVersion, "uiInternalErr"},
		},
		{
			name:       "key+定位 / 词条在场 en-US → 英文 + 半角定位",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				navigationenums.ErrStaleVersion: {"zh-CN": "该菜单项已被其他窗口修改，请刷新后重试", "en-US": "This menu item was changed in another window; refresh and try again"},
			},
			err: navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "首页"),

			want:   "This menu item was changed in another window; refresh and try again",
			absent: []string{"ErrStaleVersion"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(tc.entries, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newNavigationsPageTestRouter(t, &pagePresentationNavigationStub{},
				&pagePresentationProjectStub{listErr: tc.err})
			req := httptest.NewRequest(http.MethodGet, "/admin/navigations", nil)
			req.Header.Set("Accept-Language", tc.acceptLang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()

			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
				t.Fatalf("业务错误应给 400/409，得到 %d：%s", rec.Code, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("业务错误也该是 JSON，得到 %q", ct)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("页面应含 %q，实际：%s", tc.want, body)
			}
			for _, a := range tc.absent {
				if strings.Contains(body, a) {
					t.Errorf("页面上漏出了裸 key %q：%s", a, body)
				}
			}
		})
	}
}

func rowHTML(t *testing.T, body, rowID string) string {
	t.Helper()

	idx := strings.Index(body, `name="ids" value="`+rowID+`"`)
	if idx < 0 {
		t.Fatalf("页面里找不到行 %q：%s", rowID, body)
	}
	start := strings.LastIndex(body[:idx], "<tr data-filter-text=")
	if start < 0 {
		t.Fatalf("行 %q 之前没有行起点", rowID)
	}
	rel := strings.Index(body[idx:], "</tr>")
	if rel < 0 {
		t.Fatalf("行 %q 没有闭合", rowID)
	}
	end := idx + rel + len("</tr>")
	return body[start:end]
}

func optionLabel(t *testing.T, body, value string) string {
	t.Helper()
	open := `<option value="` + value + `">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("页面里找不到 value=%q 的下拉项，body=%s", value, body)
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</option>")
	if j < 0 {
		t.Fatalf("value=%q 的下拉项没有闭合", value)
	}
	return rest[:j]
}
