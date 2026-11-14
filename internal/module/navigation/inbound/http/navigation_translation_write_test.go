package navigationhttp

import (
	"context"
	"errors"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

type translationWriteNavigationStub struct {
	navigationcontract.NavigationService
	calls     []*navigationdto.UpdateReq
	moveCalls []*navigationdto.SwapMoveReq
}

func (s *translationWriteNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.calls = append(s.calls, req)
	return &navigationdto.NavigationResp{ID: req.ID, Title: "示例"}, nil
}

func (s *translationWriteNavigationStub) SwapMove(_ context.Context, req *navigationdto.SwapMoveReq) (*navigationdto.SwapMoveResp, error) {
	s.moveCalls = append(s.moveCalls, req)
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

type translationWriteServiceStub struct {
	calls     []*navigationcontract.TranslationWriteReq
	result    *navigationcontract.TranslationWriteResult
	nilResult bool
	err       error
}

func (s *translationWriteServiceStub) WriteTranslations(_ context.Context, req *navigationcontract.TranslationWriteReq) (*navigationcontract.TranslationWriteResult, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.nilResult {
		return nil, nil
	}
	if s.result != nil {
		return s.result, nil
	}
	return &navigationcontract.TranslationWriteResult{}, nil
}

type translationWriteRejected struct {
	err   error
	items []facing.BlockedItem
}

func (e *translationWriteRejected) Error() string                      { return e.err.Error() }
func (e *translationWriteRejected) Unwrap() error                      { return e.err }
func (e *translationWriteRejected) BlockedItems() []facing.BlockedItem { return e.items }

var _ facing.BlockedError = (*translationWriteRejected)(nil)

func newTranslationWriteRouter(svc navigationcontract.NavigationService, writer navigationcontract.TranslationWriteService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handle := NewHandle(svc)
	if writer != nil {
		handle.SetTranslationWriteService(writer)
	}
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	router.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	router.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/api/navigation/translations", pageview.HTMLFragment, handle.UpdateTranslations)
	router.POST("/api/navigation/update", pageview.HTMLFragment, handle.Update)
	router.POST("/api/navigation/move", pageview.HTMLFragment, handle.Move)
	return router
}

// postLegacyUpdate 是 legacy 编辑的表单入口（该端点只接受标准表单）。
// postMoveJSON 把 move 用例的 JSON 输入转成表单打到 /move（该端点只接受表单）。
func postMoveJSON(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	var fields map[string]string
	_ = json.Unmarshal([]byte(body), &fields)
	form := url.Values{}
	for k, v := range fields {
		if k == "action" {
			continue
		}
		form.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postLegacyUpdate(t *testing.T, router *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postNavigationWrite(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postNavigationWriteWithLanguage(t *testing.T, router *gin.Engine, lang, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", lang)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestNavigationUpdateLegacyShapeIsUnchanged(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(stub, writer)

	ok := postLegacyUpdate(t, router, url.Values{"id": {"nav-1"}, "title": {"首页"}})
	if ok.Code != http.StatusOK {
		t.Fatalf("旧形状（含未知字段）应 200，实际 %d body=%s", ok.Code, ok.Body.String())
	}
	if len(stub.calls) != 1 {
		t.Fatalf("旧形状应走到 service.Update，实际调用 %d 次", len(stub.calls))
	}
	got := stub.calls[0]
	if got.ID != "nav-1" || got.Title == nil || *got.Title != "首页" {
		t.Fatalf("旧形状的绑定结果被改动：%+v", got)
	}

	rejects := []struct {
		name string
		form url.Values
	}{
		{"缺 id（binding:required 必须仍然生效）", url.Values{"title": {"没有 id"}}},
		{"空 id", url.Values{"id": {""}}},
		{"空体", url.Values{}},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before, beforeWrites := len(stub.calls), len(writer.calls)
			rec := postLegacyUpdate(t, router, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != before || len(writer.calls) != beforeWrites {
				t.Fatal("形状被拒时不许走到 service（形状判定必须发生在写之前）")
			}
		})
	}
}

func TestNavigationUpdateTranslationShapeIsStrict(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(stub, writer)

	valid := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":"Home"}]}`

	if rec := postNavigationWrite(t, router, valid); rec.Code != http.StatusOK {
		t.Fatalf("合法形状应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.calls) != 1 {
		t.Fatalf("合法形状必须调用写服务一次，实际 %d", len(writer.calls))
	}

	shapeRejects := []struct {
		name string
		body string
	}{
		{"action 为 null", `{"action":null,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为数字", `{"action":5,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为布尔", `{"action":true,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为空串", `{"action":"","project":"p-1","lang":"en-US","rows":[]}`},
		{"action 未知值", `{"action":"delete","project":"p-1","lang":"en-US","rows":[]}`},
		{"顶层未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"keyword":"x"}`},
		{"缺 project", `{"action":"translations","lang":"en-US","rows":[]}`},
		{"project 为空", `{"action":"translations","project":"","lang":"en-US","rows":[]}`},
		{"project 非字符串", `{"action":"translations","project":7,"lang":"en-US","rows":[]}`},
		{"缺 lang", `{"action":"translations","project":"p-1","rows":[]}`},
		{"lang 不受支持", `{"action":"translations","project":"p-1","lang":"de-DE","rows":[]}`},
		{"缺 rows", `{"action":"translations","project":"p-1","lang":"en-US"}`},
		{"rows 为 null", `{"action":"translations","project":"p-1","lang":"en-US","rows":null}`},
		{"rows 不是数组", `{"action":"translations","project":"p-1","lang":"en-US","rows":{}}`},
		{"行不是对象", `{"action":"translations","project":"p-1","lang":"en-US","rows":[1]}`},
		{"行有未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":"t","extra":1}]}`},
		{"行的值是数字", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":7}]}`},
		{"行的值是 null", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":null,"target":"t"}]}`},

		{"行缺 target", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h"}]}`},
		{"行缺 context", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"hash":"h","target":"t"}]}`},
		{"行缺 hash", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","target":"t"}]}`},
		{"第二行缺 target", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":"t"},{"context":"c","hash":"h2"}]}`},
	}
	for _, tc := range shapeRejects {
		t.Run(tc.name, func(t *testing.T) {
			before, beforeWrites := len(stub.calls), len(writer.calls)
			rec := postNavigationWrite(t, router, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != before || len(writer.calls) != beforeWrites {
				t.Fatal("形状被拒时不许走到任何 service")
			}
		})
	}
}

func TestNavigationUpdateTranslationDelegatesToWriter(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{result: &navigationcontract.TranslationWriteResult{
		Written: 2, ChangedKinds: []string{"header", "footer"},
	}}
	router := newTranslationWriteRouter(stub, writer)

	body := `{"action":"translations","project":"P-1","lang":"en-US","rows":[` +
		`{"context":"navigation.label","hash":"h-1","target":"Home"},` +
		`{"context":"navigation.label","hash":"h-2","target":"About"}]}`
	rec := postNavigationWrite(t, router, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.calls) != 1 {
		t.Fatalf("写服务应被调用一次，实际 %d", len(writer.calls))
	}
	got := writer.calls[0]
	if got.ProjectID != "P-1" || got.Lang != "en-US" {
		t.Fatalf("project / lang 搬运错误：%+v", got)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows 应原序搬到写服务（空 target 也是合法行，由 service 判「本次不改」），实际 %d 行", len(got.Rows))
	}
	if got.Rows[0].Context != "navigation.label" || got.Rows[0].Hash != "h-1" || got.Rows[0].Target != "Home" {
		t.Fatalf("第一行搬运错误：%+v", got.Rows[0])
	}
	if got.Rows[1].Hash != "h-2" || got.Rows[1].Target != "About" {
		t.Fatalf("第二行搬运错误：%+v", got.Rows[1])
	}

	// 回执是 Jet 片段：写入条数来自写服务的真实结果。
	if !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("成功应回结果片段：%s", rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatal("译文分支不许落到导航项更新上")
	}
}

func TestNavigationUpdateTranslationEmptyTargetPassesShape(t *testing.T) {
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":""}]}`
	if rec := postNavigationWrite(t, router, body); rec.Code != http.StatusOK {
		t.Fatalf("显式空 target 是合法形状，应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.calls) != 1 || len(writer.calls[0].Rows) != 1 || writer.calls[0].Rows[0].Target != "" {
		t.Fatalf("空 target 行必须原样送到写服务：%+v", writer.calls)
	}
}

func TestNavigationUpdateTranslationLimits(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(stub, writer)

	row := `{"context":"navigation.label","hash":"h","target":"Home"}`
	mk := func(n int) string {
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, row)
		}
		return `{"action":"translations","project":"p-1","lang":"en-US","rows":[` + strings.Join(items, ",") + `]}`
	}
	if rec := postNavigationWrite(t, router, mk(1001)); rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 1000 行应 400，实际 %d", rec.Code)
	}
	if len(writer.calls) != 0 {
		t.Fatal("行数超限必须在写之前拒绝")
	}

	if rec := postNavigationWrite(t, router, mk(1000)); rec.Code != http.StatusOK {
		t.Fatalf("恰好 1000 行应通过形状判定并走到写服务，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.calls) != 1 || len(writer.calls[0].Rows) != 1000 {
		t.Fatalf("1000 行应原样送到写服务，实际 %d 次调用", len(writer.calls))
	}

	huge := `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"pad":"` +
		strings.Repeat("x", navigationWriteMaxBodyBytes) + `"}`
	if rec := postNavigationWrite(t, router, huge); rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 字节的请求体应 400，实际 %d", navigationWriteMaxBodyBytes, rec.Code)
	}
}

func TestNavigationUpdateTranslationsFailClosed(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub, nil)

	rec := postNavigationWrite(t, router,
		`{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h","target":"Home"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未装配写服务应 500，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("500 响应体应是错误片段：%s", rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatal("译文分支不许落到导航项更新上")
	}
}

func TestNavigationUpdateTranslationsBlockedItems(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrNotTranslatable: {
			"zh-CN": "%s：该菜单文字不参与翻译（纯数字或纯符号）",
			"en-US": "%s: this menu text is not translatable",
		},
	})
	writer := &translationWriteServiceStub{err: &translationWriteRejected{
		err: facing.WithStatus(
			facing.New(navigationenums.ErrNotTranslatable),
			http.StatusConflict),
		items: []facing.BlockedItem{{
			ID:        "h-1",
			Name:      "12345",
			ReasonKey: navigationenums.ErrNotTranslatable,
			// 声明处不再带中文兜底：空串让出口回落到 navigationReasonFallbacks，
			// 与 FactOf 现在 Fallback == Key 的形态一致。
			Fallback: "",
		}},
	}}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "该菜单文字不参与翻译"},
		{"en-US", "this menu text is not translatable"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			rec := postNavigationWriteWithLanguage(t, router, tc.lang,
				`{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":"X"}]}`)
			if rec.Code != http.StatusConflict {
				t.Fatalf("声明处给 409，出口必须照用，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			bodyText := rec.Body.String()
			// 主消息按请求语言取词；逐条明细（12345 及其原因）同样在本响应里可定位。
			if !strings.Contains(bodyText, tc.want) {
				t.Fatalf("%s 下失败提示应取该语言，实际 %s", tc.lang, bodyText)
			}
			if !strings.Contains(bodyText, "12345") {
				t.Fatalf("逐条拒绝必须给出可定位对象：%s", bodyText)
			}
			if strings.Contains(bodyText, navigationenums.ErrNotTranslatable) {
				t.Fatalf("提示里不该出现裸 key：%s", bodyText)
			}
		})
	}
}

func TestNavigationUpdateTranslationsBusinessErrorText(t *testing.T) {

	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrTranslationWriteUnavailable: {
			"zh-CN": "译文写入服务未就绪，请联系管理员",
			"en-US": "Translation write service is not ready",
		},
	})
	writer := &translationWriteServiceStub{err: navigationenums.ErrTranslationWriteUnavailableError()}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[]}`
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "译文写入服务未就绪，请联系管理员"},
		{"en-US", "Translation write service is not ready"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			rec := postNavigationWriteWithLanguage(t, router, tc.lang, body)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("声明处给 500，实际 %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("%s 下应给该语言的业务句，实际 %s", tc.lang, rec.Body.String())
			}
		})
	}
}

func TestNavigationUpdateTranslationsInternalErrorHidesDetail(t *testing.T) {
	writer := &translationWriteServiceStub{err: errors.New("pq: relation \"sys_translation\" does not exist")}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	rec := postNavigationWrite(t, router,
		`{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h","target":"Home"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未归类错误必须 500，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sys_translation") {
		t.Fatalf("内部原文泄漏到响应：%s", rec.Body.String())
	}
}

func TestNavigationUpdateMoveShapeAndDelegation(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(stub, writer)

	rec := postMoveJSON(t, router, `{"id":"nav-1","dir":"up","projectId":"p-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 move 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.moveCalls) != 1 {
		t.Fatalf("应调用 SwapMove 一次，实际 %d", len(stub.moveCalls))
	}
	got := stub.moveCalls[0]
	if got.ID != "nav-1" || got.Dir != "up" || got.ProjectID != "p-1" {
		t.Fatalf("move 入参搬运错误：%+v", got)
	}
	if len(writer.calls) != 0 {
		t.Fatal("move 分支不许落到译文写服务上")
	}

	rec = postMoveJSON(t, router, `{"id":"nav-1","dir":"down"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("缺省 projectId 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.moveCalls) != 2 || stub.moveCalls[1].ProjectID != "" {
		t.Fatalf("缺省 projectId 应搬成空串：%+v", stub.moveCalls)
	}

	for _, body := range []string{
		`{"action":"move","id":"nav-1","dir":"up","projectId":null}`,
		`{"action":"move","id":"nav-1","dir":"up","projectId":7}`,
		`{"action":"move","id":"nav-1","dir":""}`,
		`{"action":"move","dir":"up"}`,
		`{"action":"move","id":"nav-1"}`,
		`{"action":"move","id":"nav-1","dir":"up","kind":"header"}`,
		`{"action":"move","id":"nav-1","dir":"up","sortOrder":3}`,
	} {
		before := len(stub.moveCalls)
		rec := postNavigationWrite(t, router, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d body=%s", body, rec.Code, rec.Body.String())
		}
		if len(stub.moveCalls) != before {
			t.Fatalf("%s 形状被拒时不许调用 SwapMove", body)
		}
	}
}

func TestNavigationUpdateReadsBodyOnce(t *testing.T) {
	src, err := os.ReadFile("navigation_handle.go")
	if err != nil {
		t.Fatalf("读 navigation_handle.go 失败: %v", err)
	}
	text := string(src)
	start := strings.Index(text, "func (h *Handle) Update(c *gin.Context) {")
	if start < 0 {
		t.Fatal("找不到 Update 入口")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("Update 入口没有正常结束")
	}
	body := text[start : start+end]

	// update 端点只有一种确定协议：标准表单，统一走 binding.ParseForm。JSON 输入属于独立端点，不在这里分流。
	if !strings.Contains(body, "binding.ParseForm[navigationdto.UpdateReq](c)") {
		t.Fatal("update 必须经 binding.ParseForm[navigationdto.UpdateReq](c) 绑定")
	}
	for _, banned := range []string{"navigationWriteBody", "json.Unmarshal", "ShouldBindJSON", "BindBody", "application/json"} {
		if strings.Contains(body, banned) {
			t.Fatalf("update 里出现了其它协议分支（%s）：一个端点只允许一种确定协议", banned)
		}
	}

	// update 只做基本字段编辑；换位与译文各有独立端点，不再按 action 分流。
	if strings.Contains(body, "env.Route()") {
		t.Fatal("update 不该再按 action 分流：换位与译文已拆成独立端点")
	}
}

func TestNavigationUpdateTranslationsHaveNoLegacyAdapter(t *testing.T) {
	for _, file := range []string{"navigation_handle.go", "navigation_translations_handle.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", file, err)
		}
		text := string(src)
		for _, gone := range []string{
			"navigationTranslationWriteInst",
			"SetNavigationTranslationWriter",
			"updateTranslationsEnvelope",
			"writeTranslations(",
			"navigationTranslationMaxRows",
			"navigationWriteErrorsText",
		} {
			if strings.Contains(text, gone) {
				t.Errorf("%s 仍保留旧译文写形态 %q（必须只剩一条路径）", file, gone)
			}
		}
	}

	src, err := os.ReadFile("navigation_handle.go")
	if err != nil {
		t.Fatalf("读 navigation_handle.go 失败: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"writerSvc navigationcontract.TranslationWriteService",
		"func (h *Handle) SetTranslationWriteService(s navigationcontract.TranslationWriteService)",
		"h.writerSvc.WriteTranslations(",
		"pageview.FailScene(c, navigationErrScene, err)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("navigation_handle.go 缺少写服务接线：%s", want)
		}
	}
}

func TestNavigationTranslationWriteRouteAndTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	apiRouter := readFile("navigation_router.go")
	if !strings.Contains(apiRouter, `g.POST("/update", navigationenums.NavigationUpdate, pageview.HTMLFragment, handle.Update)`) {
		t.Fatal("译文写必须挂在调度的 POST /api/navigation/translations 上")
	}

	pageRouter := readFile("navigation_page_router.go")
	if strings.Contains(pageRouter, "translations/save") {
		t.Fatal("页面侧的译文保存 POST 必须已经删除")
	}

	for _, gone := range []string{"SetNavigationTranslationWriter", "SetPageMarker", "SetMenuInvalidator"} {
		if strings.Contains(pageRouter, gone) {
			t.Fatalf("页面路由仍注入旧写端口 %q（页面句柄只服务读）", gone)
		}
	}

	template := readFile("../../../../templates/admin/navigation/navigation_translations.html")
	for _, want := range []string{
		`hx-post="/api/navigation/translations"`,
		`data-write-builder="navigation-translations"`,
		`data-write-result`,
		`name="action" value="translations"`,
		`navigation_translations.js`,
	} {
		if !strings.Contains(template, want) {
			t.Errorf("模板缺少保存契约声明: %s", want)
		}
	}
	for _, gone := range []string{"rowTarget", "rowContext", "rowHash", "translations/save"} {
		if strings.Contains(template, gone) {
			t.Errorf("模板里仍有并行数组时代的痕迹 %q（按位置对齐的隐式契约必须消失）", gone)
		}
	}
}

