package routers

import (
	"context"
	publish "go_wp/pkg/publish"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_wp/internal/middleware/builtin"
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
	SetupRoutesWithRegistry(ctx, router, ready)
}

// SetupRoutesWithRegistry 与 SetupRoutesWithContext 相同，但把本次装配的登记索引交回调用方。
//
// 审计/契约工具要读「这一实例真正登记的绑定」，不能读包级默认实例：
// 默认实例只服务于没有装配索引的模块单测。
func SetupRoutesWithRegistry(ctx context.Context, router *gin.Engine, ready func() error) *mw.Registry {
	if router == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 每次装配建一个登记索引：它随本次的 RouteGroup / 模块装配下发，冻结后只读。
	// 包级 mw.Snapshot()/Reset() 只服务于没有装配索引的模块单测，
	// 因此测试里的 Reset 不会清掉正在服务实例的索引。
	a := &assembly{router: router, marks: newWiringMarks(), bgCtx: ctx, registry: mw.NewRegistry()}

	// 页面渲染的请求事实来源（CSRF / 授权菜单树）由应用注入 pkg/view。
	wireAdminViewPort()
	// 模板引擎侧的渲染作用域（取词 / 失败展开 / 导航投影）由应用注入根包。
	wireViewRenderScope()

	a.buildFoundation(ready)
	if a.db == nil {

		return a.registry
	}

	a.buildAPIAndCoreCRUD()
	a.buildIdentityAndCommerce()

	a.wireProductInventoryPorts()
	a.wireRuntimeAccessFace()
	a.buildPublishingModules()
	a.wirePublishingPorts()

	a.wireContentTemplateImpact()
	a.wireTemplateReferencePorts()

	a.startRuntimeTasks()

	a.mountAdminPages()
	a.mountPublicFace()

	// 全部路由与工具登记完成后才冻结：之后任何迟到注册都会明确 panic/报错，
	// 不会出现「启动后目录还在悄悄长」的运行时重建。
	a.registry.Freeze()
	a.tools().Freeze()

	a.runSelfCheck()
	return a.registry
}

func setupStaticFace(router *gin.Engine) {
	root := publish.ActiveRoot()

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
	entry, ok := publish.ResolveActiveEntry(root, rel)
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
	return publish.CleanSiteRel(reqPath)
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

	c.Header("Content-Type", publish.AssetContentType(path))
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

			clean, ok := publish.CleanSiteRel(rel)
			if !ok {
				c.Next()
				return
			}
			rel = clean
		}
		file, ok := activeEntryFile(publish.ActiveRoot(), rel)
		if !ok {
			c.Next()
			return
		}
		body, err := os.ReadFile(filepath.Join(publish.ActiveRoot(), filepath.FromSlash(file)))
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
	body, ok := publish.ReadNotFoundPage(publish.ActiveRoot())
	if !ok {
		return false
	}
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", body)
	return true
}
