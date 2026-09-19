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
	"net/http"
	"strings"

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
	// SiteRedirectMiddleware 在前：改 URL 后的旧路径是「指向 redirect.json 的激活链接」，
	// http.FileServer 只读文件、不认识它 —— 少了这一层，勾了「保留旧链接」的旧路径
	// 表现是 404（承诺未兑现）。重定向判定不查库，访问面零查库不变量不变。
	router.Group(siteFacePath, builtin.SiteRedirectMiddleware(), builtin.SiteCacheMiddleware(), builtin.StaticGzipMiddleware()).
		StaticFS("/", gin.Dir(pipeline.ActiveRoot(), false))
}

// siteFacePath 静态访问面挂载前缀（与内置中间件的 siteFacePrefix 同值，
// 分属两包：那边是 middleware 的私有常量）。
//
// 它同时是「这个请求属不属于访问面」的判据 —— 404 响应要按前缀分流：
// 访问面给访客，控制面（/api、/admin）保持既有的统一 JSON 错误。
const siteFacePath = "/site"

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
		if serveSiteNotFoundPage(c) {
			return
		}
		response.NotFound(c, "请求的资源不存在")
	}
}

// serveSiteNotFoundPage 访问面未知路径命中站点自定义 404 页时写出响应并返回 true。
//
// 判定只看请求前缀与激活目录根的那一个文件，不查库、不读路由表 ——
// 访问面「零查库零模板」不变量不变（与 SiteRedirectMiddleware 同一口径）。
// 前缀按「等于 /site 或以 /site/ 开头」判，避免把 /siteadmin 这类路径误当访问面。
func serveSiteNotFoundPage(c *gin.Context) bool {
	p := c.Request.URL.Path
	if p != siteFacePath && !strings.HasPrefix(p, siteFacePath+"/") {
		return false
	}
	body, ok := pipeline.ReadNotFoundPage(pipeline.ActiveRoot())
	if !ok {
		return false
	}
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", body)
	return true
}
