package sysconfighttp

// sysconfig_page_router.go — 系统设置页（/admin/system）的注册落点。
//
// 写操作由浏览器直调 /api/sysconfig/system/save，权限在 API 注册处声明。
//
// 页面 GET 的 Casbin 已挂（借 /api/sysconfig/get 的读权限点）；路由注册只出现在
// *_router.go，门禁 scripts/check-route-registration-placement.sh 守这条。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
)

// SetupSysConfigPages 注册系统设置页（adminPages = /admin 后台页面组）。
// adminPages 为 nil 时整体跳过（与 rg == nil 的早退同构）。
func SetupSysConfigPages(adminPages *gin.RouterGroup, h *AdminHandle) {
	if adminPages == nil || h == nil {
		return
	}
	adminPages.GET("/system", adminui.PageAuthz("/api/sysconfig/get"), h.SystemPage)
}
