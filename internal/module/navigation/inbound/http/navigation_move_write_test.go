package navigationhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type moveWriteNavigationStub struct {
	navigationcontract.NavigationService
	calls       []*navigationdto.SwapMoveReq
	updateCalls []*navigationdto.UpdateReq
	resp        *navigationdto.SwapMoveResp
	err         error
}

func (s *moveWriteNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.updateCalls = append(s.updateCalls, req)
	return &navigationdto.NavigationResp{ID: req.ID}, nil
}

func (s *moveWriteNavigationStub) SwapMove(_ context.Context, req *navigationdto.SwapMoveReq) (*navigationdto.SwapMoveResp, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

func initMoveWriteLogger(t *testing.T) {
	t.Helper()
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
}

func newMoveWriteRouter(svc navigationcontract.NavigationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/api/navigation/move", pageview.HTMLFragment, NewHandle(svc).Move)
	return router
}

func postMoveWrite(t *testing.T, engine *gin.Engine, lang, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/move", strings.NewReader(moveFormOf(body)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func navigationMoveResponse(t *testing.T, rec *httptest.ResponseRecorder) (int, string, json.RawMessage) {
	t.Helper()
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, rec.Body.String())
	}
	return resp.Code, resp.Message, resp.Data
}

// moveFormOf 把 JSON 形态的测试输入转成标准表单编码：
// 换位端点只接受表单，测试沿用同一组字段名即可。
func moveFormOf(body string) string {
	var fields map[string]string
	_ = json.Unmarshal([]byte(body), &fields)
	form := url.Values{}
	for k, v := range fields {
		if k == "action" {
			continue
		}
		form.Set(k, v)
	}
	return form.Encode()
}

