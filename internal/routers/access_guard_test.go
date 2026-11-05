package routers

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

	"go_wp/pkg/builder"
	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
)

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

func newAccessGuardRouter() *gin.Engine {
	router := gin.New()
	setupStaticFace(router)
	return router
}

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

		if !strings.Contains(rec.Body.String(), "noindex") {
			t.Errorf("%s 未返回守卫页（缺 noindex）", path)
		}
	}

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

	rec := doGet(router, "/about/guard.json")
	if rec.Code != http.StatusForbidden {
		t.Errorf("/about/guard.json 期望 403，实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "$2") {
		t.Error("/about/guard.json 泄漏了 bcrypt 哈希")
	}
}

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

func builtinUnlockPath() string { return accessUnlockPath }
