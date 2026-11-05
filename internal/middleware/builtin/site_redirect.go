package builtin

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/pipeline"
	"go_wp/pkg/seo"
)

func SiteFaceRel(prefix, fullPath string) (rel string, ok bool) {
	if prefix == "" {
		return strings.TrimPrefix(fullPath, "/"), true
	}
	if fullPath == prefix {
		return "", true
	}
	if !strings.HasPrefix(fullPath, prefix+"/") {
		return "", false
	}
	return strings.TrimPrefix(fullPath, prefix), true
}

func SiteRedirectMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if target, ok := indexAliasRedirectOf(prefix, c.Request.URL.Path); ok {
			c.Redirect(http.StatusMovedPermanently, target)
			c.Abort()
			return
		}
		target, code, ok := redirectTargetOf(prefix, c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}
		c.Redirect(code, target)
		c.Abort()
	}
}

func indexAliasRedirectOf(prefix, fullPath string) (target string, ok bool) {
	rel, ok := SiteFaceRel(prefix, fullPath)
	if !ok {
		return "", false
	}
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return "", false
	}
	sitePath := rel
	if !strings.HasPrefix(sitePath, "/") {
		sitePath = "/" + sitePath
	}
	if !seo.IsPublicIndexAlias(sitePath) {
		return "", false
	}
	return seo.CanonicalPublicPath(sitePath), true
}

func redirectTargetOf(prefix, fullPath string) (target string, code int, ok bool) {
	rel, inFace := SiteFaceRel(prefix, fullPath)
	if !inFace {
		return "", 0, false
	}

	rel = filepath.Clean(strings.TrimPrefix(rel, "/"))
	if rel == "." || rel == "" || filepath.IsAbs(rel) ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", 0, false
	}
	link := filepath.Join(pipeline.ActiveRoot(), rel)

	linkTarget, err := os.Readlink(link)
	if err != nil {
		return "", 0, false
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(link), linkTarget))
	data, err := os.ReadFile(filepath.Join(dir, "redirect.json"))
	if err != nil {
		return "", 0, false
	}
	directive, perr := pipeline.ParseRedirectEntry(data)
	if perr != nil || directive == nil || strings.TrimSpace(directive.TargetPath) == "" {
		return "", 0, false
	}
	code = directive.StatusCode
	if code != http.StatusMovedPermanently && code != http.StatusFound {

		code = http.StatusMovedPermanently
	}
	return directive.TargetPath, code, true
}