func moveBody(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(fields))
	for _, k := range keys {
		parts = append(parts, `"`+k+`":"`+fields[k]+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestNavigationMoveShapeIsStrict(t *testing.T) {
	// 表单编码下所有值都是字符串：形状错误只剩「必填字段缺失或为空」。
	cases := []struct {
		name string
		body string
	}{
		{"缺 id 与 dir", `{}`},
		{"缺 dir", `{"id":"nav-1"}`},
		{"空 id", `{"id":"","dir":"up"}`},
		{"空 dir", `{"id":"nav-1","dir":""}`},
		{"缺 id", `{"dir":"up"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &moveWriteNavigationStub{}
			rec := postMoveWrite(t, newMoveWriteRouter(stub), "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 0 {
				t.Fatalf("形状不对不得落 service，实际 %+v", stub.calls)
			}
			if !strings.Contains(rec.Body.String(), `data-ui-error`) {
				t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationUpdateMoveCallsSwapMoveOnce(t *testing.T) {
	stub := &moveWriteNavigationStub{resp: &navigationdto.SwapMoveResp{Moved: true}}
	rec := postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{
		"action": "move", "id": "nav-9", "projectId": "p-1", "dir": "down",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 1 {
		t.Fatalf("应恰好一次 SwapMove，实际 %d 次", len(stub.calls))
	}
	got := stub.calls[0]
	if got.ID != "nav-9" || got.ProjectID != "p-1" || got.Dir != "down" {
		t.Fatalf("请求字段被改动：%+v", got)
	}
	// 成功回 Jet 片段（不再是 JSON 回执）；交换已由 stub 断言。
	if body := rec.Body.String(); !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段，实际 %s", body)
	}

	stub.calls = nil
	rec = postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{"action": "move", "id": "nav-9", "dir": "up"}))
	if rec.Code != http.StatusOK || len(stub.calls) != 1 || stub.calls[0].ProjectID != "" {
		t.Fatalf("缺省 projectId 应放行且落到空工程：code=%d calls=%+v", rec.Code, stub.calls)
	}
}

func TestNavigationUpdateMoveErrorStatus(t *testing.T) {
	initMoveWriteLogger(t)
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"未找到", errors.New(navigationenums.ErrNotFound), http.StatusNotFound},
		{"版本冲突", errors.New(navigationenums.ErrStaleVersion), http.StatusConflict},
		{"参数非法", errors.New(navigationenums.ErrInvalidParam), http.StatusBadRequest},
		{"同级排序并列", errors.New(navigationenums.ErrSortTied), http.StatusBadRequest},
		{"未归口的内部错误", errors.New("boom: SQLSTATE 42P01"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &moveWriteNavigationStub{err: tc.err}
			rec := postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{
				"action": "move", "id": "nav-1", "dir": "up",
			}))
			if rec.Code != tc.wantCode {
				t.Fatalf("应 %d，实际 %d：%s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "SQLSTATE") || strings.Contains(rec.Body.String(), "42P01") {
				t.Fatalf("服务器细节不得进响应体：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationUpdateMoveMessageFollowsRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrSortTied: {"zh-CN": "词条-同级排序并列", "en-US": "entry-sort-tied"},
	})
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "词条-同级排序并列"},
		{"en-US", "entry-sort-tied"},
	} {
		stub := &moveWriteNavigationStub{err: errors.New(navigationenums.ErrSortTied)}
		rec := postMoveWrite(t, newMoveWriteRouter(stub), tc.lang, moveBody(map[string]string{
			"action": "move", "id": "nav-1", "dir": "up",
		}))
		// 失败回 Jet 错误片段：按请求语言取的词条必须出现在片段里。
		body := rec.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s 的错误片段应带 %q，实际 %s", tc.lang, tc.want, body)
		}
		if strings.Contains(body, "navigation.err.") {
			t.Fatalf("%s 的错误片段漏出了声明 key：%s", tc.lang, body)
		}
	}
}

var moveTemplateFormRe = regexp.MustCompile(`(?s)<form[^>]*hx-post="/api/navigation/move".*?</form>`)

var moveTemplateNameRe = regexp.MustCompile(`name="([^"]+)"`)

func TestNavigationUpdateMoveTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	router := readFile("navigation_page_router.go")
	if strings.Contains(router, `/navigations/move`) {
		t.Fatalf("页面 move 路由应已退役（净删 1）：%s", router)
	}
	if strings.Contains(readFile("navigation_page_handle.go"), "func (h *navigationPageHandle) NavigationMove") {
		t.Fatalf("NavigationMove 页面 handler 应已退役")
	}

	tpl := readFile("../../../../templates/admin/navigation/navigations.html")
	for _, want := range []string{
		`hx-post="/api/navigation/move"`,
		`data-write`,
		`name="dir"`,
		`name="dir" value="up"`,
		`name="dir" value="down"`,
		`data-write-status`,

		`<span data-requires-js hidden><button type="submit" form="nav-up-`,
		`<span data-requires-js hidden><button type="submit" form="nav-down-`,
	} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("模板应含 %q", want)
		}
	}
	for _, forbidden := range []string{
		`action="/admin/navigations/move`,
		`name="csrf_token" value="{{csrf}}"`,
		`{{csrf := .["csrf_token"]}}`,
	} {
		if strings.Contains(tpl, forbidden) {
			t.Fatalf("模板不该再出现 %q", forbidden)
		}
	}

	if !strings.Contains(tpl, `admin.navigations.write.noJs`) {
		t.Fatalf("页面级 noscript 应复用既有词条 admin.navigations.write.noJs（缺口已在模板注释登记）")
	}

	forms := moveTemplateFormRe.FindAllString(tpl, -1)
	if len(forms) != 2 {
		t.Fatalf("应恰好两个声明了本端点的行内表单（up / down），实际 %d 个", len(forms))
	}
	want := make([]string, 0, 3)
	for _, key := range []string{"id", "dir", "projectId"} {
		want = append(want, key)
	}
	sort.Strings(want)
	for i, form := range forms {
		got := moveTemplateNameRe.FindAllStringSubmatch(form, -1)
		names := make([]string, 0, len(got))
		for _, m := range got {
			names = append(names, m[1])
		}
		sort.Strings(names)
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("第 %d 个 move 表单的具名字段应为 %v（服务端白名单），实际 %v", i+1, want, names)
		}
	}
}
