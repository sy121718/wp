package mediahttp

// media_page_router.go — 媒体库后台页面的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 media_page_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。

import "github.com/gin-gonic/gin"

// SetupMediaPages 注册媒体库页面（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupMediaRoutes，不能同名。
// adminPages 为 nil 时整体跳过。
func SetupMediaPages(adminPages *gin.RouterGroup) {
	if adminPages == nil {
		return
	}
	adminPages.GET("/media", MediaPage)
}
