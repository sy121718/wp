package routers

// access_guard_test.go — AccessGuard 与访问面配合的端到端判定（PIPE-6）。
//
// 为什么必须在这一层测：守卫判「哪个条目」、静态面判「取哪个文件」，这两件事
// 分别由 middleware 与 routers 完成。只在 middleware 侧测守卫，测不到
// 「静态面确实会直出受限内容」这一半 —— 而绕过正是从这两者**不一致**的地方长出来的。
//
// 本文件钉住的绕过用例：`GET /about/index.html`。
// 守卫按 `/about` 的条目判定受限，而静态面对显式文件名走「产物内普通文件」分支
// （`/about` 是符号链接，`/about/index.html` 跟随它拿到产物入口）—— 归一不一致时
// 这条请求就是一行就写完的绕过。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
)

// publishGuardTestEntry 按生产布局发布一个条目：
//
//	{root}/public/active/{条目} -> ../../artifacts/{hash}
//	{root}/artifacts/{hash}/{index.html,guard.html,guard.json}
func publishGuardTestEntry(t *testing.T, entry string, html string, access *builder.AccessGuardSettings) {
	t.Helper()
	root := pipeline.DefaultArtifactRoot()
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	extra, _ := pipeline.BuildGuardEntries(access, "zh-CN")
	hash := pipeline.SHA256([]byte(entry + html))
	dir := filepath.Join(root, "artifacts", hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建产物目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	for name, data := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("写守卫产物 %s 失败: %v", name, err)
		}
	}
	link := filepath.Join(active, filepath.FromSlash(entry))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("建条目父目录失败: %v", err)
	}
	if err := os.Symlink("../../artifacts/"+hash, link); err != nil {
		t.Fatalf("建激活链接失败: %v", err)
	}
}

// newAccessGuardRouter 装配生产形状的访问面（含根挂载点的 NoRoute 链 + 解锁端点）。
//
// 刻意**不**像 site_404_test.go 那样覆盖 NoRoute：那条路会把访问面的静态文件
// 处理整段换掉，测不到「守卫之后静态面会不会直出」这件事 —— 而绕过恰好在那里。
func newAccessGuardRouter() *gin.Engine {
	router := gin.New()
	setupStaticFace(router)
	return router
}

// TestAccessGuardStaticFaceBlocksExplicitIndexPath 端到端：显式文件名写法不得绕过守卫。
func TestAccessGuardStaticFaceBlocksExplicitIndexPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "<html><body>SECRET-ABOUT</body></html>"
	publishGuardTestEntry(t, "about", secret,
		&builder.AccessGuardSettings{Type: builder.AccessPassword, PasswordHash: string(hash)})
	publishGuardTestEntry(t, "blog", "<html><body>PUBLIC-BLOG</body></html>", nil)

	router := newAccessGuardRouter()

	// 条目本身：两个挂载点都必须被挡，且响应必须是守卫页。
	for _, path := range []string{"/about", "/about/", "/site/about", "/site/about/"} {
		rec := doGet(router, path)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s 期望 403，实际 %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "SECRET-ABOUT") {
			t.Fatalf("%s 泄漏了受限内容（守卫被绕过）", path)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("%s 期望 private, no-store，实际 %q", path, cc)
		}
		// 守卫页必须 noindex：否则搜索引擎会收录这一页。
		if !strings.Contains(rec.Body.String(), "noindex") {
			t.Errorf("%s 未返回守卫页（缺 noindex）", path)
		}
	}

	// 显式文件名写法（**本任务要钉死的绕过用例**）：真实链上它先被
	// SiteRedirectMiddleware 的 index 别名规则 301 到条目本身（I18N-022），
	// 而条目本身被守卫挡住 —— 所以安全。判据写「要么 403、要么 301 到受限条目」：
	// 两条路都不放行内容，且 301 的目标必须**仍然是受限的那一个**
	// （301 到别处就是被这条规则链悄悄绕过）。
	for _, path := range []string{"/about/index.html", "/site/about/index.html"} {
		rec := doGet(router, path)
		switch rec.Code {
		case http.StatusForbidden:
			if !strings.Contains(rec.Body.String(), "noindex") {
				t.Errorf("%s 返回 403 但不是守卫页", path)
			}
		case http.StatusMovedPermanently:
			if loc := rec.Header().Get("Location"); loc != "/about" {
				t.Fatalf("%s 301 到 %q，绕过了受限条目", path, loc)
			}
		default:
			t.Errorf("%s 期望 403 或 301 到受限条目，实际 %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "SECRET-ABOUT") {
			t.Fatalf("%s 泄漏了受限内容（守卫被绕过）", path)
		}
	}

	// 公开页面照常直出。两种挂载点的目录形态差异是既有行为：
	// 根挂载点的 siteFileServe 直接直出入口；/site 走 StaticFS，无斜杠的目录
	// 会被它 301 到带斜杠形式。显式文件名写法由 index 别名规则 301 到条目本身。
	for _, path := range []string{"/blog", "/blog/", "/site/blog/"} {
		rec := doGet(router, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "PUBLIC-BLOG") {
			t.Errorf("%s 期望放行，实际 %d", path, rec.Code)
		}
	}
	if rec := doGet(router, "/blog/index.html"); rec.Code != http.StatusMovedPermanently ||
		rec.Header().Get("Location") != "/blog" {
		t.Errorf("/blog/index.html 期望 301 到 /blog，实际 %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// 产物内的守卫元数据不得被直出（含 bcrypt 哈希）。
	rec := doGet(router, "/about/guard.json")
	if rec.Code != http.StatusForbidden {
		t.Errorf("/about/guard.json 期望 403，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "$2") {
		t.Error("/about/guard.json 泄漏了 bcrypt 哈希")
	}
}

// TestAccessGuardStaticFaceUnlockThenServe 解锁后两个挂载点、两种写法都能拿到内容。
func TestAccessGuardStaticFaceUnlockThenServe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "<html><body>SECRET-ABOUT</body></html>"
	publishGuardTestEntry(t, "about", secret,
		&builder.AccessGuardSettings{Type: builder.AccessPassword, PasswordHash: string(hash)})
	router := newAccessGuardRouter()

	form := url.Values{"path": {"/about"}, "password": {"s3cret"}}
	req := httptest.NewRequest(http.MethodPost, builtinUnlockPath(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("解锁期望 302，实际 %d，体=%q", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "gw_access" {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("解锁未下发 cookie")
	}

	// 解锁后：条目本身必须真的直出受限内容（两种挂载点都验），
	// 显式文件名写法由 index 别名 301 到条目本身（目标是受限条目，安全）。
	for _, path := range []string{"/about", "/about/", "/site/about/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != http.StatusOK {
			t.Errorf("%s 解锁后期望 200，实际 %d", path, out.Code)
		}
		if !strings.Contains(out.Body.String(), "SECRET-ABOUT") {
			t.Errorf("%s 解锁后未拿到受限内容", path)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/about/index.html", nil)
	req.AddCookie(cookie)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code == http.StatusMovedPermanently && out.Header().Get("Location") != "/about" {
		t.Errorf("/about/index.html 301 到 %q（应为受限条目 /about）", out.Header().Get("Location"))
	}
}

// builtinUnlockPath 解锁端点路径（与 builtin 常量同源的镜像，避免测试里写死字面量）。
func builtinUnlockPath() string { return accessUnlockPath }
