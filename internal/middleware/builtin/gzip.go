package builtin

import (
	publish "go_wp/pkg/publish"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"
)

var acceptsGzip = mw.AcceptsGzip

var addVaryAcceptEncoding = mw.AddVaryAcceptEncoding

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
	f, err := os.Open(file + publish.PrecompressedSuffix)
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

	h.Set("Content-Type", publish.AssetContentType(file))

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
	clean, ok := publish.CleanSiteRel(rel)
	if !ok {
		return "", false
	}
	root := publish.ActiveRoot()

	if entry, hit := publish.ResolveActiveEntry(root, clean); hit {
		return filepath.Join(root, filepath.FromSlash(entry), "index.html"), true
	}

	target := filepath.Join(root, filepath.FromSlash(clean))
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		return target, true
	}
	return "", false
}
