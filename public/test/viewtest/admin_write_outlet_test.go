package viewtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// outletEngine 复刻生产接线：失败出口、成功出口、模板失败接线与请求取词。
func outletEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)
	engine.Use(view.FailureMiddleware(view.RenderFailure))
	engine.Use(view.UseSuccess(view.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	return engine
}

func TestWriteSuccessUsesSharedFeedback(t *testing.T) {
	engine := outletEngine(t)
	engine.POST("/ok", view.HTMLPage, func(c *gin.Context) {
		view.SuccessMessage(c, "", "已删除 2 张优惠码")
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ok", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码期望 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) || !strings.Contains(body, "已删除 2 张优惠码") {
		t.Fatalf("成功必须走共享提示组件并带原消息：%s", body)
	}
	if strings.Contains(body, "meta http-equiv") || strings.Contains(body, "jump-page") {
		t.Fatalf("不得输出整页跳转提示或 meta refresh：%s", body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("成功不得使用后端导航头，实际 %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("成功提示不能缓存，Cache-Control=%q", got)
	}
}

func TestWriteSuccessDefaultsToOperationSuccess(t *testing.T) {
	engine := outletEngine(t)
	engine.POST("/ok", view.HTMLPage, func(c *gin.Context) { view.Success(c) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ok", nil))
	if !strings.Contains(rec.Body.String(), "操作成功") {
		t.Fatalf("无消息时默认“操作成功”，实际：%s", rec.Body.String())
	}
}

func TestWriteFailureUsesSharedFailureOutlet(t *testing.T) {
	engine := outletEngine(t)
	engine.POST("/bad", view.HTMLPage, func(c *gin.Context) {
		view.FailMessage(c, "page", "", "删除失败：存在关联数据")
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/bad", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("业务失败必须给真实 4xx，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "jump-page") {
		t.Fatalf("失败必须走共享失败出口的 HTML 错误页：%s", body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("失败不得使用后端导航头，实际 %q", got)
	}
}

func TestWriteFailureWithoutMessageIsInternal(t *testing.T) {
	engine := outletEngine(t)
	engine.POST("/bad", view.HTMLPage, func(c *gin.Context) { view.FailMessage(c, "page", "", "") })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/bad", nil))
	if rec.Code < 400 {
		t.Fatalf("无消息失败不能是成功状态，实际 %d", rec.Code)
	}
}
