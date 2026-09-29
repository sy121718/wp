// routes.go — 主路由聚合（页面路由 + 各模块自装配路由）。
//
// 权限策略的两个坑（排查 403 时先看这里）：
//  1. Casbin 策略在启动时从 sys_casbin_rule 载入内存，**改库后不会自动重载** ——
//     新接口的 seed 迁移必须配合进程重启才生效，否则表现为「策略已写、接口仍 403」；
//  2. 策略按 v0 = user_id 授权（超管是 user_id=1），**is_admin=1 不自动放行** ——
//     新建的管理员账号需要在 seed/后台里单独授权，否则登录后各接口一律 403。
//
// 本文件只保留装配入口（SetupRoutes）与静态访问面/404 处理；
// 具体的装配段落见 assembly.go 与 assembly_publish.go（审计 CQ-008 的拆分）。
package routers

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/permission"
	"go_wp/internal/pipeline"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 注册全部路由。
//
// 装配说明：管理面六领域（管理员/角色/权限/菜单/部门/数据权限）合并为 admin 大模块，
// 模块内部同包直调、自包含装配；workbench、media、captcha 为独立模块。
//
// 下面这些调用**必须保持当前顺序**：装配是线性的 —— 后一段依赖前一段构造出的契约，
// 端口注入必须发生在两侧都构造完成之后。拆分只为了可读性（审计 CQ-008），
// 不是为了让它们可以并行或重排；顺序一旦改动，表现为「某个端口注入到了空实现上」，
// 而这种漂移编译期看不出来（接口断言仍在，只是断言的时机不对）。
func SetupRoutes(router *gin.Engine, ready func() error) {
	if router == nil {
		return
	}

	// 每次装配重新采集权限点声明（审计 SEC-011）：声明表描述的是「本次装配的路由 → 权限点」，
	// 同一进程内多次装配（测试里多个用例各装配一次）时累积会导致「重复声明」误报。
	permission.Reset()

	// 装配期端口标记（审计 CQ-019）：每完成一次端口注入就在对应段落里标记一次，
	// 末尾由 mustAllPortsWired 统一自检必需端口是否齐全（清单见 wiring.go）。
	a := &assembly{router: router, marks: newWiringMarks()}

	// 基础设施（模板渲染器 / 静态资源 / 访问面 / 健康检查 / db / 权限 seed）
	a.buildFoundation(ready)
	if a.db == nil {
		// 数据库未就绪：buildFoundation 已记日志；与原实现一致地停止后续装配。
		return
	}

	// 业务服务装配（/api 三层链 + 各模块自装配，按依赖顺序）
	a.buildAPIAndCoreCRUD()
	a.buildIdentityAndCommerce()

	// 端口注入（商品 ↔ 库存、访问面运行时、发布侧）
	a.wireProductInventoryPorts()
	a.wireRuntimeAccessFace()
	a.buildPublishingModules()
	a.wirePublishingPorts()
	// 模板引用反查（影响面提示与删除保护的数据源）：page / presentation 契约齐备后接线。
	a.wireContentTemplateImpact()

	// 启动期后台任务与运行时接线
	a.startRuntimeTasks()

	// 后台页面 → 装配自检 → 公开端点与兜底路由
	a.mountAdminPages()
	a.runSelfCheck()
	a.mountPublicFace()
}

