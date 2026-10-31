package shell

import (
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/pkg/safepath"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

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

func jumpData(t *testing.T, cap *captureRenderer) map[string]any {
	t.Helper()
	m, ok := cap.data.(gin.H)
	if !ok {
		t.Fatalf("渲染数据不是 gin.H：%T", cap.data)
	}
	jv, ok := m["Jump"].(adminui.JumpView)
	if !ok {
		t.Fatalf("Jump 不是 adminui.JumpView：%T", m["Jump"])
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

	if got["title"] != "已删除 2 张优惠码" {
		t.Fatalf("title 期望用正文，实际 %v", got["title"])
	}
}

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

func TestRenderJumpRejectsUnsafeBack(t *testing.T) {
	for _, bad := range []string{"", "//evil.example.com/x", "https://evil.example.com", "admin/x"} {
		_, cap := runJump(t, nil, Jump{OK: true, Msg: "已删除", Back: bad})
		got := jumpData(t, cap)
		if got["Back"] != adminHomePath {
			t.Fatalf("回跳地址 %q 应当收敛成 %q，实际 %v", bad, adminHomePath, got["Back"])
		}
	}
}

func TestRenderJumpKeepsLongSameOriginBack(t *testing.T) {
	longBack := "/admin/coupons?filter=" + strings.Repeat("a", safepath.MaxReturnPathBytes+50)
	if len(longBack) <= safepath.MaxReturnPathBytes {
		t.Fatalf("用例构造有误：%d 字节并未超过 %d", len(longBack), safepath.MaxReturnPathBytes)
	}
	longUnsafe := "//evil.example.com/x?q=" + strings.Repeat("a", safepath.MaxReturnPathBytes+50)

	t.Run("整页 HTML 档原样保留", func(t *testing.T) {
		_, cap := runJump(t, nil, Jump{OK: true, Msg: "已保存", Back: longBack})
		if got := jumpData(t, cap); got["Back"] != longBack {
			t.Fatalf("超 512 字节的同源回跳地址应原样保留，实际 %v（长度 %d）", got["Back"], len(longBack))
		}
	})

	t.Run("htmx 档 HX-Redirect 原样保留", func(t *testing.T) {
		rec, cap := runJump(t, map[string]string{"HX-Request": "true"},
			Jump{OK: true, Msg: "已保存", Back: longBack})
		if cap.called {
			t.Fatal("htmx 档不应渲染整页模板")
		}
		if got := rec.Header().Get("HX-Redirect"); got != longBack {
			t.Fatalf("HX-Redirect 应原样保留超长同源地址，实际 %q（长度 %d）", got, len(got))
		}
	})

	t.Run("长但非法的路径仍被拒绝", func(t *testing.T) {
		_, cap := runJump(t, nil, Jump{OK: true, Msg: "已保存", Back: longUnsafe})
		if got := jumpData(t, cap); got["Back"] != adminHomePath {
			t.Fatalf("协议相对跳转即使很长也必须回 %q，实际 %v", adminHomePath, got["Back"])
		}
		rec, _ := runJump(t, map[string]string{"HX-Request": "true"},
			Jump{OK: true, Msg: "已保存", Back: longUnsafe})
		if got := rec.Header().Get("HX-Redirect"); got != adminHomePath {
			t.Fatalf("htmx 档的 HX-Redirect 也应回 %q，实际 %q", adminHomePath, got)
		}
	})
}

func TestRenderJumpEmptyMsgFallsBack(t *testing.T) {
	_, cap := runJump(t, nil, Jump{OK: false, Msg: "   ", Back: "/admin/coupons"})
	got := jumpData(t, cap)
	msg, _ := got["Msg"].(string)
	if msg == "" {
		t.Fatal("空文案应回落归口文案")
	}
}

func TestRenderJumpNegativeSecondsClamped(t *testing.T) {
	_, cap := runJump(t, nil, Jump{OK: true, Msg: "已保存", Back: "/admin/coupons", Seconds: -3})
	if got := jumpData(t, cap); got["Seconds"] != 0 {
		t.Fatalf("负数秒数应收敛成 0，实际 %v", got["Seconds"])
	}
}
