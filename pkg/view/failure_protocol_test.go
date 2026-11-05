package view

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
)

// TestFailureProtocolAvailableBeforeRouteMiddleware 验证提前拒绝也能识别注册时声明的协议。
func TestFailureProtocolAvailableBeforeRouteMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		marker   gin.HandlerFunc
		fragment bool
	}{
		{"page", HTMLPage, false},
		{"fragment", HTMLFragment, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			calls := 0
			engine.Use(FailureMiddleware(func(c *gin.Context, status int, scene string, err error) {
				calls++
				if status != http.StatusForbidden || scene != "early" || IsFragment(c) != tc.fragment {
					t.Fatalf("失败出口上下文错误：status=%d scene=%q fragment=%v", status, scene, IsFragment(c))
				}
				c.Data(status, "text/html; charset=utf-8", []byte("failure"))
			}))
			engine.Use(func(c *gin.Context) {
				FailStatus(c, http.StatusForbidden, "early", facing.New("sysErrNoPerm"))
				c.Abort()
			})
			engine.POST("/arbitrary/:id", tc.marker, func(c *gin.Context) { t.Fatal("拒绝后不得执行处理器") })
			for _, accept := range []string{"application/json", "text/html"} {
				req := httptest.NewRequest(http.MethodPost, "/arbitrary/123", nil)
				req.Header.Set("Accept", accept)
				req.Header.Set("HX-Request", "true")
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden || rec.Body.String() != "failure" {
					t.Fatalf("提前拒绝未使用声明出口：%d %s", rec.Code, rec.Body.String())
				}
			}
			if calls != 2 {
				t.Fatalf("重复或遗漏失败输出：%d", calls)
			}
		})
	}
}

func TestDataRouteCannotBecomeHTMLByRequestHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(FailureMiddleware(func(c *gin.Context, status int, scene string, err error) {
		t.Fatal("未声明 HTML 的数据端点不能进入 HTML 出口")
	}))
	engine.GET("/data", func(c *gin.Context) { FailScene(c, "data", errors.New("SQLSTATE private detail")) })
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || strings.Contains(rec.Body.String(), "SQLSTATE") {
		t.Fatalf("数据失败协议或保密边界错误：%d %s", rec.Code, rec.Body.String())
	}
}

func TestFailureWriterIsRequestLocalAndWritesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	makeEngine := func(body string) *gin.Engine {
		engine := gin.New()
		engine.Use(FailureMiddleware(func(c *gin.Context, status int, scene string, err error) {
			c.String(status, "%s", body)
		}))
		engine.GET("/", HTMLFragment, func(c *gin.Context) {
			FailScene(c, "test", facing.New("sysErrBadParam"))
			FailScene(c, "test", errors.New("second"))
		})
		return engine
	}
	first, second := makeEngine("first"), makeEngine("second")
	for i := 0; i < 4; i++ {
		for _, tc := range []struct {
			engine *gin.Engine
			body   string
		}{{first, "first"}, {second, "second"}} {
			rec := httptest.NewRecorder()
			tc.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusBadRequest || rec.Body.String() != tc.body {
				t.Fatalf("失败出口跨请求串用或重复写入：%d %q", rec.Code, rec.Body.String())
			}
		}
	}
}
