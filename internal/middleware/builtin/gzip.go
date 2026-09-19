package builtin

// StaticGzipMiddleware 静态资源传输压缩中间件（手写实现，无第三方依赖）。
//
// 挂载对象：/static 后台资源与 /site 静态访问面（文本类产物）。
// 特性：
//   - 仅对 GET 且客户端 Accept-Encoding 含 gzip 的请求生效；
//   - 按 Content-Type 白名单压缩（文本类，跳过已压缩的二进制：png/jpg/woff2 等）；
//   - 已知 Content-Length 且小于阈值（1KB）时不压缩——小文件压缩负收益；
//   - gzip.Writer 走 sync.Pool 复用，避免每请求分配；
//   - 压缩时移除 Content-Length（改 chunked）、写 Content-Encoding: gzip 与 Vary 头。
//
// 实现要点：Gin 的 render（如 Data）在 WriteHeader 之后才设置 Content-Type，
// 因此压缩决策延迟到第一次 Write（此时 Header 尚未真正发送，可安全改写）。
//
// 不变量说明：仅作用于传输层编码，响应语义不变；访问面「只读静态直出」
// 不受影响（文件系统不变，只是字节流经 gzip 传输）。

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipMinSize 小于该字节数不压缩（gzip 头尾 + 小文件无收益）。
const gzipMinSize = 1024

// gzipPool 复用 gzip.Writer（Reset 到每次响应的目标 writer）。
var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// StaticGzipMiddleware 见文件头注释。
func StaticGzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		w := &gzipResponseWriter{
			ResponseWriter: c.Writer,
			accepts:        c.Request.Method == http.MethodGet && acceptsGzip(c.Request.Header.Get("Accept-Encoding")),
		}
		c.Writer = w
		defer w.close()
		c.Next()
	}
}

// gzipResponseWriter 延迟压缩包装：
// WriteHeader 只记录状态码（不转发），第一次 Write/Flush 时依据已就绪的
// Content-Type 决定是否压缩，再转发 WriteHeader + 数据。
type gzipResponseWriter struct {
	gin.ResponseWriter
	zw         *gzip.Writer
	status     int
	started    bool
	compressed bool
	accepts    bool
}

// WriteHeader 记录状态码，延迟转发（Header 在首次 Write 时真正发送）。
func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.started {
		w.status = code
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// start 首次写前决定压缩并转发 WriteHeader（幂等）。
//
// 仅 200 压缩：206 Partial Content 的 body 是字节范围的原始切片（Range 请求，
// 视频拖动/断点下载），压缩会破坏 Content-Range 语义；304/204 无 body。
func (w *gzipResponseWriter) start() {
	if w.started {
		return
	}
	w.started = true
	if w.status == 0 {
		w.status = http.StatusOK
	}
	// identity、HEAD、304 同样参与编码协商；只在 gzip 分支加 Vary 会污染共享缓存。
	addVaryAcceptEncoding(w.Header())
	if w.accepts && w.status == http.StatusOK && shouldCompressGzip(w.Header()) {
		w.compressed = true
		zw := gzipPool.Get().(*gzip.Writer)
		zw.Reset(w.ResponseWriter)
		w.zw = zw
		h := w.Header()
		h.Set("Content-Encoding", "gzip")
		// 压缩后长度未知：移除 Content-Length，走 chunked。
		h.Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(w.status)
}

// Write 压缩路径下写入 gzip 流，否则直写。
func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// WriteString 同 Write（gin 接口成员）。
func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write([]byte(s))
	}
	return w.ResponseWriter.WriteString(s)
}

// WriteHeaderNow 无 body 状态码路径（204/304 等）：补一次启动转发。
func (w *gzipResponseWriter) WriteHeaderNow() {
	w.start()
}

// Flush 转发；静态文件服务（http.ServeContent）不依赖 Flush 语义。
func (w *gzipResponseWriter) Flush() {
	w.start()
	if w.compressed {
		_ = w.zw.Flush()
	}
	w.ResponseWriter.Flush()
}

// close 补转发未启动的响应（纯状态码），结束 gzip 流并归还池。
func (w *gzipResponseWriter) close() {
	w.start()
	if w.compressed && w.zw != nil {
		_ = w.zw.Close()
		gzipPool.Put(w.zw)
		w.zw = nil
	}
}

// acceptsGzip 客户端是否接受 gzip 编码。
func acceptsGzip(header string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		enc := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if !strings.EqualFold(enc, "gzip") && enc != "*" {
			continue
		}
		_, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		quality := 1.0
		if q, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(q, 64)
		}
		accepted := err == nil && quality > 0 && quality <= 1
		if strings.EqualFold(enc, "gzip") {
			return accepted // 显式 gzip（包括 q=0）优先于通配符。
		}
		wildcard = accepted
	}
	return wildcard
}

func addVaryAcceptEncoding(h http.Header) {
	for _, value := range h.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			if field = strings.TrimSpace(field); field == "*" || strings.EqualFold(field, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// shouldCompressGzip 响应头满足压缩条件：未设置过编码、文本类 Content-Type、
// 且（无 Content-Length 或长度达到阈值）。
func shouldCompressGzip(h http.Header) bool {
	if h.Get("Content-Encoding") != "" {
		return false
	}
	ct := h.Get("Content-Type")
	if ct == "" || !isGzipType(ct) {
		return false
	}
	if len := h.Get("Content-Length"); len != "" {
		n, err := strconv.Atoi(len)
		if err != nil || n < gzipMinSize {
			return false
		}
	}
	return true
}

// isGzipType Content-Type 是否属于可压缩文本类。
func isGzipType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mt = strings.ToLower(mt)
	switch {
	case strings.HasPrefix(mt, "text/"):
		return true
	case strings.HasPrefix(mt, "application/javascript"),
		strings.HasPrefix(mt, "application/x-javascript"):
		return true
	case mt == "application/json",
		mt == "application/xml",
		mt == "application/wasm",
		mt == "image/svg+xml",
		mt == "font/ttf",
		mt == "font/otf",
		mt == "application/vnd.ms-fontobject":
		return true
	}
	return false
}
