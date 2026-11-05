package builtin

import (
	publish "go_wp/pkg/publish"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
	"go_wp/pkg/builder"
)

type guardTestEntry struct {
	HTML string

	Access *builder.AccessGuardSettings

	Lang string
}

func addGuardTestEntry(t *testing.T, entry string, e guardTestEntry) {
	t.Helper()
	root := publish.DefaultArtifactRoot()
	active := publish.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	html := e.HTML
	if html == "" {
		html = "<html><body>" + entry + "</body></html>"
	}
	extra, _ := pipeline.BuildGuardEntries(e.Access, e.Lang)
	hash := publish.SHA256([]byte(entry + html))
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

func newGuardTestSite(t *testing.T, entries map[string]guardTestEntry) {
	t.Helper()
	for entry, e := range entries {
		addGuardTestEntry(t, entry, e)
	}
}

func passwordGuard(t *testing.T, password string) *builder.AccessGuardSettings {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成哈希失败: %v", err)
	}
	return &builder.AccessGuardSettings{Type: builder.AccessPassword, PasswordHash: string(hash)}
}

func newGuardTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AccessGuardMiddleware(""))
	r.POST(AccessUnlockPath, AccessUnlockHandler())
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "CONTENT") })
	return r
}

func guardGet(r *gin.Engine, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func guardUnlock(r *gin.Engine, path, password string) (*httptest.ResponseRecorder, []*http.Cookie) {
	form := url.Values{"path": {path}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, AccessUnlockPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec, rec.Result().Cookies()
}

func TestAccessGuardBlocksEntryAndExplicitIndexPath(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
		"inner": {HTML: "<html><body>PUBLIC-INNER</body></html>"},
		"blog":  {HTML: "<html><body>PUBLIC-BLOG</body></html>"},
	})
	r := newGuardTestRouter()

	for _, path := range []string{"/about", "/about/", "/about/index.html", "//about/index.html"} {
		rec := guardGet(r, path)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s 期望 403（受限），实际 %d，体=%q", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "SECRET-ABOUT") {
			t.Fatalf("%s 泄漏了受限内容", path)
		}

		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("%s 期望 Cache-Control=private, no-store，实际 %q", path, cc)
		}
	}

	for _, path := range []string{"/blog", "/blog/index.html", "/inner"} {
		rec := guardGet(r, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CONTENT") {
			t.Errorf("%s 期望放行（200 CONTENT），实际 %d", path, rec.Code)
		}
	}
}

func TestAccessGuardHidesGuardMeta(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()
	for _, path := range []string{"/about/guard.json", "/about/guard.html"} {
		rec := guardGet(r, path)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s 期望 403，实际 %d，体=%q", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "$2a$") || strings.Contains(rec.Body.String(), "$2b$") {
			t.Fatalf("%s 泄漏了 bcrypt 哈希", path)
		}
	}
}

func TestAccessGuardUnlockFlow(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()

	badRec, badCookies := guardUnlock(r, "/about", "wrong-password")
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("错误密码期望 403，实际 %d", badRec.Code)
	}
	for _, ck := range badCookies {
		if ck.Name == accessCookieName && ck.Value != "" {
			t.Fatalf("密码错误却下发了解锁 cookie")
		}
	}

	okRec, cookies := guardUnlock(r, "/about", "s3cret")
	if okRec.Code != http.StatusFound {
		t.Fatalf("正确密码期望 302，实际 %d，体=%q", okRec.Code, okRec.Body.String())
	}
	if loc := okRec.Header().Get("Location"); loc != "/about" {
		t.Fatalf("回跳路径期望 /about，实际 %q", loc)
	}
	var cookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == accessCookieName {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("未下发解锁 cookie")
	}

	if cookie.SameSite != http.SameSiteLaxMode || !cookie.HttpOnly {
		t.Errorf("解锁 cookie 属性不符：SameSite=%v HttpOnly=%v", cookie.SameSite, cookie.HttpOnly)
	}

	for _, path := range []string{"/about", "/about/index.html"} {
		rec := guardGet(r, path, cookie)
		if rec.Code != http.StatusOK {
			t.Errorf("%s 解锁后期望 200，实际 %d", path, rec.Code)
		}
	}

	addGuardTestEntry(t, "shop", guardTestEntry{HTML: "<html><body>SECRET-SHOP</body></html>", Access: passwordGuard(t, "shop-pass")})
	rec := guardGet(r, "/shop", cookie)
	if rec.Code != http.StatusForbidden {
		t.Errorf("/shop 不应被 about 的解锁放行，实际 %d", rec.Code)
	}
}