// setupStaticFace 挂载静态访问面（docs/03-pipeline.md §5）。
//
// ActiveRoot 位于产物根下两级（{root}/public/active，pipeline.ActiveRoot() 单源），
// 符号链接目标相对可达。因此访问面根必须是 active 目录本身；文件由 StaticFS
// 只读直出，/ 落到 index 入口。
// 注意必须无条件挂载：首次发布发生在启动之后，启动时目录必然不存在，
// 若按目录存在与否跳过挂载，静态访问面将永远无法生效（每次请求动态读盘，
// 目录与产物在首次发布后即时生效）。
//
// gin.Dir(listDirectory=false) 底层仍是 http.Dir（符号链接跟随行为不变，
// 不限制 activeRoot 的 symlink 访问面），仅禁用 Readdir 以阻止目录列表
// （审计 Low：/site 目录列表开启）。
// 访问面文本产物（HTML/CSS/JS）经 StaticGzipMiddleware 传输压缩提速。
func setupStaticFace(router *gin.Engine) {
	root := pipeline.ActiveRoot()

	// 解锁端点（PIPE-6 AccessGuard）**必须显式注册**：站点独占域名根之后，
	// 根挂载点的静态面是 NoRoute，未显式注册的路径会被它当「站点里没有这个路径」
	// 吃掉（表现是守卫页提交后 404）。
	//
	// 限流挂在这里而不是全局：bcrypt 是慢哈希，没有次数上限的解锁端点仍然是
	// 「一次脚本跑一整本字典」的成本（慢哈希只把爆破成本乘了个常数）。
	// 额度与后台登录同档（每 IP 每分钟 10 次）。
	//
	// 路径落在站点命名空间里（/access/unlock）：站点若正好有一个页面的 URL 是
	// 这个路径，显式路由优先 —— 那属于保留路径，页面侧应当另选地址。
	router.POST(accessUnlockPath, builtin.RequestRateLimitMiddleware(10, time.Minute),
		builtin.AccessUnlockHandler())

	// 兼容入口：/site（控制台里的「打开站点」与既有书签仍可用）。
	router.Group(siteFacePath, siteFaceChain(siteFacePath)...).StaticFS("/", gin.Dir(root, false))

	// 根入口：**站点独占域名根**（生产语义，也是开发环境与线上一致的前提）。
	//
	// 为什么走 NoRoute 而不是再挂一个 StaticFS("/")：
	// StaticFS 会注册 /*filepath 通配路由，与控制面已有的 /admin/*、/api/* 等通配
	// 在 gin 的路由树上冲突（启动即 panic）。NoRoute 不注册任何路由，天然无冲突 ——
	// 控制面的显式路由优先匹配，剩下的路径全部按激活产物解析。
	//
	// 这条路径顺带解决了「站点与接口不同域」的问题：站内绝对链接（href="/shop"）、
	// 媒体（/storage）与动态片段（/_fragments）现在天然同域生效，
	// 不再需要独立端口的预览服务器去代理。
	chain := append(siteFaceChain(siteFaceRootPath), siteFileServeMiddleware(root), notFoundHandler())
	router.NoRoute(chain...)
}

// accessUnlockPath 访问面解锁端点路径（与 builtin.AccessUnlockPath 同值）。
//
// 常量分属两包（那边是 middleware 的公开常量）：这里引用它即可，不重复写字面量 ——
// 两处分叉的表现是「守卫页表单提交到一个没人注册的地址」，而两边都编译通过。
const accessUnlockPath = builtin.AccessUnlockPath

// siteFaceChain 访问面中间件链（挂载点无关）。
//
// 排位有讲究：SiteRedirect 在前（301 响应没有 body，压缩无从谈起）；
// AccessGuard 紧随其后（它要在 SiteCache 之前终结受限请求，自己下发
// private/no-store 覆盖公开缓存头，且必须在任何静态文件处理之前）；
// siteDirIndexServe 在后（它在 StaticGzip 之后落桶，首页与其它产物一样有传输压缩）。
func siteFaceChain(prefix string) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		builtin.SiteRedirectMiddleware(prefix),
		builtin.AccessGuardMiddleware(prefix),
		builtin.SiteCacheMiddleware(),
		builtin.StaticGzipMiddleware(),
		siteDirIndexServeMiddleware(prefix),
	}
}

