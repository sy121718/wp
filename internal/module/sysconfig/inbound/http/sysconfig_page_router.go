package sysconfighttp

// sysconfig_page_router.go — 系统设置页（/admin/system）的注册落点。
//
// 页面写操作复用 API 权限点（AGENTS：加常量 + 在路由注册处声明，不写 seed 迁移）：
// 页面前缀 /admin/system 与权限点路径 /api/sysconfig/* 不一致，必须用
// CasbinMiddlewareForPath 显式指定 obj —— 直接按页面路径 enforce 会全员 403
// （权限点表里没有页面路径）。
//
// 页面 GET 的 Casbin 已挂（借 /api/sysconfig/get 的读权限点）；路由注册只出现在
// *_router.go，门禁 scripts/check-route-registration-placement.sh 守这条。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/shell"
)

// SetupSysConfigPages 注册系统设置页（adminPages = /admin 后台页面组）。
// adminPages 为 nil 时整体跳过（与 rg == nil 的早退同构）。
func SetupSysConfigPages(adminPages *gin.RouterGroup, h *AdminHandle) {
	if adminPages == nil || h == nil {
		return
	}
	adminPages.GET("/system", shell.PageAuthz("/api/sysconfig/get"), h.SystemPage)
	adminPages.POST("/system/save", builtin.CasbinMiddlewareForPath("/api/sysconfig/save"), h.SystemSave)
}
