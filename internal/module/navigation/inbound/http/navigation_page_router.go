package navigationhttp

// navigation_page_router.go — 导航菜单后台页面的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 navigation_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 与后台权限菜单（admin/menus）严格隔离：本页管理的是公开站点导航（navigations 表）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/navigation/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// SetupNavigationPages 注册导航菜单管理页与导航译文工作台（/admin 组，
// 中间件链由装配层统一挂好）。函数名沿用 SetupXxxPages 先例：本包已有 REST
// 路由的 SetupNavigationRoutes，不能同名。
// pageSvc 是页面契约：译文保存后按 i18n:content 依赖标记手工页面待重建
// （消费者侧断言 navTranslationPageMarker 端口，实现方不支持时降级为不标记）。
// adminPages 为 nil 时整体跳过。
func SetupNavigationPages(adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, pageSvc pagecontract.PageService,
	blocks blockcontract.BlockService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)
	// 面板块能力（超级菜单）：装配期注入；未注入时面板入口降级可见（PanelAvail=false）。
	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations", h.NavigationsPage)
	// GET 读编辑表单，但代理真正 POST /api/navigation/update 的权限动作。
	adminPages.GET("/navigations/edit", builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost), h.NavigationEditFragment)
	// 面板设置复用 navigation:update；新建面板块是**块的创建**，故挂 block:create。
	adminPages.POST("/navigations/panel", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.PanelSet)
	adminPages.POST("/navigations/panel/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.PanelCreate)
	adminPages.POST("/navigations/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationCreate)
	adminPages.POST("/navigations/add-source", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationAddSource)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)
	adminPages.POST("/navigations/delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), h.NavigationDelete)
	// 批量删除复用同一条删除路径与权限点（/api/navigation/delete）：批量只是单条的加速器，
	// 不是另一件事 —— 另立权限点会让「能删一个、不能删十个」这种状态出现。
	adminPages.POST("/navigations/bulk-delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), h.NavigationsBulkDelete)
	adminPages.POST("/navigations/move", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationMove)

	// 导航译文工作台（审计 I18N-007）：菜单标签不在页面文档里，页面翻译工作台看不到它，
	// 构建期靠 navigation.label 语境回填 —— 本页是那个语境的唯一维护入口。
	// 保存复用「修改导航」权限点：译文是导航项内容的一部分。
	translations := NewNavigationTranslationHandle(navigations, projects)
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}
	if pageSvc != nil {
		if marker, ok := pageSvc.(navTranslationPageMarker); ok {
			translations.SetPageMarker(marker)
		}
	}
	// 自动发布实例侧同样要标：菜单文字也烘在 presentation 实例的产物里，而那些实例
	// 只认 menu:{projectID}:{kind} 依赖（与导航项增删改走同一条派发链、同一份键）。
	// 实现方是本模块自己的 Service（持有 SetMenuStaleDispatcher 注入的端口）。
	if invalidator, ok := navigations.(navTranslationMenuInvalidator); ok {
		translations.SetMenuInvalidator(invalidator)
	}
	adminPages.GET("/navigations/translations", translations.NavigationTranslations)
	adminPages.POST("/navigations/translations/save", builtin.CasbinMiddlewareForPath("/api/navigation/update"), translations.SaveNavigationTranslations)
}