func TestNavigationTranslationWriteReceiptIsSharedSuccess(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		"uiOpSuccess": {"zh-CN": "操作成功-词条", "en-US": "success-entry"},
	})

	writer := &translationWriteServiceStub{result: &navigationcontract.TranslationWriteResult{
		Written: 2, ChangedKinds: []string{"header", "footer"},
	}}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[` +
		`{"context":"navigation.label","hash":"h-1","target":"Home"},{"context":"navigation.label","hash":"h-2","target":"About"}]}`
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "操作成功-词条"},
		{"en-US", "success-entry"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			rec := postNavigationWriteWithLanguage(t, router, tc.lang, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("应 200，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			payload := rec.Body.String()
			// 普通成功只给公共一句提示；写入条数属明细，不进入成功消息。
			if !strings.Contains(payload, `role="status"`) || !strings.Contains(payload, tc.want) {
				t.Fatalf("%s 下应渲染公共成功提示 %q：%s", tc.lang, tc.want, payload)
			}
			if strings.Contains(payload, "2") || strings.Contains(payload, "uiOpSuccess") {
				t.Fatalf("成功回执不得携带条数或裸 key：%s", payload)
			}
		})
	}
}

func TestNavigationTranslationWriteNilReceiptIsFailure(t *testing.T) {
	writer := &translationWriteServiceStub{nilResult: true}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h","target":"Home"}]}`
	rec := postNavigationWrite(t, router, body)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("空回执应 500，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"written"`) {
		t.Fatalf("失败响应不许带 written 事实（那等于声称写了 0 条）：%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), navigationenums.ErrTranslationWriteUnavailable) {
		t.Fatalf("错误 key 应被取词，不许裸 key：%s", rec.Body.String())
	}
}