// activeEntryFile URL 路径（站点内相对路径）→ 激活目录里的产物文件（相对路径）。
//
// 产物布局：<active>/<条目>/index.html，**条目名就是 URL 路径**
// （pipeline.relActivePath），且 "/" 的条目名是字面量 "index"。
//
// 「哪一个是本次请求的条目」由 pipeline.ResolveActiveEntry 单源判定
// （/about 与 /about/index.html 取同一份产物；页面 URL 真叫 /foo/index.html 时
// 它又是另一份产物）。这里只负责把条目名加上入口文件名。
//
// 为什么必须单源：这条映射此前在访问面与守卫各写一份，而两份的任何一处差异
// 都会让守卫被绕过 —— 守卫判 `/about` 受限、访问面对 `/about/index.html`
// 原样直出。原注释里那两处易错（拼成 <rel>/index/index.html、rel 为空时直接拼
// <root>/index.html）现在由同一个函数统一处理：**只有首页能打开、其余页面与文章
// 整片 404** 那类症状不会再有第二份成因。
func activeEntryFile(root, rel string) (string, bool) {
	entry, ok := pipeline.ResolveActiveEntry(root, rel)
	if !ok {
		return "", false
	}
	file := filepath.ToSlash(filepath.Join(filepath.FromSlash(entry), "index.html"))
	return file, true
}

// siteFileServeMiddleware 命中激活产物时直出文件；未命中则交给链尾的 404 处理。
//
// 判据只看文件系统（访问面「零查库零模板」同一口径）。
// 安全：路径先 Clean 并拒绝绝对路径与 ".." 前缀 —— 这条通道不能成为目录穿越入口。
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
		// ① URL 路径 → 页面产物（<条目>/index.html）。
		if file, hit := activeEntryFile(root, rel); hit {
			serveArtifactFile(c, filepath.Join(root, filepath.FromSlash(file)))
			return
		}
		// ② 产物内的普通文件（sitemap.xml、robots.txt、favicon 等）。
		target := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(target); err == nil && !st.IsDir() {
			serveArtifactFile(c, target)
			return
		}
		c.Next() // 没有对应产物：交给链尾的 404
	}
}

// cleanSiteRel 站内路径归一；越界（绝对路径 / ".."）返回 ok=false。
//
// 唯一实现在 pipeline.CleanSiteRel：访问面用它取文件、守卫用它取判定路径，
// 两份实现分叉就是一条绕过（归一松掉一处，静态面把请求解析成受限页面的产物，
// 而守卫按另一个路径判定为「没有守卫」）。
func cleanSiteRel(reqPath string) (string, bool) {
	return pipeline.CleanSiteRel(reqPath)
}

// serveArtifactFile 直出产物文件。
//
// 为什么不用 http.FileServer：它对以 **/index.html 结尾**的路径有自己的一套
// 301 语义（localRedirect "./"）—— 我们把目录请求改写成 <条目>/index.html 交给它，
// 换来的是一条 301 到站点根，表现是「除首页外全站 301 回首页」。
// http.ServeContent 只做「按文件出内容」，没有目录语义，同时保住
// Last-Modified / ETag / Range 协商缓存（与 FileServer 的缓存能力等价）。
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
	ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if strings.HasSuffix(strings.ToLower(path), ".html") {
		ctype = "text/html; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	c.Header("Content-Type", ctype)
	http.ServeContent(c.Writer, c.Request, filepath.Base(path), st.ModTime(), f)
	c.Abort()
}

