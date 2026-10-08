package blockhttp

// block_page_router.go — 全局块管理页（/admin/blocks）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 block_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

// SetupBlockPages 注册全局块管理页（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupBlockRoutes，不能同名。
// adminPages 为 nil 时整体跳过。
//
// pages 为可选变参（装配层传入 page 契约后「影响面」才可用，见 block_page_impact.go）。
// 用变参而不是必填参数：漏传时页面降级为「影响面未装配」的明确提示，而不是启动失败 ——
// 这一批只做可见性，不该把块管理页的可用性与它绑死。
func SetupBlockPages(adminPages *gin.RouterGroup,
	blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) {
	if adminPages == nil {
		return
	}
	h := NewBlockPageHandle(blocks, projects, pages...)
	// 页面 GET 鉴权：借列表读权限点（菜单 /admin/blocks 绑 block:list → GET /api/block/list）。
	// 与页面写操作同一手法（不同动词各挂各的中间件），页面与 API 共用一条 Casbin 链。
	adminPages.GET("/blocks", shell.PageAuthz("/api/block/list"), h.BlocksList)
	// 待重建影响面清单（只读抽屉片段）：页头徽章是入口，清单在抽屉里。
	// 鉴权复用列表页读权限点（GET 与 /api/block/list 的策略动词一致）。
	adminPages.GET("/blocks/stale/drawer", builtin.CasbinMiddlewareForPath("/api/block/list"), h.BlocksStaleDrawer)
	adminPages.POST("/blocks/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.CreateBlock)
	adminPages.POST("/blocks/delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.DeleteBlock)
	// 批量删除复用单条删除的权限点（不新增权限点、不写迁移）：能删一个块的人就能删一批。
	adminPages.POST("/blocks/bulk-delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.BlocksBulkDelete)
	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), h.SaveBlockContent)
}
