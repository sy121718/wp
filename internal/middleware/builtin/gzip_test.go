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

func newGzipRequest(acceptEncoding string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/asset.js", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	return req
}

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

		body := []byte("var a=1;")
		c.Header("Content-Length", strconv.Itoa(len(body)))
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", body)
	})
	engine.GET("/pre-encoded.js", func(c *gin.Context) {
		c.Header("Content-Encoding", "br")
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", []byte("already-brotli"))
	})

	engine.GET("/range.js", func(c *gin.Context) {
		c.Header("Content-Range", "bytes 0-1023/5120")
		c.Data(http.StatusPartialContent, "application/javascript; charset=utf-8",
			bytes.Repeat([]byte("x"), 1024))
	})

	engine.GET("/not-modified.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
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

	if recorder.Body.Len() >= len(decoded) {
		t.Fatalf("压缩应小于原文: gz=%d raw=%d", recorder.Body.Len(), len(decoded))
	}
}

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

func TestStaticGzipSkipsBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/asset.png", "gzip")

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("image/png 不应压缩: got=%q", got)
	}
}

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

func TestStaticGzipSkipsAlreadyEncoded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/pre-encoded.js", "gzip")

	if got := recorder.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("不应覆盖已有 Content-Encoding: got=%q", got)
	}
}

func TestStaticGzipSkipsPartialContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/range.js", "gzip")

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("应保持 206: got=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("206 分片响应不应压缩: got=%q", got)
	}
	if recorder.Body.Len() != 1024 {
		t.Fatalf("分片 body 应原样输出 1024 字节: got=%d", recorder.Body.Len())
	}
}

func TestStaticGzipSkipsNotModified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := serveGzip("/not-modified.js", "gzip")

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("应保持 304: got=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("304 不应标记 Content-Encoding: got=%q", got)
	}
}
