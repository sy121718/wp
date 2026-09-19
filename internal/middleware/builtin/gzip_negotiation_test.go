package builtin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGzipNegotiationQuality(t *testing.T) {
	for _, tt := range []struct {
		header string
		want   bool
	}{
		{"gzip;q=0", false}, {"gzip;q=0.000", false}, {"gzip;q=0.5", true},
		{"br, *;q=1", true}, {"gzip;q=0, *;q=1", false}, {"gzip;q=invalid", false},
		{"gzip;q=2", false}, {"gzip;q=-1", false}, {"gzip;q=NaN", false},
	} {
		t.Run(tt.header, func(t *testing.T) {
			if got := acceptsGzip(tt.header); got != tt.want {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestGzipVaryIncludesIdentityAndConditionalResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(StaticGzipMiddleware())
	r.GET("/asset", func(c *gin.Context) {
		c.Header("Vary", "Origin")
		if c.GetHeader("If-None-Match") != "" {
			c.Status(http.StatusNotModified)
			return
		}
		c.Data(http.StatusOK, "text/javascript", []byte(strings.Repeat("x", 2048)))
	})
	r.HEAD("/asset", func(c *gin.Context) { c.Header("Vary", "Origin"); c.Status(http.StatusOK) })
	for _, conditional := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/asset", nil)
		if conditional {
			req.Header.Set("If-None-Match", "test")
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		vary := strings.Join(res.Header().Values("Vary"), ",")
		if !strings.Contains(vary, "Accept-Encoding") || !strings.Contains(vary, "Origin") {
			t.Fatalf("缓存变体缺失: %q", vary)
		}
	}
	req := httptest.NewRequest(http.MethodHead, "/asset", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Body.Len() != 0 || !strings.Contains(strings.Join(res.Header().Values("Vary"), ","), "Accept-Encoding") {
		t.Fatalf("HEAD 必须保持空体并声明编码变体：%v %q", res.Header(), res.Body.String())
	}

}
