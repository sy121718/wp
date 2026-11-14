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

func TestFreezeRejectsLateRegistration(t *testing.T) {
	withCleanRegistry(t)
	Declare(http.MethodGet, "/api/__test__/frozen", "frozen:read", RouteProtected)
	Freeze()
	if !Frozen() {
		t.Fatal("Freeze 后 Frozen 应为 true")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("冻结后的迟到注册必须 panic，不能静默追加")
		}
	}()
	Declare(http.MethodGet, "/api/__test__/late", "frozen:write", RouteProtected)
}

func TestEmptyPermRequiresExemptKind(t *testing.T) {
	withCleanRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("空权限配受保护用途必须 panic：否则等于静默公开")
		}
	}()
	Declare(http.MethodGet, "/api/__test__/silent-public", "", RouteProtected)
}

func TestExemptKindRejectsPerm(t *testing.T) {
	withCleanRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("豁免声明携带权限点必须 panic：两种事实自相矛盾")
		}
	}()
	Declare(http.MethodGet, "/api/__test__/exempt-with-perm", "x:read", RouteExempt)
}

// Kind 是登记事实，不从「有没有权限码」推断：漏写必须显式失败，
// 不能静默当成受保护 API 让启动核对看不出登记本身缺项。
func TestDeclareRouteRequiresExplicitKind(t *testing.T) {
	withCleanRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("缺少 Kind 的登记应当 panic")
		}
	}()
	DeclareRoute(RouteSpec{Method: http.MethodGet, Path: "/admin/__test__/page", Perm: "page:read"})
}
