package builtin

// TestSiteRedirectMiddleware 访问面重定向中间件（真实文件系统 + 真实激活链接）。
//
// 这是「改 URL 时勾选保留旧链接」能不能兑现的判据：没有它，旧路径是 404，
// 复选框形同虚设。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
)

// setupRedirectFixture 在临时产物根下落一份页面产物与一份重定向产物，并各自激活到
// /plain-path 与 /old-path（后者 301 指向 /new-path）。
func setupRedirectFixture(t *testing.T) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	store := &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()}
	pub := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}

	page, err := pipeline.NewArtifact([]byte("<html>plain</html>"), &pipeline.Manifest{
		ManifestSchemaVersion: 1, PageDocumentSchemaVersion: 1,
		SourceID: "p1", SourceType: pipeline.SourceTypePage,
		CanonicalPath: "/plain-path", SourceHash: "h", BuildInputHash: "h",
	})
	if err != nil {
		t.Fatalf("构造页面产物失败: %v", err)
	}
	loc, err := store.PutArtifact(page)
	if err != nil {
		t.Fatalf("落盘页面产物失败: %v", err)
	}
	if err = pub.Activate("/plain-path", loc); err != nil {
		t.Fatalf("激活页面产物失败: %v", err)
	}

	ra, err := pipeline.NewRedirectArtifact("/new-path", 301)
	if err != nil {
		t.Fatalf("构造重定向产物失败: %v", err)
	}
	rloc, err := store.PutRedirect(ra)
	if err != nil {
		t.Fatalf("落盘重定向产物失败: %v", err)
	}
	if err = pub.Activate("/old-path", rloc); err != nil {
		t.Fatalf("激活重定向产物失败: %v", err)
	}
}

// redirectTestRouter 访问面替身：中间件 + 一条兜底路由代表静态文件服务。
func redirectTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group(siteFacePrefix, SiteRedirectMiddleware())
	g.GET("/*rest", func(c *gin.Context) { c.String(http.StatusOK, "static-face") })
	return r
}

// TestSiteRedirectMiddleware 命中重定向 → 301；其余请求一律放行给静态面。
func TestSiteRedirectMiddleware(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantLoc    string
	}{
		// 命中的旧路径：301 到新路径（含带尾斜杠的目录访问形式）。
		{"旧路径 301", "/site/old-path", http.StatusMovedPermanently, "/new-path"},
		{"旧路径带尾斜杠 301", "/site/old-path/", http.StatusMovedPermanently, "/new-path"},
		// 普通页面产物：放行给静态面，不改行为。
		{"普通页面放行", "/site/plain-path/", http.StatusOK, ""},
		// 未激活路径：放行（由静态面决定 404）。
		{"未激活路径放行", "/site/never-activated/", http.StatusOK, ""},
		// /index 语言根别名 301 到 /（I18N-022）。
		{"index 别名 301", "/site/index", http.StatusMovedPermanently, "/"},
		{"index 带尾斜杠 301", "/site/index/", http.StatusMovedPermanently, "/"},
		// 路径越界：必须放行，而不是替攻击者读出激活目录之外的重定向指令。
		{"越界路径放行", "/site/../site/old-path", http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupRedirectFixture(t)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			redirectTestRouter().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("%s 状态码应为 %d，实际 %d", tc.path, tc.wantStatus, w.Code)
			}
			if tc.wantLoc != "" && w.Header().Get("Location") != tc.wantLoc {
				t.Fatalf("%s Location 应为 %q，实际 %q", tc.path, tc.wantLoc, w.Header().Get("Location"))
			}
		})
	}
}
