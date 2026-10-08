// Package shell 承载后台页面的公共外壳：多语言、CSRF、权限上下文、侧栏导航、分页、
// 错误出口与表单工具。
//
// 为什么要有这个包：这些是「每个后台页面都必须有、但不属于任何业务域」的东西。
// 各业务模块的页面 handler 调 shell.Prepare 就能拿到同一套壳（与页面回迁前逐字节一致）
// （同一套侧栏、同一套权限过滤、同一套文案回退），不必各自抄一份 —— 抄漏一处就会长出
// 一个没有侧栏的孤儿页。
//
// 数据来源：侧栏菜单的真源是 sys_menus 表。PermContextMiddleware 取到当前用户的
// 有效权限码后，由 admin 模块的 BuildAuthorizedTree 过滤成树放进上下文；
// Prepare 只负责把它转成渲染视图并标记当前页。
package shell

import (
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 请求上下文键。
const (
	// PermSetKey 权限码集合（map[string]bool），供模板做按钮 / 字段级可见性判断。
	PermSetKey = "perm_set"
	// NavTreeKey 授权导航树（[]admindto.MenuTreeNode），侧栏数据源。
	NavTreeKey = "nav_tree"
	// ButtonsKey 当前用户有权触发的**按钮码**集合（map[string]bool），模板据此判断
	// 按钮 / 字段级显隐。按钮码是 type=3 节点的 title_key（迁移 589）——
	// 模板层因此只出现菜单体系的词汇，不出现权限码（docs/02-Z §4.4）。
	ButtonsKey = "button_set"
)

// 侧栏状态 cookie 名。
const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

// Prepare 为页面模板数据补上外壳所需的全部键，所有后台页面渲染入口统一调用。
//
// 注入内容：lang / t / langs / lang_redirect（多语言，标题为 i18n key 时一并翻译）、
// csrf_token（layout 的 hx-headers）、PermSet（当前用户有效权限码）、
// Buttons（当前用户有权触发的按钮码 —— 模板判断按钮显隐用它，不写权限码）、
// NavGroups（侧栏导航树，已按权限过滤）、SidebarOpen / SidebarPinned / HasSubnav。
//
// 兜底：token 取不到时置空串（Jet 的 {{ .["csrf_token"] }} 对空值安全，提交会被 CSRF
// 中间件拒，与其它后台页一致）；i18n 未初始化或词条缺失时 t 返回模板内中文原文，绝不报错。
func Prepare(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}

	data = injectI18n(c, data)

	if tok, err := builtin.GetCSRFToken(c); err == nil {
		data["csrf_token"] = tok
	} else {
		data["csrf_token"] = ""
	}

	set := map[string]bool{}
	if v, ok := c.Get(PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set

	// 按钮码集合：与 PermSet 同源（都由 PermContextMiddleware 按用户有效权限码算出），
	// 但键是菜单侧的按钮码 —— 模板写 {{if isset(.Buttons["product.create"])}}。
	buttons := map[string]bool{}
	if v, ok := c.Get(ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			buttons = m
		}
	}
	data["Buttons"] = buttons

	// 侧栏导航树：数据是 sys_menus（中间件已按权限过滤），这里只标记当前页与展开态。
	navGroups := BuildNav(NavTree(c), c.Request.URL.Path, TranslateFor(c))
	data["NavGroups"] = navGroups

	// 侧边栏展开态由 cookie 决定，服务端渲染首屏即正确（无「先展开后收起」闪烁）。
	open := sidebarOpen(c)
	pinned := sidebarPinned(c)
	// 当前页不属于任何目录分组（如仪表盘这类直接链接页）且未固定时默认收起，
	// 否则二级栏无 is-active 分组会整块空白。
	if open && !pinned && !hasActiveDirGroup(navGroups) {
		open = false
	}
	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned
	// 二级栏渲染开关：仅当当前页属于某个目录分组时才渲染二级栏 DOM。
	data["HasSubnav"] = hasActiveDirGroup(navGroups)

	return data
}

