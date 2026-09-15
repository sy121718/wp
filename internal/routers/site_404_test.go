package routers

// site_404_test.go — 访问面未知路径的 404 响应（审计 SEO-013 的 verification）。
//
// 两条断言直接对应审计条目的验收口径：
//  1. 配置了自定义 404 页时，未知路径返回那份页面**且状态码仍是 404**；
//  2. 未配置时行为不变（既有的统一 JSON 404）。
//
// 为什么必须在这一层测：gin 的静态文件 handler 在 fs.Open 失败时会把 handler 链
// 换成 NoRoute（v1.12 createStaticHandler），也就是说「访问面 404」的真实出口是
// notFoundHandler —— 只测 pipeline 的文件读写测不到这条链。

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

// doGet 发起一次真实请求（走完整 handler 链）。
func doGet(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// newSiteFaceRouter 按生产形状装配访问面与未匹配路由处理器。
func newSiteFaceRouter() *gin.Engine {
	router := gin.New()
	setupStaticFace(router)
	router.NoRoute(notFoundHandler())
	return router
}

// TestSiteFaceUnknownPathServesCustomNotFound 未知路径返回自定义 404 页，状态码 404。
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
		// 状态码必须是 404：返回 200 是软 404，搜索引擎会把死链当有效页面收录 ——
		// 那正是本条审计要避免的一半问题。
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

// TestSiteFaceNotFoundUnconfiguredKeepsLegacyBehavior 未配置时行为不变。
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
	// 配置后清空 = 回到既有行为（激活目录直接对外服务，旧页面不能继续顶着）。
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

// TestSiteFaceCustomNotFoundScopedToSiteFace 自定义 404 只作用于访问面。
//
// 控制面（/api、/admin）的未匹配路径必须保持统一 JSON —— 后台/接口拿到一段 HTML
// 会让前端解析出错，而且把站点页面文案泄漏给非站点请求。
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
	for _, path := range []string{"/api/ghost", "/admin/ghost", "/siteadmin/ghost"} {
		rec := doGet(router, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 状态码期望 404，实际 %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s 不应命中站点 404 页，实际 Content-Type %q body %q", path, ct, rec.Body.String())
		}
	}
}

// TestSiteFaceActiveArtifactStillServed 回归：自定义 404 不影响已激活产物的正常输出。
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
	// 激活链接形状与 LocalPublicationStore.Activate 一致（相对目标，上溯层数 = 路径段数 + 1）。
	if err := os.Symlink("../../artifacts/hash1", filepath.Join(active, "about")); err != nil {
		t.Fatalf("建激活链接失败: %v", err)
	}
	if err := pipeline.SyncNotFoundPage(active, "<h1>页面不存在</h1>"); err != nil {
		t.Fatalf("写入自定义 404 页失败: %v", err)
	}

	router := newSiteFaceRouter()
	// 刻意不用 /site/index：index 别名规则把它 301 到 "/"（I18N-022，既有行为），
	// 拿它做断言测到的是那条中间件，而不是静态面本身。
	rec := doGet(router, "/site/about/")
	if rec.Code != http.StatusOK {
		t.Fatalf("已激活路径期望 200，实际 %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "home") {
		t.Errorf("已激活路径内容不符: %q", rec.Body.String())
	}
}
