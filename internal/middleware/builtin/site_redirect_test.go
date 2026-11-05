package builtin

import (
	publish "go_wp/pkg/publish"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRedirectFixture(t *testing.T) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	store := &publish.LocalStore{Root: publish.DefaultArtifactRoot()}
	pub := &publish.LocalPublicationStore{ActiveRoot: publish.ActiveRoot()}

	page, err := publish.NewArtifact([]byte("<html>plain</html>"), &publish.Manifest{
		ManifestSchemaVersion: 1, PageDocumentSchemaVersion: 1,
		SourceID: "p1", SourceType: publish.SourceTypePage,
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

	ra, err := publish.NewRedirectArtifact("/new-path", 301)
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

func redirectTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	g := r.Group("/site", SiteRedirectMiddleware("/site"))
	g.GET("/*rest", func(c *gin.Context) { c.String(http.StatusOK, "static-face") })
	return r
}

func TestSiteRedirectMiddleware(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantLoc    string
	}{

		{"旧路径 301", "/site/old-path", http.StatusMovedPermanently, "/new-path"},
		{"旧路径带尾斜杠 301", "/site/old-path/", http.StatusMovedPermanently, "/new-path"},

		{"普通页面放行", "/site/plain-path/", http.StatusOK, ""},

		{"未激活路径放行", "/site/never-activated/", http.StatusOK, ""},

		{"index 别名 301", "/site/index", http.StatusMovedPermanently, "/"},
		{"index 带尾斜杠 301", "/site/index/", http.StatusMovedPermanently, "/"},

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
