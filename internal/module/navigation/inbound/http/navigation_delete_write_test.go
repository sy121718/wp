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
// 成功走公共一句提示，失败回共享错误片段；空串/非法 uuid/超限都由 service 整批拒绝，
// 控制器不先过滤，避免非法项被静默丢掉。

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

// BulkDelete 复刻真实语义：任一失败整批拒绝。
func (s *navigationDeleteStub) BulkDelete(_ context.Context, req *navigationdto.BulkDeleteReq) error {
	s.calls = append(s.calls, req.IDs...)
	for _, id := range req.IDs {
		if err, ok := s.failFor[id]; ok {
			return err
		}
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
	if !strings.Contains(body, "操作成功") {
		t.Errorf("单条成功应走公共提示组件：%s", body)
	}
}

func TestNavigationDeleteBulkShape(t *testing.T) {
	stub := &navigationDeleteStub{failFor: map[string]error{"b": errors.New(navigationenums.ErrNotFound)}}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"a", "b"}})
	if rec.Code < 400 {
		t.Fatalf("任一条失败应整批拒绝（4xx），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染共享错误片段，实际 %s", body)
	}
	if strings.Contains(body, "<ul>") {
		t.Fatalf("失败明细不得渲染成列表（属业务数据），实际 %s", body)
	}
}

func TestNavigationDeleteBulkFailureIsOneMessage(t *testing.T) {
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
			if rec.Code < 400 {
				t.Fatalf("整批拒绝应为 4xx，实际 %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Fatalf("%s 请求的失败提示应为 %q，实际 %s", tc.lang, tc.want, body)
			}
			if strings.Contains(body, "<ul>") {
				t.Fatalf("失败提示不得包含逐条列表：%s", body)
			}
		})
	}
}

func TestNavigationDeleteBulkFailureNeverLeaksInternals(t *testing.T) {
	clearNavigationEntries(t)
	initNavigationDeleteLogger(t)

	for _, err := range []error{
		errors.New(navigationenums.ErrNotFound),
		errors.New(navigationenums.ErrPathTaken + "|/about"),
		errors.New(`SQLSTATE 42P01: relation "navigations" does not exist`),
	} {
		stub := &navigationDeleteStub{failFor: map[string]error{"bad": err}}
		engine := newNavigationDeleteRouter(t, stub)
		rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"bad"}})
		if rec.Code < 400 {
			t.Fatalf("%v 应整批拒绝，实际 %d", err, rec.Code)
		}
		body := rec.Body.String()
		for _, tok := range []string{"SQLSTATE", "42P01", "relation", "/about", "navigations"} {
			if strings.Contains(body, tok) {
				t.Errorf("失败提示不该出现 %q：%s", tok, body)
			}
		}
	}
}

// 控制器不再静默过滤：重复项与空串原样转交 service，由 service 按 uuid 身份
// 去重并对空标识整批拒绝。
func TestNavigationDeletePassesRawIDsToService(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {"a", "a", "b", ""}})
	body := navigationDeleteOK(t, rec)
	if got := strings.Join(stub.calls, ","); got != "a,a,b," {
		t.Fatalf("原始 ids（含重复与空串）必须整批转交 service，实际 %v", stub.calls)
	}
	if !strings.Contains(body, "操作成功") {
		t.Errorf("成功应走统一提示组件：%s", body)
	}
}

func TestNavigationDeleteEmptyValuesGoToService(t *testing.T) {
	stub := &navigationDeleteStub{failFor: map[string]error{"": errors.New(navigationenums.ErrInvalidParam)}}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": {""}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空标识应整批 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(stub.calls, ","); got != "" {
		t.Fatalf("空标识必须交给 service 判定，实际 %v", stub.calls)
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

func TestNavigationDeleteBulkTooManyIsRejected(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	stub := &navigationDeleteStub{failFor: map[string]error{
		"bulk": &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs},
	}}
	engine := newNavigationDeleteRouter(t, stub)

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "nav-"+strconv.Itoa(i))
	}
	ids[0] = "bulk"
	rec := postNavigationDeleteAt(t, engine, "/api/navigation/delete-bulk", url.Values{"ids": ids})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限提示应带上限与本次条数：%s", body)
	}
}
