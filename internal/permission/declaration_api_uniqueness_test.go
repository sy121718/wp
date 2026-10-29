package permission

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// snapshotReg 保存并恢复声明注册表。
//
// Declare 写的是包级全局状态，而真实装配会在同一进程里再跑一次 —— 测试留下的
// 「__test__」路径若不清理，后面的用例会撞上「同一权限点绑定到多条 API 路由」。
func snapshotReg(t *testing.T) {
	t.Helper()
	regMu.Lock()
	nDeclared := len(declared)
	nExempt := len(exemptRoutes)
	keys := make(map[string]Perm, len(declaredKeys))
	for k, v := range declaredKeys {
		keys[k] = v
	}
	paths := make(map[Perm]string, len(apiPermPaths))
	for k, v := range apiPermPaths {
		paths[k] = v
	}
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		defer regMu.Unlock()
		declared = declared[:nDeclared]
		exemptRoutes = exemptRoutes[:nExempt]
		declaredKeys = keys
		apiPermPaths = paths
	})
}

// TestDeclareRejectsSecondAPIPathForSamePerm 同一权限点不得绑定两条 API 路由。
//
// 判据来自生产故障形态：sys_permission 上 permission_code 唯一，超管策略由它逐行生成，
// 而 Casbin 按实际请求路径 enforce —— 第二个路径永远拿不到策略，端点对所有人 403。
func TestDeclareRejectsSecondAPIPathForSamePerm(t *testing.T) {
	snapshotReg(t)
	Declare("POST", "/api/__test__/perm-a", ProductUpdate)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("同一权限点绑定两条 API 路由应当 panic，实际放行了")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, string(ProductUpdate)) {
			t.Fatalf("panic 文案要指出权限点 %q，实际：%s", ProductUpdate, msg)
		}
		if !strings.Contains(msg, "/api/__test__/perm-a") || !strings.Contains(msg, "/api/__test__/perm-b") {
			t.Fatalf("panic 文案要同时列出两条路径，实际：%s", msg)
		}
	}()

	Declare("POST", "/api/__test__/perm-b", ProductUpdate)
}

// TestDeclareAllowsPageRouteReusingAPIPerm 页面路由复用同一权限点是合法的。
//
// 页面 GET 走 adminui 的 PageAuthz（RoutesOf 逐条 Enforce），不依赖 sys_permission 的单行路径。
func TestDeclareAllowsPageRouteReusingAPIPerm(t *testing.T) {
	snapshotReg(t)
	Declare("POST", "/api/__test__/perm-c", ProductUpdate)
	Declare("GET", "/admin/__test__/perm-page", ProductUpdate)

	if got := RoutesOf(ProductUpdate); len(got) < 2 {
		t.Fatalf("RoutesOf 应返回两条路由，实际 %d", len(got))
	}
}

// TestDeclareIdempotentForSameRoute 同一路由重复声明同一权限点是幂等的（装配多次是常态）。
func TestDeclareIdempotentForSameRoute(t *testing.T) {
	snapshotReg(t)
	Declare("POST", "/api/__test__/perm-d", ProductUpdate)
	Declare("POST", "/api/__test__/perm-d", ProductUpdate)
}

// 显式访客登记只增加审计事实，不能绕过原有会话、CSRF或归属中间件。
func TestExemptRouteKeepsMiddlewareAndExactMethod(t *testing.T) {
	snapshotReg(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := NewRouteGroup(router.Group("/api/site/exempt-audit", func(c *gin.Context) {
		if c.GetHeader("X-Visitor-CSRF") != "valid" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}))
	own := api.Group("", func(c *gin.Context) {
		if c.GetHeader("X-Visitor-ID") != "owner" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	})
	own.POST("/change", Exempt, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, tc := range []struct {
		csrf, user string
		status     int
	}{
		{"", "", http.StatusForbidden}, {"valid", "", http.StatusUnauthorized}, {"valid", "owner", http.StatusNoContent},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/site/exempt-audit/change", nil)
		req.Header.Set("X-Visitor-CSRF", tc.csrf)
		req.Header.Set("X-Visitor-ID", tc.user)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("访客保护应为%d，实际%d", tc.status, rec.Code)
		}
	}
	want := "POST /api/site/exempt-audit/change"
	found := false
	for _, e := range Exempts() {
		if e == want {
			found = true
		}
		if e == "GET /api/site/exempt-audit/change" {
			t.Fatal("不能因路径相同而豁免未登记方法")
		}
	}
	if !found {
		t.Fatal("真实注册的访客路由必须进入精确审计名单")
	}
	for _, spec := range Snapshot() {
		if spec.Path == "/api/site/exempt-audit/change" {
			t.Fatal("访客路由不生成后台权限点")
		}
	}
}
