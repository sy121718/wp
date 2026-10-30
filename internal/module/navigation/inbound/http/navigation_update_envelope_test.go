package navigationhttp

// navigation_update_envelope_test.go — POST /api/navigation/update 的**路由协议**契约
// （UpdateEnvelope 接线之后）。
//
// 与既有三份写测试的分工：navigation_translation_write_test.go / navigation_move_write_test.go
// 钉的是各分支的形状白名单与错误码映射，本文件钉的是**入口本身**——
//   - 绑定只有一处（binding.JSONLimited[UpdateEnvelope]，1 MiB），且**请求体只读一次**：
//     handler 里不再有 navigationWriteBody / json.Unmarshal 的第二段解析；
//   - 判别在 dto 的 Route()：legacy / translations / move 三条正路，其余一律 400；
//   - 两种投放（JSON 与 urlencoded/multipart 表单）走同一条入口：legacy 的宽容与
//     binding:"required" 在两处都成立，translations 的行数组在表单里是 JSON 字符串；
//   - 超过 1 MiB 在解析之前拒绝。
//
// 全部无库：形状判定必须发生在碰数据库与写入口之前；译文分支的「未注入句柄 = 500」
// 正是「形状已放行」的观测点（与 navigation_translation_write_test.go 同一口径）。

import (
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
)

// envelopeNavigationStub 记录 Update / SwapMove 的调用（译文分支不落 service，
// 走的是包级写句柄）。
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

// newEnvelopeRouter 只挂 Update（三条分支共用这一个入口，正是本文件要证的事）。
func newEnvelopeRouter(svc navigationcontract.NavigationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", NewHandle(svc).Update)
	return router
}

