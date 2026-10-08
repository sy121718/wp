package shell

// jump_test.go — 提示页出口（jump.go）的判据。
//
// 本文件测的是**出口的行为**（状态码、渲染的模板、传下去的数据、htmx 分档），
// 模板自身的渲染判据在 internal/templates/jump_page_test.go —— 两边分工的理由是
// 模板要从 templates 目录解析，而这里只需要一个能截住渲染数据的假渲染器。
//
// 走真实的 gin 引擎 + 路由（而不是直接调 c.HTML）：HTMLRender 是引擎上的字段，
// 从 *gin.Context 拿不到它（engine 字段未导出）—— 顺带也就覆盖了 c.HTML 这条真路径。
//
// 守四件事：
//  1. htmx 请求**不能**把整页 HTML 塞回片段位置（换 HX-Redirect），这是分档的硬要求；
//  2. 回跳地址非法时回控制面首页（提示页不能当开放重定向的跳板）；
//  3. 空文案回落归口文案（页面不能显示空串）；
//  4. 秒数负数收敛成 0（负数会拼出 content="-1;url=..." 这种坏 meta）。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

// captureRenderer 截住 gin 的渲染调用（模板名 + 数据），不做真实渲染。
type captureRenderer struct {
	called bool
	name   string
	data   any
}

func (r *captureRenderer) Instance(name string, data any) render.Render {
	r.called = true
	r.name = name
	r.data = data
	return discardRender{}
}

type discardRender struct{}

func (discardRender) Render(w http.ResponseWriter) error     { return nil }
func (discardRender) WriteContentType(_ http.ResponseWriter) {}

// runJump 在真实 gin 引擎上跑一次 RenderJump，返回响应记录器与渲染捕获器。
func runJump(t *testing.T, headers map[string]string, j Jump) (*httptest.ResponseRecorder, *captureRenderer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cap := &captureRenderer{}
	engine := gin.New()
	engine.HTMLRender = cap
	engine.POST("/probe", func(c *gin.Context) { RenderJump(c, j) })

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec, cap
}

// jumpData 取出渲染数据里的 Jump 视图。
func jumpData(t *testing.T, cap *captureRenderer) map[string]any {
	t.Helper()
	m, ok := cap.data.(gin.H)
	if !ok {
		t.Fatalf("渲染数据不是 gin.H：%T", cap.data)
	}
	jv, ok := m["Jump"].(jumpView)
	if !ok {
		t.Fatalf("Jump 不是 jumpView：%T", m["Jump"])
	}
	return map[string]any{
		"title":    m["title"],
		"OK":       jv.OK,
		"Msg":      jv.Msg,
		"Back":     jv.Back,
		"BackText": jv.BackText,
		"Seconds":  jv.Seconds,
	}
}

// TestRenderJumpSuccess 正常档：200 + 提示页模板 + 数据齐备。
func TestRenderJumpSuccess(t *testing.T) {
	rec, cap := runJump(t, nil, Jump{
		OK: true, Msg: "已删除 2 张优惠码",
		Back: "/admin/coupons?project=p1", BackText: "返回优惠码列表", Seconds: 1,
	})

	if !cap.called {
		t.Fatal("未调用渲染")
	}
	if cap.name != "admin/jump.html" {
		t.Fatalf("渲染的模板不对：%s", cap.name)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码期望 200，实际 %d", rec.Code)
	}
	got := jumpData(t, cap)
	if got["OK"] != true || got["Msg"] != "已删除 2 张优惠码" {
		t.Fatalf("OK/Msg 不对：%v", got)
	}
	if got["Back"] != "/admin/coupons?project=p1" || got["BackText"] != "返回优惠码列表" || got["Seconds"] != 1 {
		t.Fatalf("回跳/秒数不对：%v", got)
	}
	// 标题用正文本身（不新增全站词条），且必须是成品文案而不是 key。
	if got["title"] != "已删除 2 张优惠码" {
		t.Fatalf("title 期望用正文，实际 %v", got["title"])
	}
}

// TestRenderJumpHTMXUsesRedirect htmx 档：换 HX-Redirect，不渲染整页。
func TestRenderJumpHTMXUsesRedirect(t *testing.T) {
	rec, cap := runJump(t, map[string]string{"HX-Request": "true"},
		Jump{OK: true, Msg: "已删除", Back: "/admin/coupons?project=p1", Seconds: 1})

	if cap.called {
		t.Fatal("htmx 档不应渲染整页模板")
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/admin/coupons?project=p1" {
		t.Fatalf("HX-Redirect 期望回跳地址，实际 %q", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码期望 200，实际 %d", rec.Code)
	}
}

// TestRenderJumpRejectsUnsafeBack 非法回跳地址回控制面首页。
func TestRenderJumpRejectsUnsafeBack(t *testing.T) {
	for _, bad := range []string{"", "//evil.example.com/x", "https://evil.example.com", "admin/x"} {
		_, cap := runJump(t, nil, Jump{OK: true, Msg: "已删除", Back: bad})
		got := jumpData(t, cap)
		if got["Back"] != adminHomePath {
			t.Fatalf("回跳地址 %q 应当收敛成 %q，实际 %v", bad, adminHomePath, got["Back"])
		}
	}
}

// TestRenderJumpEmptyMsgFallsBack 空文案回落归口文案（页面不能显示空串）。
func TestRenderJumpEmptyMsgFallsBack(t *testing.T) {
	_, cap := runJump(t, nil, Jump{OK: false, Msg: "   ", Back: "/admin/coupons"})
	got := jumpData(t, cap)
	msg, _ := got["Msg"].(string)
	if msg == "" {
		t.Fatal("空文案应回落归口文案")
	}
}

// TestRenderJumpNegativeSecondsClamped 负数秒数收敛成 0（否则拼出坏 meta）。
func TestRenderJumpNegativeSecondsClamped(t *testing.T) {
	_, cap := runJump(t, nil, Jump{OK: true, Msg: "已保存", Back: "/admin/coupons", Seconds: -3})
	if got := jumpData(t, cap); got["Seconds"] != 0 {
		t.Fatalf("负数秒数应收敛成 0，实际 %v", got["Seconds"])
	}
}
