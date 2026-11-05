package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func withCleanRegistry(t *testing.T) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
}

func TestDeclareRejectsSecondAPIPathForSamePerm(t *testing.T) {
	withCleanRegistry(t)
	const perm = "product:update"
	Declare(http.MethodPost, "/api/__test__/perm-a", perm, RouteProtected)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("同一权限点绑定两条 API 路由应当 panic，实际放行了")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, perm) {
			t.Fatalf("panic 文案要指出权限点 %q，实际：%s", perm, msg)
		}
		if !strings.Contains(msg, "/api/__test__/perm-a") || !strings.Contains(msg, "/api/__test__/perm-b") {
			t.Fatalf("panic 文案要同时列出两条路径，实际：%s", msg)
		}
	}()

	Declare(http.MethodPost, "/api/__test__/perm-b", perm, RouteProtected)
}

func TestDeclareAllowsPageRouteReusingAPIPerm(t *testing.T) {
	withCleanRegistry(t)
	const perm = "product:update"
	Declare(http.MethodPost, "/api/__test__/perm-c", perm, RouteProtected)
	Declare(http.MethodGet, "/admin/__test__/perm-page", perm, RoutePage)
}

func TestDeclareRejectsConflictingExemption(t *testing.T) {
	withCleanRegistry(t)
	const perm = "product:update"
	Declare(http.MethodPost, "/api/__test__/exempt-a", perm, RouteProtected)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("同一路由既声明权限又声明豁免应当 panic")
		}
	}()
	Declare(http.MethodPost, "/api/__test__/exempt-a", "", RouteExempt)
}

func TestRouteGroupDeclaresActualPaths(t *testing.T) {
	withCleanRegistry(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := NewRouteGroup(engine.Group("/api"), RouteProtected)
	group.POST("/thing", "thing:create", func(c *gin.Context) { c.Status(http.StatusOK) })

	got := RoutesOf("thing:create")
	if len(got) != 1 || got[0].Method != http.MethodPost || got[0].Path != "/api/thing" {
		t.Fatalf("登记必须保留真实 METHOD+路径：%+v", got)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/thing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("路由应真实注册，实际 %d", rec.Code)
	}
}
