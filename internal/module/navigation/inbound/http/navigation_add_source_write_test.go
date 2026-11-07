package navigationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationservice "go_wp/internal/module/navigation/service"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// 来源加入是「整批一次调用」：控制器只解析形状并调用 service.AddSources，
// 成功/失败都走公共出口；空标识、非法 uuid、超限、候选规则、路径查重与事务都在服务层。
// 控制器不先 Normalize：否则「混合空 ID」会在到达业务规则前被静默丢掉。

type navigationAddSourceStub struct {
	navigationcontract.NavigationService
	calls   []*navigationdto.AddSourceReq
	addErr  error
	addDone int
}

func (s *navigationAddSourceStub) AddSources(_ context.Context, req *navigationdto.AddSourceReq) (int, error) {
	s.calls = append(s.calls, req)
	if s.addErr != nil {
		return 0, s.addErr
	}
	return s.addDone, nil
}

func newNavigationAddSourceRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/navigation/sources", pageview.HTMLFragment, NewHandle(svc).CreateFromSource)
	return engine
}

func postNavigationAddSource(t *testing.T, engine *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/sources", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func addSourceForm(ids ...string) url.Values {
	form := url.Values{}
	form.Set("projectId", "p1")
	form.Set("kind", "header")
	form.Set("sourceType", "page")
	for _, id := range ids {
		form.Add("ids", id)
	}
	return form
}

func TestNavigationAddSourceCallsServiceOnceWithRawIDs(t *testing.T) {
	stub := &navigationAddSourceStub{addDone: 2}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1", "pg-2", "pg-2", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("来源批量应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("应回 HTML 片段，实际 Content-Type=%q", ct)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("整批必须一次调用 service，实际 %d 次", len(stub.calls))
	}
	req := stub.calls[0]
	if req.ProjectID != "p1" || req.Kind != "header" || req.SourceType != "page" {
		t.Fatalf("作用域字段应原样转交，实际 %+v", req)
	}
	// 去重、空标识拒绝都由 service 判定；控制器不能先把空串/重复项过滤掉。
	if got := strings.Join(req.IDs, ","); got != "pg-1,pg-2,pg-2," {
		t.Fatalf("id 必须原样整批转交（含空串与重复项），实际 %q", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `role="status"`) || strings.Contains(body, "data-ui-error") {
		t.Fatalf("成功应走公共提示组件，实际：%s", body)
	}
}

func TestNavigationAddSourceMultiRejectionUsesBatchMessage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		navigationenums.ErrBatchReject: {"zh-CN": "整批-未执行", "en-US": "BATCH-REJECT-EN"},
		navigationenums.ErrNotFound:    {"zh-CN": "词条-不存在", "en-US": "ENTRY-NOT-FOUND"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	items := []facing.BlockedItem{
		{ID: "pg-2", Name: "草稿页", ReasonKey: navigationenums.NoPublicPath},
		{ID: "nope", ReasonKey: navigationenums.ErrNotFound},
	}
	stub := &navigationAddSourceStub{addErr: navigationservice.RejectBatch(3, items)}
	engine := newNavigationAddSourceRouter(t, stub)

	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "整批-未执行"},
		{"en-US", "BATCH-REJECT-EN"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/navigation/sources",
			strings.NewReader(addSourceForm("pg-1", "pg-2", "nope").Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", tc.lang)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s：多项拒绝含缺失项应 404，实际 %d", tc.lang, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "data-ui-error") {
			t.Fatalf("%s：失败应渲染共享错误片段：%s", tc.lang, body)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s：多项请求的主消息应是公共整批文案 %q，实际 %s", tc.lang, tc.want, body)
		}
		if strings.Contains(body, "<ul>") || strings.Contains(body, "pg-2") || strings.Contains(body, "nope") {
			t.Fatalf("%s：失败区不逐条列明细（明细只进 data.items）：%s", tc.lang, body)
		}
	}
	if len(stub.calls) != 2 {
		t.Fatalf("每次请求只调用一次 service，实际 %d", len(stub.calls))
	}
}

