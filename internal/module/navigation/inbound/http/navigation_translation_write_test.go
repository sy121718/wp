package navigationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
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
	router.POST("/api/navigation/update", handle.Update)
	return router
}

func postNavigationWrite(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postNavigationWriteWithLanguage(t *testing.T, router *gin.Engine, lang, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
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

	ok := postNavigationWrite(t, router, `{"id":"nav-1","title":"首页","unknownField":123}`)
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
		body string
	}{
		{"缺 id（binding:required 必须仍然生效）", `{"title":"没有 id"}`},
		{"空 id", `{"id":""}`},
		{"混形 rows", `{"id":"nav-1","rows":[]}`},
		{"混形 project", `{"id":"nav-1","project":"p-1"}`},
		{"混形 lang", `{"id":"nav-1","lang":"en-US"}`},
		{"混形 dir", `{"id":"nav-1","dir":"up"}`},
		{"混形 projectId", `{"id":"nav-1","projectId":"p-1"}`},
		{"不是对象", `[]`},
		{"JSON 非法", `{`},
		{"空体", ``},
		{"显式 null", `null`},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before, beforeWrites := len(stub.calls), len(writer.calls)
			rec := postNavigationWrite(t, router, tc.body)
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

	if !strings.Contains(rec.Body.String(), `"written":2`) {
		t.Fatalf("回执 written 必须来自写服务：%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"changedKinds":["header","footer"]`) {
		t.Fatalf("回执 changedKinds 必须原样来自写服务：%s", rec.Body.String())
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
	if !strings.Contains(rec.Body.String(), `"code":500`) {
		t.Fatalf("500 响应体形态不对：%s", rec.Body.String())
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
			facing.New(navigationenums.ErrNotTranslatable, navigationenums.FallbackFor(navigationenums.ErrNotTranslatable)),
			http.StatusConflict),
		items: []facing.BlockedItem{{
			ID:        "h-1",
			Name:      "12345",
			ReasonKey: navigationenums.ErrNotTranslatable,
			Fallback:  navigationenums.FallbackFor(navigationenums.ErrNotTranslatable),
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
			if !strings.Contains(bodyText, `"items"`) || !strings.Contains(bodyText, `"h-1"`) ||
				!strings.Contains(bodyText, navigationenums.ErrNotTranslatable) {
				t.Fatalf("整批拒绝的可定位明细必须落进 data.items：%s", bodyText)
			}
			if !strings.Contains(bodyText, tc.want) {
				t.Fatalf("%s 下明细文案应取该语言，实际 %s", tc.lang, bodyText)
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

	rec := postNavigationWrite(t, router, `{"action":"move","id":"nav-1","dir":"up","projectId":"p-1"}`)
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

	rec = postNavigationWrite(t, router, `{"action":"move","id":"nav-1","dir":"down"}`)
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

	if !strings.Contains(body, "binding.JSONLimited[navigationdto.UpdateEnvelope](c, 1<<20)") {
		t.Fatal("Update 必须经 binding.JSONLimited[UpdateEnvelope](c, 1<<20) 这一行绑定")
	}
	for _, banned := range []string{"navigationWriteBody", "json.Unmarshal", "ParseForm", "ShouldBind", "BindBody"} {
		if strings.Contains(body, banned) {
			t.Fatalf("Update 里出现了第二段读体/解析（%s）：请求体只允许读一次", banned)
		}
	}

	if !strings.Contains(body, "env.Route()") {
		t.Fatal("判别必须交给 UpdateEnvelope.Route()")
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
		"response.ErrorStatusAuto(c, \"navigation\", err)",
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
	if !strings.Contains(apiRouter, `g.POST("/update", permission.NavigationUpdate, handle.Update)`) {
		t.Fatal("译文写必须挂在调度的 POST /api/navigation/update（同一权限点、同一真实路由）上")
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
		`data-write-url="/api/navigation/update"`,
		`data-write-rows=".nav-tr-row;context,hash,target"`,
		`data-write-nav="{{ .["SuccessURL"] }}"`,
		`name="action" value="translations"`,
		`disabled data-requires-js`,

		`data-write-items`,
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

func TestNavigationTranslationWriteReceiptFollowsRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.Saved: {
			"zh-CN": "已保存 %s 条译文",
			"en-US": "Saved %s translation(s)",
		},
	})

	writer := &translationWriteServiceStub{result: &navigationcontract.TranslationWriteResult{
		Written: 2, ChangedKinds: []string{},
	}}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h","target":"Home"}]}`
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "已保存 2 条译文"},
		{"en-US", "Saved 2 translation(s)"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			rec := postNavigationWriteWithLanguage(t, router, tc.lang, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("应 200，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("%s 下回执应取该语言，实际 %s", tc.lang, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "%!") || strings.Contains(rec.Body.String(), navigationenums.Saved) {
				t.Fatalf("回执既不许带格式化垃圾也不许裸 key：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationTranslationWriteReceiptIsFactCopy(t *testing.T) {
	writer := &translationWriteServiceStub{result: &navigationcontract.TranslationWriteResult{
		Written: 2, ChangedKinds: []string{"header", "footer"},
	}}
	router := newTranslationWriteRouter(&translationWriteNavigationStub{}, writer)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[` +
		`{"context":"navigation.label","hash":"h-1","target":"Home"},{"context":"navigation.label","hash":"h-2","target":"About"}]}`
	rec := postNavigationWrite(t, router, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON：%v body=%s", err, rec.Body.String())
	}
	if got := string(payload.Data["written"]); got != "2" {
		t.Fatalf("written 必须原样来自写服务（两行提交、服务报 2），实际 %s", got)
	}
	if got := string(payload.Data["changedKinds"]); got != `["header","footer"]` {
		t.Fatalf("changedKinds 必须原样来自写服务且保序，实际 %s", got)
	}
	if _, exists := payload.Data["Written"]; exists {
		t.Fatalf("事实字段不许变大写（contract 无 json tag 的形状不得直发）：%s", rec.Body.String())
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
		adminui.PageSlot(NavigationTranslationsTemplate),
		NewNavigationTranslationHandle(workbench, projects).NavigationTranslations)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/navigations/translations?project=project-1&lang=en-US", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("译文页渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		`data-write-url="/api/navigation/update"`,
		`data-write="create"`,
		`data-write-rows=".nav-tr-row;context,hash,target"`,
		`data-write-nav="/admin/navigations/translations?`,
		`name="action" value="translations"`,
		`class="field nav-tr-row"`,
		`name="context"`,
		`name="hash"`,
		`name="target"`,
		`data-write-status`,
		`disabled data-requires-js`,
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

	if !strings.Contains(body, "lang=en-US") || !strings.Contains(body, "project=project-1") {
		t.Errorf("数据写回跳地址应带 project 与 lang：%s", body)
	}
}
