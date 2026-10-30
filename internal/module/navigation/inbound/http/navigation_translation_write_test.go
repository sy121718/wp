package navigationhttp

// navigation_translation_write_test.go — /api/navigation/update 的**严格 tagged union** 形状契约，
// 以及译文分支与写服务（contract.TranslationWriteService）之间的接线契约。
//
// 一条路径承担三件事：旧的导航项更新、译文整表提交（action="translations"）、同级移动
// （action="move"）。三者共用同一个权限点与同一条真实路由，所以「判别键写得模棱两可」
// 「两条形状混在一起」这类缺陷不会有任何编译期或运行期报错 —— 只会在某天把不该写的
// 数据写进去。本文件把形状判据（含拒绝面）逐条钉住，全部无库。
//
// 译文分支现在**直接调 writerSvc**（不再是「拼回 JSON 交给旧 helper」，也没有包级句柄）：
// 这里用四种桩证明接线 —— 成功桩（回执 written / changedKinds 来自服务返回值）、
// 空回执桩（nil, nil → 500，不伪造 written:0）、拒绝桩（BlockedError 的
// 逐条明细落进 data.items 且状态码取声明处）、缺服务（fail closed 500 + 归位文案）。

import (
	"context"
	"encoding/json"
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
)

// translationWriteNavigationStub 只实现 Update / SwapMove（其余走嵌入的 nil 接口）。
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

// translationWriteServiceStub 记录调用入参，按预设返回结果或错误。
//
// nilResult 模拟「实现缺陷：nil, nil」—— 与「零值结果」是两件事，出口必须区分。
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

// translationWriteRejected 整批拒绝的形状（与 service 的 TranslationRejectedError 同形状：
// 出口只认 facing.BlockedError 接口，不认识具体类型）。
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

// ------------------------------------------------------------------ 形状契约

// TestNavigationUpdateLegacyShapeIsUnchanged 缺 action = 旧的更新请求，行为不变。
//
// 三条硬事实：`binding:"required"` 的 id 仍然生效（裸 json.Unmarshal 会静默跳过它）、
// 旧契约里多出来的未知字段仍被忽略（不借这次迁移收紧）、任一条分支的专属键一出现就拒（混形）。
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

// TestNavigationUpdateTranslationShapeIsStrict 译文分支：判别键与字段白名单一票否决。
func TestNavigationUpdateTranslationShapeIsStrict(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	writer := &translationWriteServiceStub{}
	router := newTranslationWriteRouter(stub, writer)

	valid := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":"Home"}]}`

	// 先确认夹具本身是合法形状（否则「全都 400」也可能是判别键写错了造成的假绿）：
	// 合法形状必须真的走到写服务 —— 这是本文件「不再只 500 假成功」的基线。
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
		// 缺键与「显式空串」是两件事：只有后者是契约里的「本次不改动这一行」。
		// 缺键若被放行，下游会把零值的 target 当成空译文跳过 → 形状错误静默变成
		// 200 + written:0（客户端看不出一个字段都没提交）。
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

// TestNavigationUpdateTranslationDelegatesToWriter 译文分支直接调写服务：入参逐字段搬运。
//
// 这一条替换的是旧实现里「把已解析的结构拼回 JSON 字节再交给 helper」的形状：
// 现在 dto → contract 是一次显式转换，**不判业务**。若搬运漏字段（例如 Rows 的 target），
// 下游会把它读成「本次不改动这一行」，症状是 200 + written:0 —— 所以三条字段逐一比对。
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
	// 回执 written 来自服务返回值，不是 handler 自己数出来的。
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

// TestNavigationUpdateTranslationEmptyTargetPassesShape 显式空 target 是合法形状。
//
// 与「行缺 target 必须 400」配对：两条一起才说明判据卡的是**键是否存在**，不是
// 「值是否为空」。空 target 也要原样送到写服务（由它解释成「本次不改这一行」）。
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

// TestNavigationUpdateTranslationLimits 行数上限与请求体上限都在写入之前拦下。
//
// 行数上限的判据已统一到 dto（updateTranslationMaxRows = 1000，与旧常量同一数字），
// 所以这里断言的是**外部行为**（1000 收下、1001 拒），不再引用 handler 的私有常量。
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
	// 恰好等于上限：形状合法 → 真的走到写服务（说明上限本身没被多算一行）。
	if rec := postNavigationWrite(t, router, mk(1000)); rec.Code != http.StatusOK {
		t.Fatalf("恰好 1000 行应通过形状判定并走到写服务，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(writer.calls) != 1 || len(writer.calls[0].Rows) != 1000 {
		t.Fatalf("1000 行应原样送到写服务，实际 %d 次调用", len(writer.calls))
	}
	// 请求体上限：解析之前就要拒（否则超大 body 会先进内存）。
	huge := `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"pad":"` +
		strings.Repeat("x", navigationWriteMaxBodyBytes) + `"}`
	if rec := postNavigationWrite(t, router, huge); rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 字节的请求体应 400，实际 %d", navigationWriteMaxBodyBytes, rec.Code)
	}
}