func TestAccessGuardFingerprintChangeInvalidatesCookie(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", root)
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "old-pass")},
	})
	r := newGuardTestRouter()
	_, cookies := guardUnlock(r, "/about", "old-pass")
	var cookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == accessCookieName {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("未下发解锁 cookie")
	}
	if rec := guardGet(r, "/about", cookie); rec.Code != http.StatusOK {
		t.Fatalf("解锁后期望 200，实际 %d", rec.Code)
	}

	if err := os.Remove(filepath.Join(publish.ActiveRoot(), "about")); err != nil {
		t.Fatalf("移除旧激活链接失败: %v", err)
	}
	addGuardTestEntry(t, "about", guardTestEntry{HTML: "<html><body>SECRET-ABOUT-V2</body></html>", Access: passwordGuard(t, "new-pass")})
	if rec := guardGet(r, "/about", cookie); rec.Code != http.StatusForbidden {
		t.Fatalf("改密码后旧 cookie 应失效（403），实际 %d", rec.Code)
	}

	if rec, _ := guardUnlock(r, "/about", "new-pass"); rec.Code != http.StatusFound {
		t.Fatalf("新密码应解锁成功，实际 %d", rec.Code)
	}
}

func TestAccessGuardMembers(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"member": {HTML: "<html><body>MEMBERS-ONLY</body></html>", Access: &builder.AccessGuardSettings{Type: builder.AccessMembers}},
	})
	r := newGuardTestRouter()

	SetAccessGuardLoginProbe(nil)
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("探针未注入时期望 403，实际 %d", rec.Code)
	}

	SetAccessGuardLoginProbe(func(*gin.Context) bool { return false })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("未登录期望 403，实际 %d", rec.Code)
	}

	SetAccessGuardLoginProbe(func(*gin.Context) bool { return true })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusOK {
		t.Fatalf("已登录期望 200，实际 %d", rec.Code)
	}

	SetAccessGuardLoginProbe(func(*gin.Context) bool { panic("probe boom") })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("探针 panic 时期望 403，实际 %d", rec.Code)
	}
	SetAccessGuardLoginProbe(nil)
}

func TestAccessGuardMetaCorruptedFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", root)
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})

	target, err := os.Readlink(filepath.Join(publish.ActiveRoot(), "about"))
	if err != nil {
		t.Fatalf("读链接失败: %v", err)
	}
	dir := filepath.Clean(filepath.Join(publish.ActiveRoot(), target))
	if err := os.WriteFile(filepath.Join(dir, pipeline.GuardMetaFileName), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("写坏元数据失败: %v", err)
	}
	r := newGuardTestRouter()
	rec := guardGet(r, "/about")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("元数据损坏时期望 403（fail closed），实际 %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SECRET-ABOUT") {
		t.Fatal("元数据损坏时泄漏了受限内容")
	}
}

func TestAccessCookieTamperRejected(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()
	_, cookies := guardUnlock(r, "/about", "s3cret")
	var cookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == accessCookieName {
			cookie = ck
		}
	}
	if cookie == nil {
		t.Fatal("未下发解锁 cookie")
	}
	forged := *cookie

	forged.Value = cookie.Value[:len(cookie.Value)-32] + strings.Repeat("0", 32)
	if rec := guardGet(r, "/about", &forged); rec.Code != http.StatusForbidden {
		t.Fatalf("篡改签名期望 403，实际 %d", rec.Code)
	}

	forged2 := *cookie
	if len(forged2.Value) > 40 {
		forged2.Value = "A" + cookie.Value[1:]
	}
	if rec := guardGet(r, "/about", &forged2); rec.Code != http.StatusForbidden {
		t.Fatalf("篡改载荷期望 403，实际 %d", rec.Code)
	}
}

func TestAccessCookieExpiredRejected(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	codec, ok := accessCookieCodec()
	if !ok {
		t.Fatal("会话密钥未就绪")
	}
	value, err := codec.encode(accessPayload{
		V: accessCookieVersion,
		T: time.Now().Add(-13 * time.Hour).Unix(),
		F: pipeline.GuardFingerprint("whatever"),
		P: []string{"about"},
	})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, ok := codec.decode(value); ok {
		t.Fatal("过期载荷不应通过校验")
	}
}

func TestAccessUnlockRejectsNonGuardedPath(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"public": {HTML: "<html><body>PUBLIC</body></html>"},
	})
	r := newGuardTestRouter()
	rec, cookies := guardUnlock(r, "/public", "anything")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("公开页面解锁期望 403，实际 %d", rec.Code)
	}
	for _, ck := range cookies {
		if ck.Name == accessCookieName && ck.Value != "" {
			t.Fatal("公开页面不应下发解锁 cookie")
		}
	}
}

func TestAccessUnlockRejectsOpenRedirect(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()
	for _, evil := range []string{"//evil.example.com", "https://evil.example.com", "/about/../../etc"} {
		rec, _ := guardUnlock(r, evil, "s3cret")
		if loc := rec.Header().Get("Location"); strings.Contains(loc, "evil.example.com") || strings.Contains(loc, "etc") {
			t.Fatalf("path=%q 造成开放重定向：Location=%q", evil, loc)
		}
	}
}

func TestAccessGuardCoversContentVariants(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()

	for _, path := range []string{
		"/about",
		"/about/",
		"/about/index.html",
		"/about/index.html.gz",
		"/about/index.html.br",
		"/About",
		"/ABOUT/index.html",
		"//about//index.html",
		"/about/index.html?x=1",
	} {
		rec := guardGet(r, path)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "SECRET-ABOUT") {
			t.Errorf("%s 绕过了守卫（code=%d）—— 受限产物的任何访问变体都必须 403", path, rec.Code)
		}
	}
}
