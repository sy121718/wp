package navigationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"

	"github.com/spf13/viper"
)

// 导航删除是标准表单：单条用 id，批量用重复的 ids。
// 成功回 Jet 片段（单条结论 / 批量计数 + 逐条原因），失败回错误片段。

type navigationDeleteStub struct {
	navigationcontract.NavigationService
	calls   []string
	failFor map[string]error
}

func (s *navigationDeleteStub) Delete(_ context.Context, req *navigationdto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if err, ok := s.failFor[req.ID]; ok {
		return err
	}
	return nil
}

func newNavigationDeleteRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := NewHandle(svc)
	engine.POST("/api/navigation/delete", pageview.HTMLFragment, h.Delete)
	engine.POST("/api/navigation/delete-bulk", pageview.HTMLFragment, h.DeleteBulk)
	return engine
}

func postNavigationDelete(t *testing.T, engine *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func postNavigationDeleteAt(t *testing.T, engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func postNavigationDeleteLangAt(t *testing.T, engine *gin.Engine, path, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func postNavigationDeleteLang(t *testing.T, engine *gin.Engine, lang string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func navigationDeleteOK(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200 + HTML 片段，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("应回 HTML 片段，实际 Content-Type=%q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段：%s", body)
	}
	return body
}

// bulkItemMessage 从批量结果片段里取某条失败项的原因文本。
func bulkItemMessage(t *testing.T, body, id string) string {
	t.Helper()
	marker := id + "："
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("片段里没有 id=%s 的逐条结果：%s", id, body)
	}
	rest := body[i+len(marker):]
	if end := strings.Index(rest, "<"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

func clearNavigationEntries(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func initNavigationDeleteLogger(t *testing.T) {
	t.Helper()
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
}

func TestNavigationDeleteSingleShape(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDelete(t, engine, url.Values{"id": {"row-1"}})
	body := navigationDeleteOK(t, rec)
	if got := stub.calls; len(got) != 1 || got[0] != "row-1" {
		t.Fatalf("service 应恰好被调用一次且带 row-1，实际 %v", got)
	}
	if !strings.Contains(body, "操作已完成") && !strings.Contains(body, "navDone") {
		t.Errorf("单条成功片段应给出结论：%s", body)
	}
}

func TestNavigationDeleteBulkShape(t *testing.T) {
	stub := &navigationDeleteStub{failFor: map[string]error{"b": errors.New(navigationenums.ErrNotFound)}}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"a", "b"}})
	body := navigationDeleteOK(t, rec)
	if got := strings.Join(stub.calls, ","); got != "a,b" {
		t.Fatalf("批档应按顺序逐条调用 service，实际 %v", stub.calls)
	}
	// 逐条失败原因必须在片段里出现（否则用户不知道哪条没删掉）。
	if got := bulkItemMessage(t, body, "b"); got == "" {
		t.Fatalf("批量片段应给出 b 的失败原因：%s", body)
	}
}

func TestNavigationDeleteBulkReasonsFollowRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrNotFound: {"zh-CN": "词条-导航项不存在", "en-US": "entry-navigation-item-missing"},
	})
	stub := &navigationDeleteStub{failFor: map[string]error{"b": errors.New(navigationenums.ErrNotFound)}}
	engine := newNavigationDeleteRouter(t, stub)

	for _, tc := range []struct{ name, lang, want string }{
		{"中文", "zh-CN", "词条-导航项不存在"},
		{"英文", "en-US", "entry-navigation-item-missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postNavigationDeleteLangAt(t, engine, "/api/navigation/delete-bulk", tc.lang, url.Values{"ids": {"a", "b"}})
			body := navigationDeleteOK(t, rec)
			if got := bulkItemMessage(t, body, "b"); got != tc.want {
				t.Fatalf("%s 请求的逐条原因应为 %q，实际 %q", tc.lang, tc.want, got)
			}
		})
	}
}

func TestNavigationDeleteBulkReasonsFallBackWhenEntryMissing(t *testing.T) {
	clearNavigationEntries(t)
	initNavigationDeleteLogger(t)

	const detail = "2026-10-09T00:00:00Z"
	cases := []struct {
		name     string
		err      error
		want     string
		wantGone []string
	}{
		{
			name:     "业务 key",
			err:      errors.New(navigationenums.ErrNotFound),
			want:     navigationInternalFallback,
			wantGone: []string{navigationenums.ErrNotFound},
		},
		{
			name:     "带参形态",
			err:      errors.New(navigationenums.ErrPathTaken + "|/about"),
			want:     navigationInternalFallback,
			wantGone: []string{navigationenums.ErrPathTaken, "|", "/about"},
		},
		{
			name:     "带定位形态",
			err:      navigationenums.ErrDetail(navigationenums.ErrStaleVersion, detail),
			want:     navigationenums.ErrStaleVersion + ": " + detail,
			wantGone: []string{"操作失败"},
		},
		{
			name:     "未归口的内部错误",
			err:      errors.New("SQLSTATE 42P01: relation \"navigations\" does not exist"),
			want:     navigationInternalFallback,
			wantGone: []string{"SQLSTATE", "42P01", "relation", "navigations"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationDeleteStub{failFor: map[string]error{"bad": tc.err}}
			engine := newNavigationDeleteRouter(t, stub)
			rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"bad"}})
			body := navigationDeleteOK(t, rec)
			got := bulkItemMessage(t, body, "bad")
			if got != tc.want {
				t.Fatalf("逐条原因应为 %q，实际 %q", tc.want, got)
			}
			for _, tok := range tc.wantGone {
				if strings.Contains(got, tok) {
					t.Errorf("逐条原因不该出现 %q：%q", tok, got)
				}
			}
		})
	}
}

func TestNavigationDeleteDedupesIDs(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"a", "a", "b", ""}})
	body := navigationDeleteOK(t, rec)
	if got := strings.Join(stub.calls, ","); got != "a,b" {
		t.Fatalf("空串应被丢掉、重复 id 只删一次，实际 %v", stub.calls)
	}
	if !strings.Contains(body, "2") {
		t.Errorf("批量片段应给出成功计数 2：%s", body)
	}
}

func TestNavigationDeleteEmptyIDListIsNoop(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {""}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 ids 应 400（没有可删的东西），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatalf("空 ids 不该调用 service，实际 %v", stub.calls)
	}
}

func TestNavigationDeleteShapeRejections(t *testing.T) {
	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"两个键都没有", "/api/navigation/delete", url.Values{}},
		{"id 是空串", "/api/navigation/delete", url.Values{"id": {""}}},
		{"ids 只有空串", "/api/navigation/delete-bulk", url.Values{"ids": {""}}},
		{"批量缺少 ids", "/api/navigation/delete-bulk", url.Values{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationDeleteStub{}
			engine := newNavigationDeleteRouter(t, stub)
			rec := postNavigationDeleteAt(t, engine, tc.path, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("形状错应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 0 {
				t.Fatalf("形状错的请求不该调用 service，实际 %v", stub.calls)
			}
			if !strings.Contains(rec.Body.String(), `data-ui-error`) {
				t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationDeleteBulkLimits(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "nav-"+strconv.Itoa(i))
	}
	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": ids})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限提示应带上限与本次条数：%s", body)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("超限不该逐条删除，实际 %v", stub.calls)
	}
}
