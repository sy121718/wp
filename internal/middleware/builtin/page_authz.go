package builtin

// 后台页面 GET 的权限判定沿用 CasbinDecide，与 API 使用同一策略。
// 拒绝由调用方注入出口，本包不依赖模板；adminui.PageAuthz 将其接到 JSON 失败协议。
// 页面可复用对应 API 权限点，obj/act 与该权限点的请求路径和方法一致。

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
	return PageCasbinMiddlewareAs(obj, http.MethodGet, reject)
}

// PageCasbinMiddlewareAs 同 PageCasbinMiddleware，但显式指定 act。
//
// 只给「页面是 GET，而该页菜单绑的权限点声明为 POST」这类存量特例用（如 /admin/seo →
// seo:audit → POST /api/seo/audit）：enforce 的码必须与菜单绑定的码一致，否则菜单可见、
// 点进去被拒。新页面一律用 PageCasbinMiddleware（act 固定 GET）。
func PageCasbinMiddlewareAs(obj, act string, reject PageRejectFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, key := CasbinDecide(c, obj, act)
		if status == 0 {
			c.Next()
			return
		}
		reject(c, status, key)
		c.Abort()
	}
}
