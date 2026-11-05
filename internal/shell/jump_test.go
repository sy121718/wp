package shell

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/view"
)

func jumpEngine(t *testing.T, j Jump) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../templates", true)
	engine.Use(view.FailureMiddleware(adminui.RenderFailure))
	engine.Use(view.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/probe", view.HTMLPage, func(c *gin.Context) { RenderJump(c, j) })
	return engine
}

func runJump(t *testing.T, j Jump) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	jumpEngine(t, j).ServeHTTP(rec, req)
	return rec
}

func TestRenderJumpSuccessUsesSharedFeedback(t *testing.T) {
	rec := runJump(t, Jump{OK: true, Msg: "已删除 2 张优惠码", Back: "/admin/coupons?project=p1", Seconds: 1})

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码期望 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功必须是共享提示组件：%s", body)
	}
	if !strings.Contains(body, "已删除 2 张优惠码") {
		t.Fatalf("成功消息应原样出现：%s", body)
	}
	if strings.Contains(body, "meta http-equiv") || strings.Contains(body, "jump-page") {
		t.Fatalf("不得再输出整页跳转提示或 meta refresh：%s", body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("成功不得使用后端导航头，实际 %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("成功提示不能缓存，Cache-Control=%q", got)
	}
}

func TestRenderJumpSuccessDefaultsToOperationSuccess(t *testing.T) {
	rec := runJump(t, Jump{OK: true})
	if !strings.Contains(rec.Body.String(), "操作成功") {
		t.Fatalf("无消息时默认“操作成功”，实际：%s", rec.Body.String())
	}
}

func TestRenderJumpFailureUsesSharedFailureOutlet(t *testing.T) {
	rec := runJump(t, Jump{Msg: "删除失败：存在关联数据", Back: "/admin"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("业务失败必须给真实 4xx，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "jump-page") || !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("失败必须走共享失败出口的 HTML 错误页：%s", body)
	}
	if strings.Contains(body, "jump-page") {
		t.Fatalf("失败不得走整页跳转提示：%s", body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("失败不得使用后端导航头，实际 %q", got)
	}
}

func TestRenderJumpFailureWithoutMessageIsInternal(t *testing.T) {
	rec := runJump(t, Jump{})
	if rec.Code < 400 {
		t.Fatalf("无消息失败不能是成功状态，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "jump-page") {
		t.Fatalf("不得回落到整页跳转提示")
	}
}

func TestSafeBackKeepsSameOriginAndRejectsOthers(t *testing.T) {
	c := &gin.Context{}
	if got := SafeBack(c, "/admin/pages?a=1", "/admin"); got != "/admin/pages?a=1" {
		t.Fatalf("同源相对路径应保留，实际 %q", got)
	}
	for _, bad := range []string{"https://evil.example/x", "//evil.example/x", "javascript:alert(1)", ""} {
		if got := SafeBack(c, bad, "/admin"); got != "/admin" {
			t.Fatalf("不安全回跳 %q 应回落 /admin，实际 %q", bad, got)
		}
	}
}
