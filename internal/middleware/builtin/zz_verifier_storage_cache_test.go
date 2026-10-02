package builtin

// zz_verifier_storage_cache_test.go — 独立验证者（verifier）对 /storage 缓存头
// 三分支的构造真实请求验证。不入实现者改动；用真实 gin 引擎 + 真实静态文件服务。
//
// 复现装配形态（internal/routers/assembly.go）：
//   router.Group("/storage", nosniff中间件, StorageCacheMiddleware()).StaticFS("/", gin.Dir(root, false))

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

const (
	wantImmutable  = "public, max-age=31536000, immutable"
	wantRevalidate = "public, max-age=0, must-revalidate"
)

func newStorageRouter(t *testing.T, root string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	g := router.Group("/storage",
		func(c *gin.Context) {
			c.Header("X-Content-Type-Options", "nosniff")
			c.Next()
		},
		StorageCacheMiddleware(),
	)
	g.StaticFS("/", gin.Dir(root, false))
	return router
}

func doGet(t *testing.T, router *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestVerifierStorageCacheBranches 三分支 + 边界：缓存语义必须与
// 「URL 是否与内容绑定」一致。真实文件走真实静态服务（状态码也一并断言）。
func TestVerifierStorageCacheBranches(t *testing.T) {
	root := t.TempDir()

	// 真实落盘的文件：新格式变体、旧格式变体、原图、语义化 stem 变体、存量随机名 stem 变体。
	files := []string{
		"123_thumb-1-ab12cd34.jpg",
		"123_thumb.jpg",
		"123.png",
		"photo_thumb-1-92ca61da.jpg",
		"1700000000000000000_ab12cd_thumb-1-deadbeef.jpg",
		"x_thumb-1-abcd1234.jpg",
		"123_full-1-ab12cd34.jpg",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("落盘测试文件失败: %v", err)
		}
	}

	cases := []struct {
		name     string
		target   string
		want     string
		wantCode int
	}{
		// —— 分支 1：新格式变体（带完整指纹形状）→ immutable ——
		{"新格式变体 thumb", "/storage/123_thumb-1-ab12cd34.jpg", wantImmutable, http.StatusOK},
		{"新格式变体 full", "/storage/123_full-1-ab12cd34.jpg", wantImmutable, http.StatusOK},
		{"语义化 stem 变体", "/storage/photo_thumb-1-92ca61da.jpg", wantImmutable, http.StatusOK},
		{"存量随机名 stem 变体", "/storage/1700000000000000000_ab12cd_thumb-1-deadbeef.jpg", wantImmutable, http.StatusOK},
		{"带查询串", "/storage/123_thumb-1-ab12cd34.jpg?v=2", wantImmutable, http.StatusOK},

		// —— 分支 2：原图 → must-revalidate（换图原地覆盖，URL 与内容解耦）——
		{"原图", "/storage/123.png", wantRevalidate, http.StatusOK},

		// —— 分支 3：旧格式变体（无指纹）→ must-revalidate ——
		{"旧格式变体", "/storage/123_thumb.jpg", wantRevalidate, http.StatusOK},

		// —— 边界：指纹形状不完整一律 revalidate ——
		{"大写 hash", "/storage/123_thumb-1-AB12CD34.jpg", wantRevalidate, http.StatusNotFound},
		{"7 位 hash", "/storage/123_thumb-1-ab12cd3.jpg", wantRevalidate, http.StatusNotFound},
		{"无 generation", "/storage/123_thumb-ab12cd34.jpg", wantRevalidate, http.StatusNotFound},

		// —— 404 与目录请求也落 revalidate（不为未知路径预先放宽）——
		{"不存在的路径", "/storage/nope.png", wantRevalidate, http.StatusNotFound},
		{"目录请求（gin.Dir 禁列表）", "/storage/", wantRevalidate, http.StatusNotFound},
	}

	router := newStorageRouter(t, root)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doGet(t, router, tc.target)
			if got := w.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("GET %s → Cache-Control = %q，期望 %q", tc.target, got, tc.want)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("GET %s → X-Content-Type-Options = %q，期望 nosniff", tc.target, got)
			}
			if w.Code != tc.wantCode {
				t.Errorf("GET %s → 状态码 = %d，期望 %d", tc.target, w.Code, tc.wantCode)
			}
		})
	}
}

// TestVerifierStorageCacheCounterexample 把 Lead 指定的反例显式钉住：
// 「用户原名恰好命中指纹形状的原图」在当前正则下会被判 immutable。
// 这个测试**记录事实**而不是断言对错 —— 可达性由上传路径决定（见验证报告）。
func TestVerifierStorageCacheCounterexample(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x_thumb-1-abcd1234.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	router := newStorageRouter(t, root)
	got := doGet(t, router, "/storage/x_thumb-1-abcd1234.jpg").Header().Get("Cache-Control")
	t.Logf("反例 x_thumb-1-abcd1234.jpg → Cache-Control = %q（immutable 即命中放宽判据）", got)
	if got != wantImmutable {
		t.Logf("注意：该反例当前**不会**被判 immutable（判据比预期更紧）")
	}
}

// TestVerifierStorageCacheTraversal 畸形路径的安全边界：
// 判据取 URL basename，含 "../" 的畸形路径若 basename 命中指纹形状也会拿到 immutable 头；
// 但 Go 标准库 http.FileServer 会先 path.Clean 再交给 http.Dir.Open，
// 所以 Clean 后仍在 root 内解析 —— 真正的判据是「能否读到 root 之外的文件」，
// 而不是「畸形路径是否返回 200」。
func TestVerifierStorageCacheTraversal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "123_thumb-1-ab12cd34.jpg"), []byte("IN-ROOT"), 0o644); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	// root 之外的文件：穿越若得逞会读到它
	if err := os.WriteFile(filepath.Join(base, "secret.jpg"), []byte("OUTSIDE-SECRET"), 0o644); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}

	router := newStorageRouter(t, root)
	for _, target := range []string{
		"/storage/../secret.jpg",
		"/storage/../../secret.jpg",
		"/storage/%2e%2e/secret.jpg",
		"/storage/sub/../../secret.jpg",
		"/storage/../123_thumb-1-ab12cd34.jpg",
		"/storage/sub/../../123_thumb-1-ab12cd34.jpg",
	} {
		w := doGet(t, router, target)
		t.Logf("GET %s → 状态码=%d Cache-Control=%q body=%q",
			target, w.Code, w.Header().Get("Cache-Control"), w.Body.String())
		if w.Body.String() == "OUTSIDE-SECRET" {
			t.Errorf("GET %s 读到了 root 之外的文件内容（穿越成功）", target)
		}
	}
}
