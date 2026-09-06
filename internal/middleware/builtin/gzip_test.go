package builtin

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newGzipRequest 构造带/不带 Accept-Encoding 的 GET 请求。
func newGzipRequest(acceptEncoding string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/asset.js", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	return req
}

// gzipEngine 挂 StaticGzipMiddleware，注册一个文本静态资源处理器（模拟静态文件）。
func gzipEngine() *gin.Engine {
	engine := gin.New()
	engine.Use(StaticGzipMiddleware())
	engine.GET("/asset.js", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/javascript; charset=utf-8",
			bytes.Repeat([]byte("var workbench=1;/* padding for size threshold */"), 40))
	})
	engine.GET("/asset.png", func(c *gin.Context) {
		c.Data(http.StatusOK, "image/png", bytes.Repeat([]byte{0x89, 0x50}, 4096))
	})
	engine.GET("/tiny.js", func(c *gin.Context) {
		// 显式 Content-Length（http.ServeContent 对静态文件的行为），模拟小文件阈值。
		body := []byte("var a=1;")
		c.Header("Content-Length", strconv.Itoa(len(body)))
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", body)
	})
	engine.GET("/pre-encoded.js", func(c *gin.Context) {
		c.Header("Content-Encoding", "br")
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", []byte("already-brotli"))
	})
	return engine
}

func serveGzip(path string, acceptEncoding string) *httptest.ResponseRecorder {
	req := newGzipRequest(acceptEncoding)
	req.URL.Path = path
	recorder := httptest.NewRecorder()
	gzipEngine().ServeHTTP(recorder, req)
	return recorder
}

// 客户端接受 gzip：文本文件应压缩，响应可解压还原且头正确。
func TestStaticGzipCompressesTextAsset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/asset.js", "gzip, deflate")

	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("应下发 Content-Encoding: gzip, got=%q", got)
	}
	if got := recorder.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("应包含 Vary: Accept-Encoding, got=%q", got)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("静态资源应 200: got=%d", recorder.Code)
	}
	// 解压还原并校验内容非空。
	zr, err := gzip.NewReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip 解压失败: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip 读取失败: %v", err)
	}
	if len(decoded) == 0 || !bytes.Contains(decoded, []byte("var workbench=1;")) {
		t.Fatalf("解压后内容不正确: len=%d", len(decoded))
	}
	// 压缩产物应明显小于原文。
	if recorder.Body.Len() >= len(decoded) {
		t.Fatalf("压缩应小于原文: gz=%d raw=%d", recorder.Body.Len(), len(decoded))
	}
}

// 无 Accept-Encoding：不压缩、原样输出。
func TestStaticGzipSkipsWithoutAcceptEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/asset.js", "")

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("无 Accept-Encoding 不应压缩: got=%q", got)
	}
	if recorder.Body.Len() == 0 {
		t.Fatalf("响应体不应为空")
	}
}

// 非文本类型（png）：即使接受 gzip 也不压缩。
func TestStaticGzipSkipsBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/asset.png", "gzip")

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("image/png 不应压缩: got=%q", got)
	}
}

// 文本但长度低于阈值：不压缩（负收益）。
func TestStaticGzipSkipsTinyText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/tiny.js", "gzip")

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("小于阈值的小文本不应压缩: got=%q", got)
	}
	if recorder.Body.String() != "var a=1;" {
		t.Fatalf("小文本应原样输出: got=%q", recorder.Body.String())
	}
}

// 已显式设置 Content-Encoding（如 upstream 已压缩）：不重复压缩。
func TestStaticGzipSkipsAlreadyEncoded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/pre-encoded.js", "gzip")

	if got := recorder.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("不应覆盖已有 Content-Encoding: got=%q", got)
	}
}
