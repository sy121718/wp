package adminui

// authz.go — 后台**页面 GET** 鉴权的路由侧唯一约定写法（task-92 批 4a 自 internal/shell/authz.go 迁入）。
//
// 分层：判定在 internal/middleware/builtin（本包不重复判定），拒绝形态在本包的 reject.go
// （本包不渲染整页以外的形态）。本文件只剩一件事：把判定与本包的拒绝出口**绑成中间件**。
//
// 为什么接线的两端都在这里：出口（reject.go）在本包，判定在 builtin，而本包本来就 import
// builtin（reject.go 的 PageRejectFunc 类型），所以 adminui → builtin 是既有方向，不成环。
// 反过来让 builtin 自己默认调本包出口才会成环：builtin 管请求管道、刻意不 import 任何渲染代码
// （见 internal/middleware/builtin/page_authz.go 文件头），出口只能由调用方注入。
//
// 为什么不是留在壳层：`internal/shell` 正在按计划整体删除，页面鉴权的路由侧写法与
// 「拒绝长什么样」同属后台外壳知识，终局归属就是本包（迁移计划表见
// internal/shell/shell_migration_test.go；docs/rules/response-shapes.md 的「计数口径与时点」）。
//
// 分档（与迁移前逐字一致，实现在 reject.go）：
//   · 未登录 → 302 到 /admin/login，与既有页面语义一致（API 侧返回 401 JSON）；
//   · 无权限 → 真实状态码 + 整页提示（jump 形态），不是 JSON 报文 —— 直接输 URL 的人看得懂
//     发生了什么、知道去找谁开权限；
//   · htmx 请求 → HX-Redirect 回后台首页：htmx 对 4xx 默认不 swap，直接回 403 的表现是
//     「点了没反应」，比看到提示页更糟。
//
// 壳键注入器固定传本包 Prepare：拒绝页要渲染完整侧栏（layout 会 include
// partials/sidebar.html），所以壳键必须是全量那一套，不能只给一条消息。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

// PageAuthz 后台页面 GET 鉴权中间件的**唯一约定写法**：判定取 builtin 的实现，
// 出口绑本包的 Reject。页面 router 里写在 GET 的第二个参数位置。
//
//	adminPages.GET("/roles", adminui.PageAuthz("/api/role/list"), handle.RolesPage)
//
// obj 必须是**该页菜单绑定的权限码**所指的 API 路径 —— 双向对应由
// scripts/check-page-get-authz.sh 守着（obj 写错的表现是「菜单看得见、点进去被拒」）。
func PageAuthz(obj string) gin.HandlerFunc {
	return builtin.PageCasbinMiddleware(obj, Reject(Prepare))
}

// PageAuthzAs 同 PageAuthz，但显式指定 act —— 只给「页面是 GET，而该页菜单绑的权限点
// 声明为 POST」这类存量特例用（见 builtin.PageCasbinMiddlewareAs 的说明）。
func PageAuthzAs(obj, act string) gin.HandlerFunc {
	return builtin.PageCasbinMiddlewareAs(obj, act, Reject(Prepare))
}