// PermContextMiddleware 把当前用户的有效权限码集合与授权导航树写入请求上下文。
//
// 仅后台页面路由挂载。API 鉴权由 Casbin 中间件独立负责，两者共用同一权限来源
// （EffectivePermissionCodes），保证「API 放行的权限」与「页面/按钮显示的权限」同源。
//
// 失败一律降级：查询失败按空权限集渲染（页面照常显示，只是需要权限的按钮与菜单消失），
// 菜单树构建失败则侧栏为空 —— 都不影响页面内容与鉴权判定，但必须留痕才能定位。
func PermContextMiddleware(authz admincontract.AuthzContextService) gin.HandlerFunc {
	return func(c *gin.Context) {
		set := map[string]bool{}
		buttons := map[string]bool{}
		if authz != nil {
			if v, ok := c.Get("user_id"); ok {
				if uid, ok := v.(int64); ok && uid > 0 {
					codes, aerr := authz.EffectivePermissionCodes(c.Request.Context(), uint64(uid))
					if aerr != nil {
						logger.Scene("dashboard").With("userId", uid).
							Error(aerr, "权限上下文查询失败，本页按空权限集渲染")
					}
					for _, code := range codes {
						set[code] = true
					}
					// 按钮码：由 type=3 节点按同一批有效权限码算出（同源，见契约注释）。
					if list, berr := authz.AuthorizedButtonCodes(c.Request.Context(), codes); berr != nil {
						logger.Scene("dashboard").With("userId", uid).
							Error(berr, "按钮码查询失败，本页按钮按无权限渲染")
					} else {
						for _, b := range list {
							buttons[b] = true
						}
					}
					// 侧栏导航树与上面那批权限码同源：菜单真源是 sys_menus 表，
					// admin 模块按权限码过滤后返回（自动补祖先目录、排除按钮与外链）。
					tree, terr := authz.BuildAuthorizedTree(c.Request.Context(), codes)
					if terr != nil {
						logger.Scene("dashboard").With("userId", uid).
							Error(terr, "导航菜单树构建失败，本页侧栏为空")
					} else {
						c.Set(NavTreeKey, tree)
					}
				}
			}
		}
		c.Set(PermSetKey, set)
		c.Set(ButtonsKey, buttons)
		c.Next()
	}
}

// NavTree 取本请求的授权菜单树；未注入（未登录 / 构建失败 / 非页面路由）时返回 nil。
func NavTree(c *gin.Context) []admindto.MenuTreeNode {
	if v, ok := c.Get(NavTreeKey); ok {
		if tree, ok := v.([]admindto.MenuTreeNode); ok {
			return tree
		}
	}
	return nil
}

// injectI18n 注入多语言键（lang / t / langs / lang_redirect，标题是 i18n key 时翻译）。
//
// t 的签名是 func(key, fallback string) string，模板用 {{ .["t"]("shell.brand", "管理后台") }}。
// 实测取翻译路径的取舍见 internal/templates/i18n_jet_test.go。
func injectI18n(c *gin.Context, data gin.H) gin.H {
	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t
	data["langs"] = templates.LanguageOptions(lang)
	// 语言切换表单的 redirect 隐藏域：当前页 URI 经 LangRedirect 收敛
	//（只允许站内相对路径、最长 langRedirectMaxBytes 字节），详见 notice.go。
	data["lang_redirect"] = LangRedirect(c)

	// 页面标题：enums 常量已是 key（如 MsgPagesTitle），此处按当前语言翻译；
	// 非 key 的字面量标题（如登录页）走 fallback 原样返回，不改变行为。
	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}
	return data
}

// TranslateFor 返回 Go 侧文案翻译函数（分页条等由 Go 拼接、不经模板的文案）。
// c 为 nil 时按默认语言返回，绝不 panic。
func TranslateFor(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

// adminHomePath 控制面首页（仪表盘）。
//
// 曾经回落 "/"，但 **/ 现在归前台首页**（站点独占域名根）—— 后台的
// 「回到首页」类回落若还指向 "/"，操作者会被丢到店铺首页，
// 看起来像登录态丢了。菜单树里「仪表盘」的 path 本来就是 /admin，两边一致。
const adminHomePath = "/admin"

// requestURI 返回当前请求的站内 URI（含 query）；异常时回控制面首页。
func requestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return adminHomePath
	}
	return uri
}

// hasActiveDirGroup 判断导航树中是否存在「当前页所在的目录分组」（有二级内容的组）。
// 仪表盘这类直接链接页返回 false。
func hasActiveDirGroup(groups []NavGroup) bool {
	for _, g := range groups {
		if g.Active && len(g.Nodes) > 0 {
			return true
		}
	}
	return false
}

// sidebarOpen 侧边栏是否展开：cookie 缺省为展开（首次访问体验）。
func sidebarOpen(c *gin.Context) bool {
	v, err := c.Cookie(sidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

// sidebarPinned 侧边栏是否固定（固定后导航不再自动收起）。
func sidebarPinned(c *gin.Context) bool {
	v, _ := c.Cookie(sidebarCookiePinned)
	return v == "1"
}