func TestNavigationAddSourceSingleRejectionUsesSpecificReason(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		navigationenums.ErrBatchReject: {"zh-CN": "整批-未执行", "en-US": "BATCH-REJECT-EN"},
		navigationenums.ErrPathTaken:   {"zh-CN": "词条-路径已存在", "en-US": "ENTRY-PATH-TAKEN"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	stub := &navigationAddSourceStub{addErr: navigationservice.RejectBatch(1, []facing.BlockedItem{
		{ID: "pg-1", Name: "A 页", ReasonKey: navigationenums.ErrPathTaken},
	})}
	engine := newNavigationAddSourceRouter(t, stub)

	// 合法重复（同一 uuid 两种写法）去重后仍是单条请求：主消息必须是该条具体原因。
	form := addSourceForm("aaaaaaaa-1111-4a11-8a11-111111111111",
		strings.ToUpper("aaaaaaaa-1111-4a11-8a11-111111111111"))
	rec := postNavigationAddSource(t, engine, form)
	if rec.Code != http.StatusConflict {
		t.Fatalf("单条路径冲突应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "词条-路径已存在") || strings.Contains(body, "整批-未执行") {
		t.Fatalf("单条请求应给该条具体原因而不是公共整批文案：%s", body)
	}
}

func TestNavigationAddSourceKindIsPassedThrough(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want string
	}{
		{"显式 footer", "footer", "footer"},
		{"非法类型不静默回落", "sidebar", "sidebar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{addDone: 1}
			engine := newNavigationAddSourceRouter(t, stub)
			form := addSourceForm("pg-1")
			form.Set("kind", tc.kind)
			rec := postNavigationAddSource(t, engine, form)
			if rec.Code != http.StatusOK {
				t.Fatalf("应 200（校验归 service），实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 1 || stub.calls[0].Kind != tc.want {
				t.Fatalf("kind 应为 %q 原样转交，实际 %+v", tc.want, stub.calls)
			}
		})
	}
}

func TestNavigationAddSourceInternalErrorIsOpaque(t *testing.T) {
	stub := &navigationAddSourceStub{addErr: errors.New(`relation "navigations" does not exist (SQLSTATE 42P01)`)}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("内部错误应 500，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", body)
	}
	for _, leak := range []string{"navigations", "SQLSTATE", "42P01"} {
		if strings.Contains(body, leak) {
			t.Fatalf("500 回执不该带原始异常（%q）：%s", leak, body)
		}
	}
}

func TestNavigationAddSourceShapeRejections(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"缺 projectId", url.Values{"kind": {"header"}, "sourceType": {"page"}, "ids": {"pg-1"}}},
		{"projectId 空串", url.Values{"projectId": {""}, "kind": {"header"}, "sourceType": {"page"}, "ids": {"pg-1"}}},
		{"缺 kind", url.Values{"projectId": {"p1"}, "sourceType": {"page"}, "ids": {"pg-1"}}},
		{"kind 空串", url.Values{"projectId": {"p1"}, "kind": {""}, "sourceType": {"page"}, "ids": {"pg-1"}}},
		{"缺 sourceType", url.Values{"projectId": {"p1"}, "kind": {"header"}, "ids": {"pg-1"}}},
		{"sourceType 空串", url.Values{"projectId": {"p1"}, "kind": {"header"}, "sourceType": {""}, "ids": {"pg-1"}}},
		{"缺 ids", url.Values{"projectId": {"p1"}, "kind": {"header"}, "sourceType": {"page"}}},
		{"空体", url.Values{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{}
			engine := newNavigationAddSourceRouter(t, stub)
			rec := postNavigationAddSource(t, engine, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("形状错应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 0 {
				t.Fatalf("形状错不该调用 service，实际 %+v", stub.calls)
			}
			if !strings.Contains(rec.Body.String(), `data-ui-error`) {
				t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationAddSourceTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	router := readFile("navigation_page_router.go")
	if strings.Contains(router, "add-source") {
		t.Fatalf("页面 add-source 路由应已退役：%s", router)
	}
	page := readFile("navigation_page_handle.go")
	if strings.Contains(page, "func (h *navigationPageHandle) NavigationAddSource") {
		t.Fatalf("NavigationAddSource 页面 handler 应已退役")
	}
	tpl := readFile("../../../../templates/admin/navigation/navigations.html")
	for _, want := range []string{
		`hx-post="/api/navigation/sources"`,
		`name="sourceType"`,
		`name="ids"`,
		`data-write-status`,
		`data-write-result`,
	} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("模板应含 %q", want)
		}
	}

	for _, forbidden := range []string{
		`action="/admin/navigations/add-source`,
		`name="sourceIds"`,
	} {
		if strings.Contains(tpl, forbidden) {
			t.Fatalf("模板不该再出现 %q", forbidden)
		}
	}
}
