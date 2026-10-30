package navigationhttp

// navigation_page_router.go — 导航菜单后台页面的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 navigation_page_handle.go。路由注册只出现在 *_router.go，门禁
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

// NavigationEditFormTemplate 导航编辑抽屉片段（片段出口，不带页壳）。
const NavigationEditFormTemplate = "admin/navigation/navigation_edit_form"

// NavigationsPageTemplate 导航菜单页的模板名。
const NavigationsPageTemplate = "admin/navigation/navigations"

// NavigationTranslationsTemplate 导航译文页的模板名。
const NavigationTranslationsTemplate = "admin/navigation/navigation_translations.html"

// SetupNavigationPages 注册导航菜单管理页与导航译文工作台（/admin 组，
// 中间件链由装配层统一挂好）。函数名沿用 SetupXxxPages 先例：本包已有 REST
// 路由的 SetupNavigationRoutes，不能同名。
// pageSvc 保留在签名里但**已不参与任何接线**：译文保存的失效编排（标页面 / 标实例 /
// 入队）整体下沉到 service 的 TranslationWriter，由装配层把三个跨模块端口直接交给它，
// 页面句柄不再转手这些端口。形参留在原位是为了本批不同时改调用点（装配 + 两个测试），
// 下线动作随 TEST owner 的迁移一波做。
// adminPages 为 nil 时整体跳过。
func SetupNavigationPages(adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, _ pagecontract.PageService,
	blocks blockcontract.BlockService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)
	// 面板块能力（超级菜单）：装配期注入；未注入时**编辑抽屉**里的面板区整体降级
	//（NavEditPanelAvail=false，入口隐藏 —— 不留一个点了没反应的下拉）。
	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations",
		adminui.PageAuthz("/api/navigation/list"),
		adminui.PageSlot(NavigationsPageTemplate),
		h.NavigationsPage)
	// GET 读编辑表单，但代理真正 POST /api/navigation/update 的权限动作。
	adminPages.GET("/navigations/edit",
		builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost),
		adminui.BareSlot(NavigationEditFormTemplate),
		h.NavigationEditFragment)
	// 面板设置复用 navigation:update；新建面板块是**块的创建**，故挂 block:create。
	adminPages.POST("/navigations/panel", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.PanelSet)
	adminPages.POST("/navigations/panel/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.PanelCreate)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)
	// 同级上移 / 下移已迁到真实 JSON 端点（POST /api/navigation/update，action="move"，
	// 见 navigation_move_handle.go）：页面这一侧不再保留它的 POST。

	// 导航译文工作台（审计 I18N-007）：菜单标签不在页面文档里，页面翻译工作台看不到它，
	// 构建期靠 navigation.label 语境回填 —— 本页是那个语境的唯一维护入口。
	// 保存复用「修改导航」权限点：译文是导航项内容的一部分。
	translations := NewNavigationTranslationHandle(navigations, projects)
	// 读依赖：工作台渲染要按 hash 取当前译文现值（LoadTargetsForProject），
	// 这是**读**端口，与写路径无关，保留。取不到就退化成只显示原文（可见降级）。
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}
	// 译文保存的写入口在真实 JSON 端点 POST /api/navigation/update（action="translations"，
	// 见 navigation_handle.go）：它由 SetupNavigationRoutes 注册，那条链上的业务全在
	// service 的 TranslationWriter 里（三个跨模块同事务端口由装配层注入）。
	// 本页的句柄只服务**读**（GET 工作台 + build 取现值），不再充当写入口。
	adminPages.GET("/navigations/translations",
		adminui.PageAuthz("/api/navigation/list"),
		adminui.PageSlot(NavigationTranslationsTemplate),
		translations.NavigationTranslations)
}