// TestNavigationUpdateTranslationsFailClosed 写服务未装配：拒绝，而不是「接受请求但什么都没写」。
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

// TestNavigationUpdateTranslationsBlockedItems 整批拒绝：状态码来自声明处，明细进 data.items。
//
// 三条判据：① 业务错误的状态码由 facing.WithStatus 声明（这里 409），出口不重新分档；
// ② facing.BlockedError 的逐条明细落进 data.items（id / name / reasonKey / message）；
// ③ 明细里的 message 按**请求语言**取词（reasonKey 是声明处的 i18n key）。
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

// TestNavigationUpdateTranslationsBusinessErrorText 普通业务错误（无明细）：状态码 + 文案都由声明处给。
func TestNavigationUpdateTranslationsBusinessErrorText(t *testing.T) {
	// 合成词条 + 用后还原：本包已有的注入助手（navigation_facing_text_test.go）。
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

// TestNavigationUpdateTranslationsInternalErrorHidesDetail 未归类错误（SQL / 表名）：对外只给归口文案。
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

// ------------------------------------------------------------------ 移动分支

// TestNavigationUpdateMoveShapeAndDelegation move 档：白名单、必填、可选 projectId 全部原样保留。
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

	// projectId 缺省 = 不校验工程归属（空串）。
	rec = postNavigationWrite(t, router, `{"action":"move","id":"nav-1","dir":"down"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("缺省 projectId 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.moveCalls) != 2 || stub.moveCalls[1].ProjectID != "" {
		t.Fatalf("缺省 projectId 应搬成空串：%+v", stub.moveCalls)
	}

	// projectId 显式 null 与非字符串：形状错（与 addSource 档的 kind 同一条判据）。
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

// ------------------------------------------------------------------ 源码级契约

// TestNavigationUpdateReadsBodyOnce 入口只有一处绑定：源码里不再有第二段读体/解析。
//
// 这条是**源码级**断言而非行为断言：行为上「读两次」很容易因为流被重置而看不出来
// （pkg/binding.JSONLimited 会把读过的 body 重新塞回 Request.Body），只有把调用点
// 钉住，后来者才没法悄悄在 Update 里再加一段 navigationWriteBody。
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
	// 判别必须走 dto：handler 自己 switch 字面量就是第二个真相。
	if !strings.Contains(body, "env.Route()") {
		t.Fatal("判别必须交给 UpdateEnvelope.Route()")
	}
}

// TestNavigationUpdateTranslationsHaveNoLegacyAdapter 旧的 raw 拼装 / 旧写句柄必须消失。
//
// 三个符号是「翻译 MVC 之前」的形态：把 dto 的结构拼回 JSON 字节再交给 helper
// （updateTranslationsEnvelope）、包级可变句柄（navigationTranslationWriteInst）、以及
// 它对应的注入点（SetNavigationTranslationWriter）。它们留着就等于保留第二条写路径，
// 而两条路径的事务边界与失效口径不同 —— 哪一条在跑只能靠读代码判断。
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
	// 新形态必须在位：写服务字段 + 注入点 + 唯一调用。
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

// TestNavigationTranslationWriteRouteAndTemplateContract 路由/权限/模板三条声明事实。
//
// 这一层是**源码级**断言（无库、无装配）：本模块拿不到 authorizedAPI 组（它在
// internal/routers/assembly.go 里装配），所以用「唯一注册点 + 句子形态」钉住：
//   - 译文写只有一条路径：POST /api/navigation/update，权限点仍是调度的 NavigationUpdate；
//   - 页面侧的旧 POST 已经不存在（既没有路由，也没有模板表单会发它）；
//   - 模板声明了行数组与「成功回跳」，且保存按钮无 JS 时是真 disabled。
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
	// 页面侧不再是写入口：包级写句柄与它的注入点都必须消失，否则同一动作会有两条
	// 事务边界不同的写路径，而在跑哪一条只能靠读代码判断。
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
		// 整批拒绝的逐条明细只有 opt-in 了才会渲染：模板漏了它，后端算出来的
		// data.items 就到不了运营眼前（页面只显示一句「保存失败」）。
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

// ------------------------------------------------------------------ 回执事实 / 页面渲染

// TestNavigationTranslationWriteReceiptFollowsRequestLanguage 成功回执按请求语言取词。
//
// 回执文案现在**不再**由 handler 取词拼好（那是「取词 + 填占位符」的成品句），
// 而是交 enums.Saved 这个 key 给 pkg/response：取词在一次内存查询里完成，
// 参数注入走 key|param 协议（模板只允许 %s）。这条链断了的表现是「英文界面里出现中文」
// 或「响应里裸出 key」，而中文环境下完全看不出来。
func TestNavigationTranslationWriteReceiptFollowsRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.Saved: {
			"zh-CN": "已保存 %s 条译文",
			"en-US": "Saved %s translation(s)",
		},
	})
	// 空切片而不是 nil：service 的成功路径从不返回 nil ChangedKinds（见 contract 的注释），
	// 「没有变化」的回执也必须是 [] 而不是 null。桩照抄这条事实，回执断言才有意义。
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

// TestNavigationTranslationWriteReceiptIsFactCopy 回执是 contract 事实的纯拷贝。
//
// HTTP 层只许搬运：written 条数、changedKinds 顺序都必须是写服务交回的那个值
// （handler 自己数行数或重排位置都会让回执与库内实况不一致，而测试看不出来）。
// 代价是「没有变化」时 data 也必须是 {written:0, changedKinds:[]} —— 不是 null、也不是省略。
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

// TestNavigationTranslationWriteNilReceiptIsFailure 写服务交空回执 = 失败，不是「写了 0 条」。
//
// 空回执意味着「是否写入无从得知」。把它报成 200 + written:0 会让运营以为
// 「提交了但库内没变化」而不再重试 —— 这是一条只能靠状态码纠正的误导。
// 判据是既有的装配失败事实（500），不是新造的错误形状。
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

// TestNavigationTranslationsPageRendersJSONWriteForm 整页渲染：模板声明与旧并行数组契约的消失。
//
// 渲染层是「模板被改坏却没人发现」的唯一防线：本用例走**真实 GET 路由 + 真实模板**，
// 断言四件事 —— ① JSON 写的五个声明都在（端点 / 模式 / 行数组 / 回跳 / 动作标签）；
// ② 行容器与三个具名字段是真的渲染出来了（不是只在模板源码里）；
// ③ 并行数组时代的 `rowContext/rowHash/rowTarget` 与页面 POST 地址一个都不剩；
// ④ 保存按钮无 JS 时是真 disabled。
//
// 页面句柄只保留读侧（workspace 的 build / sourceIndex），写侧端口已随旧写路径删除 ——
// 因此这里构造的最小句柄不需要注入任何写端口。
func TestNavigationTranslationsPageRendersJSONWriteForm(t *testing.T) {
	projects := &pagePresentationProjectStub{}
	// 页壳渲染的是**正文**，但页面数据来自 build（要 OpenSourceIndex）；只实现 Tree /
	// SourceGroups 的桩会让 build 走「索引未装配」那一支，整页只剩归口错误槽 ——
	// 下面那 11 条契约一条都不会出现。所以这里换成工作台桩（真实来源索引）。
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
	// 回跳地址必须已经被转义/拼好（含 project 与 lang），不是模板自己拼的裸串。
	if !strings.Contains(body, "lang=en-US") || !strings.Contains(body, "project=project-1") {
		t.Errorf("数据写回跳地址应带 project 与 lang：%s", body)
	}
}
