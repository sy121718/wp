package routers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
)

func doGet(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func newSiteFaceRouter() *gin.Engine {
	router := gin.New()
	setupStaticFace(router)
	router.NoRoute(notFoundHandler())
	return router
}

func TestSiteFaceUnknownPathServesCustomNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	const page = "<!doctype html><html><body><h1>页面不存在</h1></body></html>"
	if err := pipeline.SyncNotFoundPage(active, page); err != nil {
		t.Fatalf("写入自定义 404 页失败: %v", err)
	}

	router := newSiteFaceRouter()
	for _, path := range []string{"/site/ghost", "/site/a/b/c", "/site/blog/不存在的文章"} {
		rec := doGet(router, path)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 状态码期望 404，实际 %d", path, rec.Code)
		}
		if got := rec.Body.String(); got != page {
			t.Errorf("%s 未返回自定义 404 页，实际响应体 %q", path, got)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("%s Content-Type 期望 text/html，实际 %q", path, ct)
		}
	}
}

func TestSiteFaceNotFoundUnconfiguredKeepsLegacyBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}

	router := newSiteFaceRouter()
	rec := doGet(router, "/site/ghost")
	if rec.Code != http.StatusNotFound {
		t.Errorf("未配置时状态码期望 404，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("未配置时应保持既有统一 JSON 响应，实际 Content-Type %q body %q", ct, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("未配置时不应输出 HTML 页面，实际 body %q", rec.Body.String())
	}

	if err := pipeline.SyncNotFoundPage(active, "<h1>旧页</h1>"); err != nil {
		t.Fatalf("写入 404 页失败: %v", err)
	}
	if err := pipeline.SyncNotFoundPage(active, ""); err != nil {
		t.Fatalf("清空 404 页失败: %v", err)
	}
	rec = doGet(router, "/site/ghost")
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("清空配置后未回到既有响应，实际 Content-Type %q body %q", ct, rec.Body.String())
	}
}

func TestSiteFaceCustomNotFoundScopedToSiteFace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	if err := pipeline.SyncNotFoundPage(active, "<h1>页面不存在</h1>"); err != nil {
		t.Fatalf("写入自定义 404 页失败: %v", err)
	}

	router := newSiteFaceRouter()

	for _, path := range []string{"/api/ghost", "/admin/ghost", "/storage/ghost"} {
		rec := doGet(router, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 状态码期望 404，实际 %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s 不应命中站点 404 页，实际 Content-Type %q body %q", path, ct, rec.Body.String())
		}
	}

	for _, path := range []string{"/siteadmin/ghost", "/ghost", "/a/b/c", "/_fragments/ghost"} {
		rec := doGet(router, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 状态码期望 404，实际 %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("%s 应命中站点 404 页，实际 Content-Type %q body %q", path, ct, rec.Body.String())
		}
	}
}

func TestSiteFaceActiveArtifactStillServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	active := pipeline.ActiveRoot()
	art := filepath.Join(filepath.Dir(filepath.Dir(active)), "artifacts", "hash1")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	if err := os.MkdirAll(art, 0o755); err != nil {
		t.Fatalf("建产物目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(art, "index.html"), []byte("<h1>home</h1>"), 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}

	if err := os.Symlink("../../artifacts/hash1", filepath.Join(active, "about")); err != nil {
		t.Fatalf("建激活链接失败: %v", err)
	}
	if err := pipeline.SyncNotFoundPage(active, "<h1>页面不存在</h1>"); err != nil {
		t.Fatalf("写入自定义 404 页失败: %v", err)
	}

	router := newSiteFaceRouter()

	rec := doGet(router, "/site/about/")
	if rec.Code != http.StatusOK {
		t.Fatalf("已激活路径期望 200，实际 %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "home") {
		t.Errorf("已激活路径内容不符: %q", rec.Body.String())
	}
}