// siteDirIndexServeMiddleware 直出「目录根请求」对应的 index 条目。
//
// 为什么是直出而不是改路径交给 http.FileServer：FileServer 对以 /index.html 结尾的
// 路径有**自己的**301 语义（localRedirect "./"），改写过去只会换来一条 301 死循环
// （实测 /site/ → 301 "./"、/site/index/index.html → 301 "/index"）。所以这里读字节
// 直接写响应，并 c.Abort() 挡住后面的静态处理。
//
// 排位：**在 StaticGzipMiddleware 之后**，这样 c.Data 走的是已被 gzip 包裹的
// c.Writer，首页与其它产物一样有传输压缩与 Cache-Control。
//
// 存在的理由（原本是审计里明确挂着的一条未修项）：激活目录里每个页面的条目名就是它的
// URL 路径 —— 无扩展名的**目录符号链接**（pipeline.relActivePath："/" → "index"、
// "/about" → "about"），index.html 在那个目录里面。而 http.FileServer 处理
// "/site/"、"/site/en/" 这类目录根请求时找的是 <active>/<rel>/index.html，
// 找不到 → 默认 404。表现是**首页与多语言语言根整片打不开**，其余页面正常。
//
// 只修 "/site/" 是半修：语言根（/site/en/）是同一套映射语义，必须一起覆盖，
// 所以这里按「目录根 + index 子条目」这条统一规则判定，而不是给首页开特例。
//
// 判据只看文件系统（与访问面「零查库零模板」同一口径）：命中才直出，
// 不命中一律原样交给 FileServer 走它自己的 index.html / 目录逻辑。
// 路径先 Clean 并拒绝 ".."，这条通道不能成为目录穿越的新入口。
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
			// 归一与其它访问面通道同源（pipeline.CleanSiteRel）。
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

// siteFacePath 静态访问面挂载前缀（与内置中间件的 siteFacePrefix 同值，
// 分属两包：那边是 middleware 的私有常量）。
//
// 它同时是「这个请求属不属于访问面」的判据 —— 404 响应要按前缀分流：
// 访问面给访客，控制面（/api、/admin）保持既有的统一 JSON 错误。
const siteFacePath = "/site"

// siteFaceRootPath 站点根挂载前缀（空串 = 独占域名根）。
// 与 siteFacePath 一起构成两个挂载点，中间件链按各自前缀参数化。
const siteFaceRootPath = ""

// notFoundHandler 未匹配路由的响应：访问面（/site）优先返回站点自定义 404 页。
//
// 为什么访问面的 404 会走到这里：gin 的静态文件 handler 在 fs.Open 失败时
// 把 handler 链整体换成 NoRoute（v1.12 createStaticHandler），所以 /site 下
// 未知路径的响应体由本函数决定。少了这一层，访客拿到的是后台 API 形态的 JSON
// 「请求的资源不存在」—— 没有站名、没有导航，死链等于直接流失（审计 SEO-013）。
//
// 状态码恒为 404：自定义页只换响应体与 Content-Type，语义不动。绝不能返 200 ——
// 那是软 404，搜索引擎会把死链当有效页面收进索引，正是要避免的另一半问题。
// 未配置自定义页时行为与既有一致（response.NotFound）。
func notFoundHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 访问面在根上：任何落到这里的 GET/HEAD 都已经是「站点里没有这个路径」，
		// 所以优先用站点自定义 404 页。控制面路径（/api、/admin…）保持既有 JSON ——
		// 前端脚本按 JSON 形状解析错误，换成 HTML 会让它们全部失效。
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

// controlPlanePrefixes 控制面路径前缀（与显式注册的路由一致）。
//
// 用途只有一个：兜底 404 时区分「访客走错了站点路径」与「前端调错了接口」——
// 前者给站点 404 页、后者给 JSON，两者混用会让其中一边彻底失效。
var controlPlanePrefixes = []string{
	"/api/", "/api", "/admin/", "/admin", "/workbench/", "/workbench",
	"/_fragments/", "/_fragments", "/storage/", "/storage", "/static/", "/static",
	"/livez", "/readyz", "/analytics/", "/payment/",
}

// isControlPlanePath 判断路径是否属于控制面。
func isControlPlanePath(p string) bool {
	for _, prefix := range controlPlanePrefixes {
		if p == prefix || strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// serveSiteNotFoundPage 访问面未知路径命中站点自定义 404 页时写出响应并返回 true。
//
// 判定只看请求前缀与激活目录根的那一个文件，不查库、不读路由表 ——
// 访问面「零查库零模板」不变量不变（与 SiteRedirectMiddleware 同一口径）。
// 前缀按「等于 /site 或以 /site/ 开头」判，避免把 /siteadmin 这类路径误当访问面。
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