// envelopeJSON 造 JSON 投放（contentType 为空 = 不设 Content-Type）。
func envelopeJSON(t *testing.T, router *gin.Engine, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// envelopeForm 造 urlencoded 投放。
func envelopeForm(t *testing.T, router *gin.Engine, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// envelopeMultipart 造 multipart 投放（字段顺序固定，便于复现）。
func envelopeMultipart(t *testing.T, router *gin.Engine, order []string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	for _, key := range order {
		if err := mw.WriteField(key, values.Get(key)); err != nil {
			t.Fatalf("写 multipart 字段 %s 失败: %v", key, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestNavigationUpdateLegacyThroughJSONAndForm legacy 档在 JSON 与表单两条路上都保持迁移前语义。
//
// 宽容：未知字段仍被忽略（不借这次迁移收紧旧契约）；严格：binding:"required" 的 id
// 仍在（DecodeLegacy 走的是 gin 的 binding，不是裸 json.Unmarshal）。
func TestNavigationUpdateLegacyThroughJSONAndForm(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(stub)

	// JSON：未知字段 + 指针字段半填。
	rec := envelopeJSON(t, router, "application/json", `{"id":"nav-1","title":"首页","unknownField":123}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy JSON（含未知字段）应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.updateCalls) != 1 {
		t.Fatalf("legacy 应调用一次 service.Update，实际 %d", len(stub.updateCalls))
	}
	got := stub.updateCalls[0]
	if got.ID != "nav-1" || got.Title == nil || *got.Title != "首页" || got.Path != nil {
		t.Fatalf("legacy 绑定结果被改动：%+v", got)
	}

	// 表单：同一字段集，走 BindForm → DecodeLegacy 的同一次解析。
	values := url.Values{"id": {"nav-2"}, "title": {"关于"}}
	rec = envelopeForm(t, router, values)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy 表单应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.updateCalls) != 2 {
		t.Fatalf("legacy 表单应再调用一次 service.Update，实际 %d", len(stub.updateCalls))
	}
	if got := stub.updateCalls[1]; got.ID != "nav-2" || got.Title == nil || *got.Title != "关于" {
		t.Fatalf("legacy 表单绑定结果不对：%+v", got)
	}

	// multipart：与 urlencoded 同一档。
	multi := url.Values{"id": {"nav-3"}, "sortOrder": {"7"}}
	rec = envelopeMultipart(t, router, []string{"id", "sortOrder"}, multi)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy multipart 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	last := stub.updateCalls[len(stub.updateCalls)-1]
	if last.ID != "nav-3" || last.SortOrder == nil || *last.SortOrder != 7 {
		t.Fatalf("legacy multipart 绑定结果不对（sortOrder 必须还原成数字）：%+v", last)
	}

	// required 在两条路上都成立。
	for name, call := range map[string]func() *httptest.ResponseRecorder{
		"JSON 缺 id": func() *httptest.ResponseRecorder {
			return envelopeJSON(t, router, "application/json", `{"title":"没有 id"}`)
		},
		"表单缺 id": func() *httptest.ResponseRecorder {
			return envelopeForm(t, router, url.Values{"title": {"没有 id"}})
		},
		"JSON 空 id": func() *httptest.ResponseRecorder {
			return envelopeJSON(t, router, "application/json", `{"id":""}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(stub.updateCalls)
			rec := call()
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.updateCalls) != before {
				t.Fatal("required 不满足时不许走到 service")
			}
		})
	}
}

// TestNavigationUpdateRejectedShapes 判别键写坏、未知标签、混形：一律 400 且不落任何分支。
func TestNavigationUpdateRejectedShapes(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(stub)

	cases := []struct {
		name string
		body string
	}{
		{"action 为 null", `{"action":null,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为数字", `{"action":5}`},
		{"action 为布尔", `{"action":true}`},
		{"action 为空串", `{"action":"","id":"nav-1"}`},
		{"action 纯空白", `{"action":"   "}`},
		{"action 未知值", `{"action":"delete","id":"nav-1"}`},
		{"混形 rows", `{"id":"nav-1","rows":[]}`},
		{"混形 project", `{"id":"nav-1","project":"p-1"}`},
		{"混形 lang", `{"id":"nav-1","lang":"en-US"}`},
		{"混形 dir", `{"id":"nav-1","dir":"up"}`},
		{"混形 projectId", `{"id":"nav-1","projectId":"p-1"}`},
		{"JSON 不是对象", `[]`},
		{"JSON 非法", `{`},
		{"空体", ``},
		{"显式 null", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeUpdate, beforeMove := len(stub.updateCalls), len(stub.moveCalls)
			rec := envelopeJSON(t, router, "application/json", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.updateCalls) != beforeUpdate || len(stub.moveCalls) != beforeMove {
				t.Fatal("形状被拒时不许落到任何分支")
			}
		})
	}
}

// TestNavigationUpdateTranslationsEnvelope 显式译文分支经入口后仍按原 helper 的判据走。
//
// 三档观测点：
//   - 合法形状 + 句柄未注入 → 500（穿过形状层、被 fail closed 挡下）；
//   - 形状错（顶层未知键 / 行缺 target）→ 400；
//   - JSON 与表单（rows 是 JSON 字符串）走同一条路。
func TestNavigationUpdateTranslationsEnvelope(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(stub)

	row := `{"context":"navigation.label","hash":"h-1","target":"Home"}`
	validJSON := `{"action":"translations","project":"p-1","lang":"en-US","rows":[` + row + `]}`
	if rec := envelopeJSON(t, router, "application/json", validJSON); rec.Code != http.StatusInternalServerError {
		t.Fatalf("合法形状 + 未注入句柄应 500（fail closed），实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 表单：写基座把 rows 整数组 JSON.stringify 成字符串，这一层由入口剥掉。
	formValues := url.Values{
		"action":  {"translations"},
		"project": {"p-1"},
		"lang":    {"en-US"},
		"rows":    {"[" + row + "]"},
	}
	if rec := envelopeForm(t, router, formValues); rec.Code != http.StatusInternalServerError {
		t.Fatalf("表单译文分支应穿过形状层（500 fail closed），实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := envelopeMultipart(t, router,
		[]string{"action", "project", "lang", "rows"}, formValues); rec.Code != http.StatusInternalServerError {
		t.Fatalf("multipart 译文分支应穿过形状层（500 fail closed），实际 %d body=%s", rec.Code, rec.Body.String())
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
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.updateCalls)
			rec := envelopeJSON(t, router, "application/json", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.updateCalls) != before {
				t.Fatal("译文分支不许落到导航项更新上")
			}
		})
	}

	// 译文分支的任何形态都不许落到 service.Update。
	if len(stub.updateCalls) != 0 {
		t.Fatalf("译文分支调用了 %d 次 service.Update", len(stub.updateCalls))
	}
}

// TestNavigationUpdateMoveEnvelope 显式 move 分支：请求字段逐字到 SwapMove，形状错 400。
func TestNavigationUpdateMoveEnvelope(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(stub)

	rec := envelopeJSON(t, router, "application/json", `{"action":"move","id":"nav-1","dir":"up","projectId":"p-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 move 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.moveCalls) != 1 {
		t.Fatalf("move 应调用一次 SwapMove，实际 %d", len(stub.moveCalls))
	}
	if got := stub.moveCalls[0]; got.ID != "nav-1" || got.Dir != "up" || got.ProjectID != "p-1" {
		t.Fatalf("move 字段没有逐字送达：%+v", got)
	}

	// 表单投放同一档。
	if rec := envelopeForm(t, router, url.Values{"action": {"move"}, "id": {"nav-2"}, "dir": {"down"}}); rec.Code != http.StatusOK {
		t.Fatalf("表单 move 应 200，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if got := stub.moveCalls[len(stub.moveCalls)-1]; got.ID != "nav-2" || got.Dir != "down" || got.ProjectID != "" {
		t.Fatalf("表单 move 字段不对（projectId 缺省 = 不校验归属）：%+v", got)
	}

	rejects := []struct {
		name string
		body string
	}{
		{"缺 id", `{"action":"move","dir":"up"}`},
		{"缺 dir", `{"action":"move","id":"nav-1"}`},
		{"未知键 kind", `{"action":"move","id":"nav-1","dir":"up","kind":"header"}`},
		{"id 为 null", `{"action":"move","id":null,"dir":"up"}`},
		{"projectId 为 null", `{"action":"move","id":"nav-1","dir":"up","projectId":null}`},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.moveCalls)
			rec := envelopeJSON(t, router, "application/json", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.moveCalls) != before {
				t.Fatal("形状被拒时不许走到 SwapMove")
			}
		})
	}
}

// TestNavigationUpdateBodyLimit 超过 1 MiB 在解析之前拒绝（任何分支都不落）。
func TestNavigationUpdateBodyLimit(t *testing.T) {
	stub := &envelopeNavigationStub{}
	router := newEnvelopeRouter(stub)

	huge := `{"id":"nav-1","pad":"` + strings.Repeat("x", navigationWriteMaxBodyBytes) + `"}`
	rec := envelopeJSON(t, router, "application/json", huge)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 字节应 400，实际 %d", navigationWriteMaxBodyBytes, rec.Code)
	}
	if len(stub.updateCalls) != 0 || len(stub.moveCalls) != 0 {
		t.Fatal("超限请求不许落到任何分支")
	}
}

// TestNavigationUpdateRouterIsSingleEntrypoint 路由只有一条：POST /api/navigation/update。
func TestNavigationUpdateRouterIsSingleEntrypoint(t *testing.T) {
	raw, err := os.ReadFile("navigation_router.go")
	if err != nil {
		t.Fatalf("读 navigation_router.go 失败: %v", err)
	}
	if !strings.Contains(string(raw), `g.POST("/update", permission.NavigationUpdate, handle.Update)`) {
		t.Fatal("Update 必须挂在 POST /update 的唯一注册点上")
	}
}
