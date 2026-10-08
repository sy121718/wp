package masterdatahttp

// masterdata_page_router.go — 后台「变更记录」页（/admin/masterdata/changes）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 masterdata_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）。

import (
	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// SetupMasterDataPages 注册后台「变更记录」页。
//
// 只读页面，没有写表单 —— 记录由业务模块在写操作里经本模块契约追加，
// 后台不提供「手工补一条」的口子。pages 为 nil 时整体跳过。
func SetupMasterDataPages(pages *gin.RouterGroup, svc masterdatacontract.MasterDataService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	changePages := NewMasterDataChangePageHandle(svc, project)
	pages.GET("/masterdata/changes", changePages.MasterDataChangesPage)
}
