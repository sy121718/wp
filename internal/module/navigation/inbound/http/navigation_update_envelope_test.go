package navigationhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// 导航写端点按动作拆开：一个端点一种确定协议。
//   /api/navigation/update        表单（基本字段编辑）
//   /api/navigation/move          表单（上移 / 下移）
//   /api/navigation/translations  JSON（行是 context/hash/target 三元组）

type envelopeNavigationStub struct {
	navigationcontract.NavigationService
	updateCalls []*navigationdto.UpdateReq
	moveCalls   []*navigationdto.SwapMoveReq
}

func (s *envelopeNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.updateCalls = append(s.updateCalls, req)
	return &navigationdto.NavigationResp{ID: req.ID, Title: "示例"}, nil
}

func (s *envelopeNavigationStub) SwapMove(_ context.Context, req *navigationdto.SwapMoveReq) (*navigationdto.SwapMoveResp, error) {
	s.moveCalls = append(s.moveCalls, req)
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

func newEnvelopeRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	router.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := NewHandle(svc)
	router.POST("/api/navigation/update", pageview.HTMLFragment, h.Update)
	router.POST("/api/navigation/move", pageview.HTMLFragment, h.Move)
	router.POST("/api/navigation/translations", pageview.HTMLFragment, h.UpdateTranslations)
	return router
}

func envelopeJSON(t *testing.T, router *gin.Engine, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func envelopeForm(t *testing.T, router *gin.Engine, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postMoveForm(t *testing.T, router *gin.Engine, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/move", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestNavigationUpdateLegacyUsesForm(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(t, stub)

	rec := envelopeForm(t, router, url.Values{"id": {"nav-2"}, "title": {"关于"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy 表单应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.updateCalls) != 1 {
		t.Fatalf("legacy 应调用一次 service.Update，实际 %d", len(stub.updateCalls))
	}
	if got := stub.updateCalls[0]; got.ID != "nav-2" || got.Title == nil || *got.Title != "关于" {
		t.Fatalf("legacy 表单绑定结果不对：%+v", got)
	}

	// 该端点只有标准表单编码一种协议：上传型 multipart 是另一个端点的职责，不在这里兼容。

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"缺 id", url.Values{"title": {"没有 id"}}},
		{"空 id", url.Values{"id": {""}}},
		{"空体", url.Values{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.updateCalls)
			rec := envelopeForm(t, router, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.updateCalls) != before {
				t.Fatal("required 不满足时不许走到 service")
			}
			if !strings.Contains(rec.Body.String(), `data-ui-error`) {
				t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationTranslationsEnvelope(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(t, stub)

	row := `{"context":"navigation.label","hash":"h-1","target":"Home"}`
	validJSON := `{"action":"translations","project":"p-1","lang":"en-US","rows":[` + row + `]}`
	if rec := envelopeJSON(t, router, "application/json", validJSON); rec.Code != http.StatusInternalServerError {
		t.Fatalf("合法形状 + 未注入句柄应 500（fail closed），实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.updateCalls) != 0 {
		t.Fatal("译文分支不该落到 basic update")
	}

	rejects := []struct {
		name string
		body string
	}{
		{"顶层未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"keyword":"x"}`},
		{"缺 project", `{"action":"translations","lang":"en-US","rows":[]}`},
		{"lang 不受支持", `{"action":"translations","project":"p-1","lang":"de-DE","rows":[]}`},
		{"缺 rows", `{"action":"translations","project":"p-1","lang":"en-US"}`},
		{"rows 为 null", `{"action":"translations","project":"p-1","lang":"en-US","rows":null}`},
		{"行缺 target", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h"}]}`},
		{"行有未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":"t","extra":1}]}`},
		{"非法 JSON", `{`},
		{"空体", ``},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.updateCalls)
			rec := envelopeJSON(t, router, "application/json", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.updateCalls) != before {
				t.Fatal("形状被拒时不许落 service")
			}
		})
	}
}

func TestNavigationMoveUsesForm(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(t, stub)

	rec := postMoveForm(t, router, url.Values{"id": {"nav-1"}, "dir": {"up"}, "projectId": {"p-1"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 move 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.moveCalls) != 1 {
		t.Fatalf("move 应调用一次 SwapMove，实际 %d", len(stub.moveCalls))
	}
	if got := stub.moveCalls[0]; got.ID != "nav-1" || got.Dir != "up" || got.ProjectID != "p-1" {
		t.Fatalf("move 字段没有逐字送达：%+v", got)
	}

	if rec := postMoveForm(t, router, url.Values{"id": {"nav-2"}, "dir": {"down"}}); rec.Code != http.StatusOK {
		t.Fatalf("缺省 projectId 的 move 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if got := stub.moveCalls[len(stub.moveCalls)-1]; got.ID != "nav-2" || got.Dir != "down" || got.ProjectID != "" {
		t.Fatalf("表单 move 字段不对（projectId 缺省 = 不校验归属）：%+v", got)
	}

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"缺 id", url.Values{"dir": {"up"}}},
		{"缺 dir", url.Values{"id": {"nav-1"}}},
		{"空 id", url.Values{"id": {""}, "dir": {"up"}}},
		{"空体", url.Values{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.moveCalls)
			rec := postMoveForm(t, router, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.moveCalls) != before {
				t.Fatal("形状被拒时不许走到 SwapMove")
			}
		})
	}
}

func TestNavigationUpdateRouterRegistersSplitEndpoints(t *testing.T) {
	raw, err := os.ReadFile("navigation_router.go")
	if err != nil {
		t.Fatalf("读 navigation_router.go 失败: %v", err)
	}
	src := string(raw)
	for _, want := range []string{
		`g.POST("/update", navigationenums.NavigationUpdate, pageview.HTMLFragment, handle.Update)`,
		`g.POST("/move", navigationenums.NavigationMove, pageview.HTMLFragment, handle.Move)`,
		`g.POST("/translations", navigationenums.NavigationTranslate, pageview.HTMLFragment, handle.UpdateTranslations)`,
		`g.POST("/sources", navigationenums.NavigationSourceAdd, pageview.HTMLFragment, handle.CreateFromSource)`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("路由必须独立注册：缺少 %s", want)
		}
	}
}
