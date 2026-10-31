package builtin

import (
	"io"
	"net/http"
	"path"
	"regexp"

	"github.com/gin-gonic/gin"
)

const (
	storageCacheImmutable  = "public, max-age=31536000, immutable"
	storageCacheRevalidate = "public, max-age=0, must-revalidate"
)

var storageImmutableVariant = regexp.MustCompile(`_(?:thumb|small|medium|full)-\d+-[0-9a-f]{8}\.jpg$`)

func StorageCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		if storageImmutableVariant.MatchString(path.Base(c.Request.URL.Path)) {

			c.Header("Cache-Control", storageCacheRevalidate)
			c.Writer = &storageCacheWriter{ResponseWriter: c.Writer}
		} else {

			c.Header("Cache-Control", storageCacheRevalidate)
		}
		c.Next()
	}
}

type storageCacheWriter struct {
	gin.ResponseWriter
}

func (w *storageCacheWriter) WriteHeader(code int) {
	if isCacheableOK(code) {
		w.ResponseWriter.Header().Set("Cache-Control", storageCacheImmutable)
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *storageCacheWriter) Write(b []byte) (int, error) {
	if isCacheableOK(w.ResponseWriter.Status()) {
		w.ResponseWriter.Header().Set("Cache-Control", storageCacheImmutable)
	}
	return w.ResponseWriter.Write(b)
}

func isCacheableOK(code int) bool {
	return code >= http.StatusOK && code < http.StatusMultipleChoices
}

func (w *storageCacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *storageCacheWriter) ReadFrom(r io.Reader) (int64, error) {
	if isCacheableOK(w.ResponseWriter.Status()) {
		w.ResponseWriter.Header().Set("Cache-Control", storageCacheImmutable)
	}
	sfCommitHeader(w.ResponseWriter)
	return sfForward(w.ResponseWriter, r)
}
