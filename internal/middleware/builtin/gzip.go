package builtin

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
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

func (w *gzipResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.start()
	if w.compressed {
		return io.Copy(sfWriterOnly{w}, r)
	}
	sfCommitHeader(w.ResponseWriter)
	return sfForward(w.ResponseWriter, r)
}

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

func PrecompressedAssetMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return
		}

		if servePrecompressed(c, prefix) {
			return
		}
	}
}

func servePrecompressed(c *gin.Context, prefix string) bool {

	if !acceptsGzip(c.Request.Header.Get("Accept-Encoding")) {
		return false
	}

	if c.Request.Header.Get("Range") != "" {
		return false
	}
	file, ok := siteAssetFile(prefix, c.Request.URL.Path)
	if !ok {
		return false
	}
	f, err := os.Open(file + pipeline.PrecompressedSuffix)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return false
	}

	h := c.Writer.Header()
	h.Set("Content-Encoding", "gzip")

	h.Set("Content-Type", pipeline.AssetContentType(file))

	addVaryAcceptEncoding(h)

	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))

	http.ServeContent(newSendfileWriter(c.Writer), c.Request, filepath.Base(file), st.ModTime(), f)
	c.Abort()
	return true
}

func siteAssetFile(prefix, urlPath string) (string, bool) {
	rel, inFace := SiteFaceRel(prefix, urlPath)
	if !inFace {
		return "", false
	}
	clean, ok := pipeline.CleanSiteRel(rel)
	if !ok {
		return "", false
	}
	root := pipeline.ActiveRoot()

	if entry, hit := pipeline.ResolveActiveEntry(root, clean); hit {
		return filepath.Join(root, filepath.FromSlash(entry), "index.html"), true
	}

	target := filepath.Join(root, filepath.FromSlash(clean))
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		return target, true
	}
	return "", false
}
