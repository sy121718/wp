package routers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/pipeline"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(router *gin.Engine, ready func() error) {
	SetupRoutesWithContext(context.Background(), router, ready)
}

// SetupRoutesWithContext 装配路由，并让后台循环挂在这个 ctx 上。
//
// 进程收尾时调用方先 cancel 再 Shutdown：循环在取消后立刻停止领取新任务，
// 不会出现「库已经关了，worker 还在领任务」。
// 不传 ctx 的 SetupRoutes 保留给测试与历史调用点，它等价于一个永不过期的 ctx。
func SetupRoutesWithContext(ctx context.Context, router *gin.Engine, ready func() error) {
	if router == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mw.Reset()

	a := &assembly{router: router, marks: newWiringMarks(), bgCtx: ctx}

	a.buildFoundation(ready)
	if a.db == nil {

		return
	}

	a.buildAPIAndCoreCRUD()
	a.buildIdentityAndCommerce()

	a.wireProductInventoryPorts()
	a.wireRuntimeAccessFace()
	a.buildPublishingModules()
	a.wirePublishingPorts()

	a.wireContentTemplateImpact()

	a.startRuntimeTasks()

	a.mountAdminPages()
	a.runSelfCheck()
	a.mountPublicFace()
}

func setupStaticFace(router *gin.Engine) {
	root := pipeline.ActiveRoot()

	router.POST(accessUnlockPath, builtin.RequestRateLimitMiddleware(10, time.Minute),
		builtin.AccessUnlockHandler())

	router.Group(siteFacePath, siteFaceChain(siteFacePath)...).StaticFS("/", builtin.NoDirListFS(root))

	chain := append(siteFaceChain(siteFaceRootPath), siteFileServeMiddleware(root), notFoundHandler())
	router.NoRoute(chain...)
}

const accessUnlockPath = builtin.AccessUnlockPath

func siteFaceChain(prefix string) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		builtin.SiteRedirectMiddleware(prefix),
		builtin.AccessGuardMiddleware(prefix),
		builtin.SiteCacheMiddleware(),
		builtin.PrecompressedAssetMiddleware(prefix),
		builtin.StaticGzipMiddleware(),
		siteDirIndexServeMiddleware(prefix),
	}
}

func activeEntryFile(root, rel string) (string, bool) {
	entry, ok := pipeline.ResolveActiveEntry(root, rel)
	if !ok {
		return "", false
	}
	file := filepath.ToSlash(filepath.Join(filepath.FromSlash(entry), "index.html"))
	return file, true
}

func siteFileServeMiddleware(root string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		rel, ok := cleanSiteRel(c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}

		if file, hit := activeEntryFile(root, rel); hit {
			serveArtifactFile(c, filepath.Join(root, filepath.FromSlash(file)))
			return
		}

		target := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(target); err == nil && !st.IsDir() {
			serveArtifactFile(c, target)
			return
		}
		c.Next()
	}
}

func cleanSiteRel(reqPath string) (string, bool) {
	return pipeline.CleanSiteRel(reqPath)
}

func serveArtifactFile(c *gin.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		c.Next()
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		c.Next()
		return
	}

	c.Header("Content-Type", pipeline.AssetContentType(path))
	http.ServeContent(c.Writer, c.Request, filepath.Base(path), st.ModTime(), f)
	c.Abort()
}

func siteDirIndexServeMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		reqPath := c.Request.URL.Path
		if !strings.HasSuffix(reqPath, "/") {
			c.Next()
			return
		}
		rel, inFace := builtin.SiteFaceRel(prefix, reqPath)
		if !inFace {
			c.Next()
			return
		}
		rel = strings.Trim(rel, "/")
		if rel != "" {

			clean, ok := pipeline.CleanSiteRel(rel)
			if !ok {
				c.Next()
				return
			}
			rel = clean
		}
		file, ok := activeEntryFile(pipeline.ActiveRoot(), rel)
		if !ok {
			c.Next()
			return
		}
		body, err := os.ReadFile(filepath.Join(pipeline.ActiveRoot(), filepath.FromSlash(file)))
		if err != nil {
			c.Next()
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", body)
		c.Abort()
	}
}

const siteFacePath = "/site"

const siteFaceRootPath = ""

func notFoundHandler() gin.HandlerFunc {
	return func(c *gin.Context) {

		if isControlPlanePath(c.Request.URL.Path) {
			response.NotFound(c, "请求的资源不存在")
			return
		}
		if serveSiteNotFoundPage(c, siteFaceRootPath) {
			return
		}
		response.NotFound(c, "请求的资源不存在")
	}
}

var controlPlanePrefixes = []string{
	"/api/", "/api", "/admin/", "/admin", "/workbench/", "/workbench",
	"/storage/", "/storage", "/static/", "/static",
	"/livez", "/readyz", "/analytics/", "/payment/",
}

func isControlPlanePath(p string) bool {
	for _, prefix := range controlPlanePrefixes {
		if p == prefix || strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func serveSiteNotFoundPage(c *gin.Context, prefix string) bool {
	if _, inFace := builtin.SiteFaceRel(prefix, c.Request.URL.Path); !inFace {
		return false
	}
	body, ok := pipeline.ReadNotFoundPage(pipeline.ActiveRoot())
	if !ok {
		return false
	}
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", body)
	return true
}
