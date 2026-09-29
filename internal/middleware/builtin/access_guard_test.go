package builtin

// access_guard_test.go — AccessGuard 访问面守卫的判定测试（PIPE-6）。
//
// 最该被钉死的一条是「条目内文件的显式文件名写法不得绕过守卫」：
// 守卫按条目名判定受限，而访问面既能用 `/about` 取到产物、也能用
// `/about/index.html` 取到同一份产物。两者归一不一致时，第二次请求就是一条
// 一行就写完的绕过（守卫判 `/about` 受限、静态面把 `/about/index.html`
// 当「产物内普通文件」原样直出）。TestAccessGuardBlocksEntryAndExplicitIndexPath
// 就是这条断言。

import (
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

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
)

// guardTestEntry 一个待发布的条目。
type guardTestEntry struct {
	// HTML 产物入口内容。
	HTML string
	// Access 访问设置（nil/公开 = 不带守卫产物）。
	Access *builder.AccessGuardSettings
	// Lang 守卫页语言。
	Lang string
}

// addGuardTestEntry 发布一个条目（建产物目录 + 相对符号链接）。
func addGuardTestEntry(t *testing.T, entry string, e guardTestEntry) {
	t.Helper()
	root := pipeline.DefaultArtifactRoot()
	active := pipeline.ActiveRoot()
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatalf("建激活目录失败: %v", err)
	}
	html := e.HTML
	if html == "" {
		html = "<html><body>" + entry + "</body></html>"
	}
	extra, _ := pipeline.BuildGuardEntries(e.Access, e.Lang)
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

// newGuardTestSite 构造最小激活站点布局（与生产同形）：
//
//	{root}/public/active/{条目} -> ../../artifacts/{hash}   （相对符号链接）
//	{root}/artifacts/{hash}/{index.html,guard.html,guard.json}
//
// 调用前必须已 Setenv GO_WP_ARTIFACT_ROOT。
func newGuardTestSite(t *testing.T, entries map[string]guardTestEntry) {
	t.Helper()
	for entry, e := range entries {
		addGuardTestEntry(t, entry, e)
	}
}

// passwordGuard 生成一个 password 类型的访问设置（哈希来自明文）。
func passwordGuard(t *testing.T, password string) *builder.AccessGuardSettings {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成哈希失败: %v", err)
	}
	return &builder.AccessGuardSettings{Type: builder.AccessPassword, PasswordHash: string(hash)}
}

// newGuardTestRouter 装配一条「守卫 + 终点」的最小访问面：
// 守卫放行时由终点返回 200 CONTENT（模拟静态面直出）。
func newGuardTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AccessGuardMiddleware(""))
	r.POST(AccessUnlockPath, AccessUnlockHandler())
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "CONTENT") })
	return r
}

// guardGet 发起一次 GET。
func guardGet(r *gin.Engine, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// guardUnlock 发起一次解锁请求，返回响应与全部 Set-Cookie。
func guardUnlock(r *gin.Engine, path, password string) (*httptest.ResponseRecorder, []*http.Cookie) {
	form := url.Values{"path": {path}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, AccessUnlockPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec, rec.Result().Cookies()
}

// TestAccessGuardBlocksEntryAndExplicitIndexPath 守卫拦住条目本身**以及**它的
// 显式文件名写法 —— 后者是静态面真正会直出的那条路径。
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
		// 未解锁的响应绝不能是可缓存的公开响应。
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("%s 期望 Cache-Control=private, no-store，实际 %q", path, cc)
		}
	}
	// 公开页面与受限页面下的公开子条目照常放行。
	for _, path := range []string{"/blog", "/blog/index.html", "/inner"} {
		rec := guardGet(r, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CONTENT") {
			t.Errorf("%s 期望放行（200 CONTENT），实际 %d", path, rec.Code)
		}
	}
}

// TestAccessGuardHidesGuardMeta 产物目录里的守卫元数据（含 bcrypt 哈希）不得被直出。
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

// TestAccessGuardUnlockFlow 解锁成功 → 同一 cookie 放行；密码错误 → 403 且不下发 cookie。
func TestAccessGuardUnlockFlow(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	r := newGuardTestRouter()

	// 密码错误：403 + 不下发解锁 cookie。
	badRec, badCookies := guardUnlock(r, "/about", "wrong-password")
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("错误密码期望 403，实际 %d", badRec.Code)
	}
	for _, ck := range badCookies {
		if ck.Name == accessCookieName && ck.Value != "" {
			t.Fatalf("密码错误却下发了解锁 cookie")
		}
	}

	// 正确密码：302 回跳 + 下发解锁 cookie。
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
	// SameSite=Lax 是这条链路 CSRF 论证的前提（跨站 POST 不带 cookie）。
	if cookie.SameSite != http.SameSiteLaxMode || !cookie.HttpOnly {
		t.Errorf("解锁 cookie 属性不符：SameSite=%v HttpOnly=%v", cookie.SameSite, cookie.HttpOnly)
	}

	// 带 cookie：条目本身与显式文件名写法都要放行。
	for _, path := range []string{"/about", "/about/index.html"} {
		rec := guardGet(r, path, cookie)
		if rec.Code != http.StatusOK {
			t.Errorf("%s 解锁后期望 200，实际 %d", path, rec.Code)
		}
	}
	// 未解锁的**另一个**受限条目不受影响。
	addGuardTestEntry(t, "shop", guardTestEntry{HTML: "<html><body>SECRET-SHOP</body></html>", Access: passwordGuard(t, "shop-pass")})
	rec := guardGet(r, "/shop", cookie)
	if rec.Code != http.StatusForbidden {
		t.Errorf("/shop 不应被 about 的解锁放行，实际 %d", rec.Code)
	}
}

