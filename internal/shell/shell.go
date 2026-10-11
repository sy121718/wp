// Package shell 承载后台页面的公共外壳：多语言、CSRF、权限上下文、分页、
// 错误出口与表单工具。
//
// 为什么要有这个包：这些是「每个后台页面都必须有、但不属于任何业务域」的东西。
// 各业务模块的页面 handler 调 adminui.Prepare 就能拿到同一套壳（与页面回迁前逐字节一致）
// （同一套侧栏、同一套权限过滤、同一套文案回退），不必各自抄一份 —— 抄漏一处就会长出
// 一个没有侧栏的孤儿页。
//
// 装配的归属：外壳装配（Prepare 一族）自 task-92 批 2a 起归 internal/adminui ——
// 它注入的壳键与侧栏视图本来就是同一件事的两半（视图构造自 task-72 起已在那边）。
// order 五个生产文件的六处装配调用已直引 adminui.Prepare，本包的过渡转发已删除。
//
// 权限上下文的归属：中间件与三个请求上下文键均在 internal/middleware/adminctx，
// 它认识 admin 模块的权限契约；页面装配由 internal/adminui 读取同一组键。
// 本包不再声明权限上下文键。
//
// 数据来源：侧栏菜单的真源是 sys_menus 表。中间件取到当前用户的有效权限码后，由 admin
// 模块的 BuildAuthorizedTree 过滤成树放进上下文；装配侧（adminui.Prepare）只负责把它交给
// adminui.BuildNav 并标记当前页 —— 视图构造（NavGroup / NavNode、路径段归属、隐藏项过滤）
// 自 task-72 起归 internal/adminui。
package shell

import (
	"go_wp/internal/templates"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 权限上下文中间件与导航树读取器已于 task-92 批 3 迁出/删除；
// 请求上下文键现由 internal/middleware/adminctx 声明。失败语义见该包注释。

// TranslateFor 返回 Go 侧文案翻译函数（分页条等由 Go 拼接、不经模板的文案）。
// c 为 nil 时按默认语言返回，绝不 panic。
func TranslateFor(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

// adminHomePath 控制面首页（仪表盘）。
//
// 曾经回落 "/"，但 **/ 现在归前台首页**（站点独占域名根）—— 后台的
// 「回到首页」类回落若还指向 "/"，操作者会被丢到店铺首页，
// 看起来像登录态丢了。菜单树里「仪表盘」的 path 本来就是 /admin，两边一致。
const adminHomePath = "/admin"

// requestURI 返回当前请求的站内 URI（含 query）；异常时回控制面首页。
func requestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return adminHomePath
	}
	return uri
}
