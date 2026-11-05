package middleware

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

const gzipMinSize = 1024

var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

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

type gzipResponseWriter struct {
	gin.ResponseWriter
	zw         *gzip.Writer
	status     int
	started    bool
	compressed bool
	accepts    bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.started {
		w.status = code
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) start() {
	if w.started {
		return
	}
	w.started = true
	if w.status == 0 {
		w.status = http.StatusOK
	}

	addVaryAcceptEncoding(w.Header())
	if w.accepts && w.status == http.StatusOK && shouldCompressGzip(w.Header()) {
		w.compressed = true
		zw := gzipPool.Get().(*gzip.Writer)
		zw.Reset(w.ResponseWriter)
		w.zw = zw
		h := w.Header()
		h.Set("Content-Encoding", "gzip")

		h.Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(w.status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	w.start()
	if w.compressed {
		return w.zw.Write([]byte(s))
	}
	return w.ResponseWriter.WriteString(s)
}

func (w *gzipResponseWriter) WriteHeaderNow() {
	w.start()
}

func (w *gzipResponseWriter) Flush() {
	w.start()
	if w.compressed {
		_ = w.zw.Flush()
	}
	w.ResponseWriter.Flush()
}

func (w *gzipResponseWriter) close() {
	w.start()
	if w.compressed && w.zw != nil {
		_ = w.zw.Close()
		gzipPool.Put(w.zw)
		w.zw = nil
	}
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ReadFrom 保留零拷贝：未压缩时把 ReaderFrom 交给内层 writer。
func (w *gzipResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.start()
	if w.compressed {
		return io.Copy(writerOnly{w}, r)
	}
	// 与 gin 的 sendfile 判据一致：先 Flush 提交头，再让内层处理 ReaderFrom；
	// 内层没有 ReaderFrom 时用隐藏式 writer 包装，走普通 Copy。
	w.ResponseWriter.Flush()
	if rf := zeroCopyTarget(w.ResponseWriter); rf != nil {
		return rf.ReadFrom(r)
	}
	return io.Copy(writerOnly{w.ResponseWriter}, r)
}

// writerOnly 隐藏底层可能实现的 ReaderFrom，避免 io.Copy 绕过 gzip。
type writerOnly struct{ io.Writer }

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
			return accepted
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

// AcceptsGzip 判定请求是否接受 gzip 编码。
func AcceptsGzip(header string) bool { return acceptsGzip(header) }

// AddVaryAcceptEncoding 给响应补 Vary: Accept-Encoding。
func AddVaryAcceptEncoding(h http.Header) { addVaryAcceptEncoding(h) }

// GzipMinSize 是实时压缩的最小响应体（字节）。
const GzipMinSize = gzipMinSize

// IsGzipType 判定 Content-Type 是否适合 gzip。
func IsGzipType(contentType string) bool { return isGzipType(contentType) }

// zeroCopyTarget 剥开包装后找到实现了 ReaderFrom 的 writer。
func zeroCopyTarget(w http.ResponseWriter) io.ReaderFrom {
	if rf, ok := w.(io.ReaderFrom); ok {
		return rf
	}
	if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		if inner := u.Unwrap(); inner != nil && inner != w {
			return zeroCopyTarget(inner)
		}
	}
	return nil
}