func TestNavigationTranslationsPageRendersJSONWriteForm(t *testing.T) {
	projects := &pagePresentationProjectStub{}

	workbench := &workbenchNavigationStub{index: &workbenchIndex{
		kinds: []string{"header"},
		entries: []navigationcontract.TranslationSourceEntry{{
			Hash: i18n.ContentHash("首页"), Source: "首页", Path: "/",
			NodeID: "row-1", Context: "navigation.label", Kinds: []string{"header"},
		}},
	}}
	router := newNavigationsPageTestRouter(t, workbench, projects)
	router.GET("/admin/navigations/translations",
		pageview.Slot(NavigationTranslationsTemplate, pageview.RenderPage),
		NewNavigationTranslationHandle(workbench, projects).NavigationTranslations)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/navigations/translations?project=project-1&lang=en-US", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("译文页渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		`hx-post="/api/navigation/translations"`,
		`data-write-builder="navigation-translations"`,
		`data-write-result`,
		`name="action" value="translations"`,
		`class="field nav-tr-row"`,
		`name="context"`,
		`name="hash"`,
		`name="target"`,
		`data-write-status`,
		`navigation_translations.js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("译文页缺少 %q（模板契约被改坏）", want)
		}
	}
	for _, gone := range []string{"rowContext", "rowHash", "rowTarget", "translations/save"} {
		if strings.Contains(body, gone) {
			t.Errorf("译文页仍有 %q（按位置对齐的并行数组契约必须消失）", gone)
		}
	}

	if !strings.Contains(body, `name="lang" value="en-US"`) || !strings.Contains(body, `name="project" value="project-1"`) {
		t.Errorf("写请求应带 project 与 lang：%s", body)
	}
}
