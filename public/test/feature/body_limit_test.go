package feature

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/response"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitRejectsLargeJSONBody(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.POST("/limited", builtin.RequestBodyLimitMiddleware(10, 100), func(c *gin.Context) {
				response.Success(c, gin.H{"reached": true})
			})
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/limited",
		RawBody: []byte(strings.Repeat("a", 11)),
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码不正确: got=%d want=%d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRequestBodyLimitUsesUploadLimitForUploadRoute(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.POST("/upload/file", builtin.RequestBodyLimitMiddleware(10, 100), func(c *gin.Context) {
				response.Success(c, gin.H{"reached": true})
			})
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/upload/file",
		RawBody: []byte(strings.Repeat("a", 20)),
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码不正确: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}

func TestRequestRateLimitBlocksRepeatedRequests(t *testing.T) {
	engine := gin.New()
	engine.GET("/limited-rate", builtin.RequestRateLimitMiddleware(2, time.Minute), func(c *gin.Context) {
		response.Success(c, gin.H{"reached": true})
	})

	for i := 0; i < 2; i++ {
		recorder, err := support.SendRequest(engine, support.RequestOptions{
			Method: http.MethodGet,
			Path:   "/limited-rate",
		})
		if err != nil {
			t.Fatalf("发送请求失败: %v", err)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("前两次请求应放行: got=%d want=%d", recorder.Code, http.StatusOK)
		}
	}

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/limited-rate",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("第三次请求应被限流: got=%d want=%d", recorder.Code, http.StatusTooManyRequests)
	}
	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusTooManyRequests)
	}
}

// chunked 传输（Content-Length = -1）超限：不得依赖不可信的 Content-Length 头，
// MaxBytesReader 读取时强制截断，读取方收到 *http.MaxBytesError 应判定为 413。
func TestRequestBodyLimitRejectsChunkedBodyOverLimit(t *testing.T) {
	engine := gin.New()
	engine.POST("/limited", builtin.RequestBodyLimitMiddleware(10, 100), func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		response.Success(c, gin.H{"reached": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader(strings.Repeat("a", 11)))
	req.ContentLength = -1 // 模拟 chunked / 未知长度

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked 超限应 413: got=%d want=%d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

// chunked 传输未超限：应正常放行，读取内容完整。
func TestRequestBodyLimitAcceptsChunkedBodyWithinLimit(t *testing.T) {
	engine := gin.New()
	engine.POST("/limited", builtin.RequestBodyLimitMiddleware(10, 100), func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		response.Success(c, gin.H{"len": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/limited", strings.NewReader(strings.Repeat("a", 5)))
	req.ContentLength = -1 // 模拟 chunked / 未知长度

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("chunked 未超限应放行: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}