// TestAccessGuardFingerprintChangeInvalidatesCookie 改密码 → 旧解锁 cookie 立刻失效。
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

	// 换密码重建产物目录（新哈希 → 新指纹）。
	// 清理旧激活链接，避免与新链接冲突。
	if err := os.Remove(filepath.Join(pipeline.ActiveRoot(), "about")); err != nil {
		t.Fatalf("移除旧激活链接失败: %v", err)
	}
	addGuardTestEntry(t, "about", guardTestEntry{HTML: "<html><body>SECRET-ABOUT-V2</body></html>", Access: passwordGuard(t, "new-pass")})
	if rec := guardGet(r, "/about", cookie); rec.Code != http.StatusForbidden {
		t.Fatalf("改密码后旧 cookie 应失效（403），实际 %d", rec.Code)
	}
	// 新密码可以解锁。
	if rec, _ := guardUnlock(r, "/about", "new-pass"); rec.Code != http.StatusFound {
		t.Fatalf("新密码应解锁成功，实际 %d", rec.Code)
	}
}

// TestAccessGuardMembers 登录可见：探针未注入时 fail closed；注入为已登录时放行。
func TestAccessGuardMembers(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"member": {HTML: "<html><body>MEMBERS-ONLY</body></html>", Access: &builder.AccessGuardSettings{Type: builder.AccessMembers}},
	})
	r := newGuardTestRouter()

	// 探针未注入：fail closed（含未登录与「装配漏了」两种情况）。
	SetAccessGuardLoginProbe(nil)
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("探针未注入时期望 403，实际 %d", rec.Code)
	}
	// 注入「未登录」：
	SetAccessGuardLoginProbe(func(*gin.Context) bool { return false })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("未登录期望 403，实际 %d", rec.Code)
	}
	// 注入「已登录」：
	SetAccessGuardLoginProbe(func(*gin.Context) bool { return true })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusOK {
		t.Fatalf("已登录期望 200，实际 %d", rec.Code)
	}
	// 探针 panic 不能把静态请求打成 500，也不能顺带放行。
	SetAccessGuardLoginProbe(func(*gin.Context) bool { panic("probe boom") })
	if rec := guardGet(r, "/member"); rec.Code != http.StatusForbidden {
		t.Fatalf("探针 panic 时期望 403，实际 %d", rec.Code)
	}
	SetAccessGuardLoginProbe(nil)
}

// TestAccessGuardMetaCorruptedFailsClosed 守卫元数据损坏（存在但解析不了）必须 fail closed。
func TestAccessGuardMetaCorruptedFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GO_WP_ARTIFACT_ROOT", root)
	if err := auth.Init(nil); err != nil {
		t.Fatalf("初始化会话密钥失败: %v", err)
	}
	newGuardTestSite(t, map[string]guardTestEntry{
		"about": {HTML: "<html><body>SECRET-ABOUT</body></html>", Access: passwordGuard(t, "s3cret")},
	})
	// 写坏元数据（合法 JSON 但缺类型）。
	target, err := os.Readlink(filepath.Join(pipeline.ActiveRoot(), "about"))
	if err != nil {
		t.Fatalf("读链接失败: %v", err)
	}
	dir := filepath.Clean(filepath.Join(pipeline.ActiveRoot(), target))
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

// TestAccessCookieTamperRejected 签名被篡改的解锁 cookie 判废（不放行）。
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
	// 换掉签名段（末 32 个字符）。
	forged.Value = cookie.Value[:len(cookie.Value)-32] + strings.Repeat("0", 32)
	if rec := guardGet(r, "/about", &forged); rec.Code != http.StatusForbidden {
		t.Fatalf("篡改签名期望 403，实际 %d", rec.Code)
	}
	// 换掉载荷（后缀不变）：签名同样对不上。
	forged2 := *cookie
	if len(forged2.Value) > 40 {
		forged2.Value = "A" + cookie.Value[1:]
	}
	if rec := guardGet(r, "/about", &forged2); rec.Code != http.StatusForbidden {
		t.Fatalf("篡改载荷期望 403，实际 %d", rec.Code)
	}
}

// TestAccessCookieExpiredRejected 过期载荷判废。
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

// TestAccessUnlockRejectsNonGuardedPath 解锁端点不能给「没有守卫的页面」发通行证。
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

// TestAccessUnlockRejectsOpenRedirect 回跳路径由归一结果重建，不原样回显表单输入。
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
