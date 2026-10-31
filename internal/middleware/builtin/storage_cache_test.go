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
	okImmutable  = "public, max-age=31536000, immutable"
	okRevalidate = "public, max-age=0, must-revalidate"
)

func newCacheProbeRouter(t *testing.T, root string) *gin.Engine {
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

func TestStorageCacheShapeValidButMissing(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "123_thumb-1-ab12cd34.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	router := newCacheProbeRouter(t, root)

	cases := []struct {
		name   string
		target string
	}{
		{"同形但文件不存在（换图后旧变体被清理）", "/storage/123_thumb-9-fedcba98.jpg"},
		{"同形但文件不存在（变体生成失败只留 DB 行）", "/storage/456_full-2-0011aabb.jpg"},
		{"同形但附件不存在", "/storage/999999_medium-1-deadbeef.jpg"},
		{"同形但带查询串", "/storage/123_thumb-9-fedcba98.jpg?v=3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("GET %s → 状态码 = %d，期望 404", tc.target, w.Code)
			}
			if got := w.Header().Get("Cache-Control"); got != okRevalidate {
				t.Errorf("GET %s → Cache-Control = %q，期望 %q（形状合法但不存在的路径不得拿 immutable）",
					tc.target, got, okRevalidate)
			}
		})
	}
}

func TestStorageCacheShapeAndStatusBothMatter(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"123_thumb-1-ab12cd34.jpg", "123_thumb.jpg", "123.png"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("落盘失败: %v", err)
		}
	}
	router := newCacheProbeRouter(t, root)

	cases := []struct {
		name     string
		target   string
		want     string
		wantCode int
	}{
		{"形状命中 + 200", "/storage/123_thumb-1-ab12cd34.jpg", okImmutable, http.StatusOK},
		{"形状命中 + 404", "/storage/123_thumb-1-ffffffff.jpg", okRevalidate, http.StatusNotFound},
		{"形状不命中 + 200", "/storage/123.png", okRevalidate, http.StatusOK},
		{"形状不命中（旧格式变体）+ 200", "/storage/123_thumb.jpg", okRevalidate, http.StatusOK},
		{"形状不命中 + 404", "/storage/nope.png", okRevalidate, http.StatusNotFound},
		{"形状不匹配（大写 hash）", "/storage/123_thumb-1-AB12CD34.jpg", okRevalidate, http.StatusNotFound},
		{"形状不匹配（7 位 hash）", "/storage/123_thumb-1-ab12cd3.jpg", okRevalidate, http.StatusNotFound},
		{"形状不匹配（无 generation）", "/storage/123_thumb-ab12cd34.jpg", okRevalidate, http.StatusNotFound},
		{"目录请求（禁列表）", "/storage/", okRevalidate, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if got := w.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("GET %s → Cache-Control = %q，期望 %q", tc.target, got, tc.want)
			}
			if w.Code != tc.wantCode {
				t.Errorf("GET %s → 状态码 = %d，期望 %d", tc.target, w.Code, tc.wantCode)
			}
			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("GET %s → X-Content-Type-Options = %q，期望 nosniff", tc.target, got)
			}
		})
	}
}
