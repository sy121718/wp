package adminui

// 页面 GET 的权限判定沿用 builtin，拒绝统一返回 JSON。
// 本包只负责把判定与拒绝出口装配为中间件。

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
	return builtin.PageCasbinMiddleware(obj, Reject())
}

// PageAuthzAs 同 PageAuthz，但显式指定 act —— 只给「页面是 GET，而该页菜单绑的权限点
// 声明为 POST」这类存量特例用（见 builtin.PageCasbinMiddlewareAs 的说明）。
func PageAuthzAs(obj, act string) gin.HandlerFunc {
	return builtin.PageCasbinMiddlewareAs(obj, act, Reject())
}
