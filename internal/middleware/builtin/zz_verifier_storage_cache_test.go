package builtin

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

func TestVerifierStorageCacheBranches(t *testing.T) {
	root := t.TempDir()

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

		{"新格式变体 thumb", "/storage/123_thumb-1-ab12cd34.jpg", wantImmutable, http.StatusOK},
		{"新格式变体 full", "/storage/123_full-1-ab12cd34.jpg", wantImmutable, http.StatusOK},
		{"语义化 stem 变体", "/storage/photo_thumb-1-92ca61da.jpg", wantImmutable, http.StatusOK},
		{"存量随机名 stem 变体", "/storage/1700000000000000000_ab12cd_thumb-1-deadbeef.jpg", wantImmutable, http.StatusOK},
		{"带查询串", "/storage/123_thumb-1-ab12cd34.jpg?v=2", wantImmutable, http.StatusOK},

		{"原图", "/storage/123.png", wantRevalidate, http.StatusOK},

		{"旧格式变体", "/storage/123_thumb.jpg", wantRevalidate, http.StatusOK},

		{"大写 hash", "/storage/123_thumb-1-AB12CD34.jpg", wantRevalidate, http.StatusNotFound},
		{"7 位 hash", "/storage/123_thumb-1-ab12cd3.jpg", wantRevalidate, http.StatusNotFound},
		{"无 generation", "/storage/123_thumb-ab12cd34.jpg", wantRevalidate, http.StatusNotFound},

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

func TestVerifierStorageCacheTraversal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "123_thumb-1-ab12cd34.jpg"), []byte("IN-ROOT"), 0o644); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}

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
