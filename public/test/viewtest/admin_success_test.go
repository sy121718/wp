package viewtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/view"
)

func newSuccessEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)
	engine.Use(view.UseSuccess(view.NewSuccessWriter()))
	engine.POST("/ok", func(c *gin.Context) { view.Success(c) })
	engine.POST("/msg", func(c *gin.Context) {
		view.SuccessMessage(c, "", "已保存")
	})
	return engine
}

func TestSuccessWriterEmitsSharedComponent(t *testing.T) {
	engine := newSuccessEngine(t)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/ok", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("成功状态应为 200，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("成功提示不能缓存，Cache-Control=%q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功提示必须是单根状态节点，实际：%s", body)
	}
	if !strings.Contains(body, "操作成功") {
		t.Fatalf("默认成功提示应为“操作成功”，实际：%s", body)
	}
	if strings.Count(body, "操作成功") != 1 {
		t.Fatalf("成功提示只能出现一次，实际：%s", body)
	}
}

func TestSuccessWriterUsesExplicitMessage(t *testing.T) {
	engine := newSuccessEngine(t)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/msg", nil)
	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "已保存") {
		t.Fatalf("自定义消息应原样出现，实际：%s", body)
	}
	if strings.Contains(body, "操作成功") {
		t.Fatalf("自定义消息不应再叠加默认提示，实际：%s", body)
	}
}

func TestSuccessWithoutWriterFailsSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/ok", func(c *gin.Context) { view.Success(c) })

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/ok", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code < 400 {
		t.Fatalf("未装配成功出口时不能静默 200，实际 %d", rec.Code)
	}
}
