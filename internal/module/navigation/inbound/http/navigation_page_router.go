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

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
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
	// 面板块能力（超级菜单）：装配期注入；未注入时**编辑抽屉**里的面板区整体降级
	//（NavEditPanelAvail=false，入口隐藏 —— 不留一个点了没反应的下拉）。
	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations", adminui.PageAuthz("/api/navigation/list"), h.NavigationsPage)
	// GET 读编辑表单，但代理真正 POST /api/navigation/update 的权限动作。
	adminPages.GET("/navigations/edit", builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost), h.NavigationEditFragment)
	// 面板设置复用 navigation:update；新建面板块是**块的创建**，故挂 block:create。
	adminPages.POST("/navigations/panel", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.PanelSet)
	adminPages.POST("/navigations/panel/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.PanelCreate)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)
	// 同级上移 / 下移已迁到真实 JSON 端点（POST /api/navigation/update，action="move"，
	// 见 navigation_move_write.go）：页面这一侧不再保留它的 POST。

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
	// 译文保存的写入口在真实 JSON 端点 POST /api/navigation/update（action="translations"）：
	// 它由 SetupNavigationRoutes 注册，拿不到这里的 writer / marker / invalidator 端口，
	// 所以把带端口的句柄交给包级写入口（未注入时该分支 fail closed，见
	// navigation_translation_write.go）。
	SetNavigationTranslationWriter(translations)
	adminPages.GET("/navigations/translations", adminui.PageAuthz("/api/navigation/list"), translations.NavigationTranslations)
}
