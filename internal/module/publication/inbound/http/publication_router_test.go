package pubhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_wp/internal/middleware/builtin"
	pubhttp "go_wp/internal/module/publication/inbound/http"
	"go_wp/internal/permission"
	"go_wp/pkg/auth"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func TestSEOAuditRequiresBothExistingPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatal(err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ code, path string }{
		{"publication:seo_audit", "/api/publication/seo-audit"},
		{"seo:audit", "/api/seo/audit"},
	} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_permission WHERE permission_code = ? AND api_path = ? AND api_method = 'POST' AND status = 1", item.code, item.path).Scan(&n).Error; err != nil || n != 1 {
			t.Fatalf("权限点 %s 路径 %s 缺失: count=%d err=%v", item.code, item.path, n, err)
		}
	}
	if err := pkgcasbin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })

	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("本地 Redis 不可用: %v", err)
	}
	const subject = "987654321"
	sessionID, err := auth.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveUserSession(context.Background(), &auth.UserSession{
		ID: 987654321, SessionID: sessionID, Username: "seo-audit-role", Status: 1,
	}, time.Minute); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.DeleteUserSession(context.Background(), 987654321) })
	cookieEngine := gin.New()
	cookieEngine.GET("/session", func(c *gin.Context) {
		if err := auth.SaveCookieSession(c, &auth.CookieSession{SessionID: sessionID}, false); err != nil {
			t.Error(err)
		}
	})
	cookieWriter := httptest.NewRecorder()
	cookieEngine.ServeHTTP(cookieWriter, httptest.NewRequest(http.MethodGet, "/session", nil))
	cookie := cookieWriter.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("测试会话 Cookie 创建失败")
	}
	r := gin.New()
	api := permission.NewRouteGroup(r.Group("/api", func(c *gin.Context) {
		c.Set("user_id", int64(987654321))
	}, builtin.CasbinMiddleware()))
	pubhttp.SetupPublicationRoutes(api, db)
	var declared bool
	for _, spec := range permission.Snapshot() {
		if spec.Path == "/api/publication/seo-audit" && spec.Method == http.MethodPost && spec.Perm == permission.PublicationSEOAudit {
			declared = true
		}
	}
	if !declared {
		t.Fatal("SEO 体检路由没有声明真实 publication 权限点")
	}
	request := func() int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/publication/seo-audit", strings.NewReader("project="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie[0])
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if status := request(); status != http.StatusForbidden {
		t.Fatalf("无权限用户得到 %d", status)
	}
	enforcer := pkgcasbin.GetEnforcer()
	if _, err := enforcer.AddPolicy(subject, "/api/publication/seo-audit", "POST", "publication:seo_audit"); err != nil {
		t.Fatal(err)
	}
	if status := request(); status != http.StatusForbidden {
		t.Fatalf("仅 publication 权限用户得到 %d", status)
	}
	if _, err := enforcer.RemovePolicy(subject, "/api/publication/seo-audit", "POST", "publication:seo_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddPolicy(subject, "/api/seo/audit", "POST", "seo:audit"); err != nil {
		t.Fatal(err)
	}
	if status := request(); status != http.StatusForbidden {
		t.Fatalf("仅 SEO 权限用户得到 %d", status)
	}
	if _, err := enforcer.AddPolicy(subject, "/api/publication/seo-audit", "POST", "publication:seo_audit"); err != nil {
		t.Fatal(err)
	}
	if status := request(); status != http.StatusBadRequest {
		t.Fatalf("同时拥有两个权限但缺参数的用户得到 %d，预期进入业务 handler", status)
	}
}
