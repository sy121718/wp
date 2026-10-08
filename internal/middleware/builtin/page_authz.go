package builtin

// page_authz.go — 后台**页面 GET** 的鉴权判定（审计项见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）。
//
// 背景：菜单按权限渲染，但**菜单隐藏不是访问控制** —— 任何登录账号直接输 URL 就能进。
// 实测（2026-10-08）：页面组 GET 里只有 28 条挂了 Casbin，analytics / content / block /
// order / product / user / mail / inventory 等整片区域裸奔；`admin_pages_router.go` 的文件头
// 记过一次真实事故（`/admin/administrators` 把全部管理员的用户名 / 姓名 / 邮箱 / 手机号
// 露给任何登录账号）。
//
// 本文件只管两件事：**判定**与**拒绝标记**。
//   · 判定走 CasbinDecide（sub=用户 id, obj=权限点路径, act 固定 GET），与 `authorizedAPI`
//     组用同一个 enforcer、同一批策略 —— 页面与 JSON API 是**一条**链，不是两条；
//   · obj 一律 `/api/*` 形态：页面**借**对应读 API 的权限点（`/admin/blocks` 借
//     `/api/block/list` 的 `block:list`），权限点表不必新增页面路径形态的行；
//     副作用是 **URL 与鉴权解耦** —— `sys_menus.path` 是可改的数据（docs/02-Z §4.2），
//     而 Casbin obj 是 API 路径，改后台 URL 不需要碰任何策略、权限点或菜单绑定。
//     反过来若把 obj 设成页面路径，每改一次 URL 都要迁一次权限数据（那正是这几份文档
//     要消除的第三份真源）。
//
// **拒绝的形态不在这里**：页面被拒要出整页提示（直接输 URL 的人看得懂发生了什么、知道
// 找谁开权限），而渲染属于表现层。因此出口由调用方注入（PageRejectFunc），本包不 import
// 任何渲染代码 —— 中间件管请求管道，不管 HTML。
//
// 唯一约定写法（页面 router 里，放在 `pages.GET(...)` 的第二个参数位置）：
//
//	pages.GET("/blocks", shell.PageAuthz("/api/block/list"), h.BlocksList)
//
// shell.PageAuthz 是薄包装：把 shell.RejectPageRequest 绑到本中间件上。

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PageRejectFunc 页面请求被拒时的表现层出口：
// status 为 401/403/500，key 为 pkg/enums 里的文案键（判定层不认识 i18n，只给键）。
type PageRejectFunc func(c *gin.Context, status int, key string)

// PageCasbinMiddleware 后台页面 GET 的鉴权中间件，obj 为该页借用的读权限点路径。
//
// obj 必须与**该页菜单绑定的权限码**指向同一个 API 路径（菜单 `/admin/xxx` 绑的码 →
// sys_permission.api_path），否则会出现「菜单可见、点进去被拒」。门禁
// scripts/check-page-get-authz.sh 双向守这条。
func PageCasbinMiddleware(obj string, reject PageRejectFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, key := CasbinDecide(c, obj, http.MethodGet)
		if status == 0 {
			c.Next()
			return
		}
		reject(c, status, key)
		c.Abort()
	}
}
