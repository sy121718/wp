package dashboardhttp

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"

	admincontract "go_wp/internal/module/admin/contract"

	"go_wp/internal/middleware/builtin"
)

// helpers.go - 后台页面共享辅助（CSRF 数据注入、权限上下文、侧边栏状态、JSON 安全串、表单取值与分页解析）。
//
// 同主题的专门文件：i18n.go（多语言注入）、pagination.go（分页组件）、page_error.go（错误出口）、
// site_url.go（详情页路径派生）；本文件只放跨域共用的零散工具。

// withCSRF 向模板数据注入当前会话的 CSRF token（layout 的 hx-headers 使用）。
// token 获取失败时置空串：Jet 的 {{ .["csrf_token"] }} 对缺 key 安全输出空值，
// 不阻塞页面渲染；已登录用户正常流程下 token 必然存在（登录时已生成）。
func withCSRF(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	// 多语言：注入 lang / t / langs / lang_redirect，并把 title（enums key）翻成当前语言。
	// 缺词条时 t 回退模板内中文原文，绝不报错（见 i18n.go）。
	data = withI18n(c, data)
	if tok, err := builtin.GetCSRFToken(c); err == nil {
		data["csrf_token"] = tok
	} else {
		data["csrf_token"] = ""
	}
	// 当前用户有效权限码集合（permContextMiddleware 注入）。
	// 模板用 {{if .PermSet["role:list"]}} 控制菜单/按钮/字段显示，
	// 与 Casbin API 鉴权同源，避免「看得到但点不了」。
	set := map[string]bool{}
	if v, ok := c.Get(permSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set
	// 侧边栏导航树（按权限过滤 + 当前页标记）。
	// 子页面（如翻译工作台）经 navPathFor 归到所属菜单项，避免整组失去高亮。
	navGroups := buildNav(set, navPathFor(c.Request.URL.Path))
	data["NavGroups"] = navGroups
	// 侧边栏展开态：由 cookie 决定，服务端渲染首屏即正确（无「先展开后收起」闪烁）。
	// 点击菜单导航时前端写 cookie=0，固定（pin）时写 cookie 且不再自动收起。
	open := sidebarOpen(c)
	pinned := sidebarPinned(c)
	// 当前页不属于任何目录分组（如「仪表盘」这类直接链接页）且未固定时默认收起，
	// 否则二级栏无 is-active 分组会整块空白。
	if open && !pinned && !hasActiveDirGroup(navGroups) {
		open = false
	}
	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned
	// 二级栏渲染开关：仅当当前页属于某个目录分组时才渲染二级栏 DOM。
	// 仪表盘这类直接链接页不渲染（无空栏占位）；此时点一级目录图标由 admin.js
	// 跳转到该组第一个页面（data-first-url），目标页正常渲染二级栏。
	data["HasSubnav"] = hasActiveDirGroup(navGroups)
	return data
}

// sidebarCookieOpen / sidebarCookiePinned 侧边栏状态 cookie 名。
const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

// hasActiveDirGroup 判断导航树中是否存在「当前页所在的目录分组」（有二级内容的组）。
// 仪表盘这类直接链接页返回 false。
func hasActiveDirGroup(groups []navGroup) bool {
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

// permSetKey 请求上下文中的权限码集合键。
const permSetKey = "perm_set"

// permContextMiddleware 把当前用户有效权限码集合写入请求上下文（仅 dashboard 页面路由挂载）。
//
// API 鉴权由 Casbin 中间件独立负责，两者共用同一权限来源；
// 权限码查询失败时降级为空集合（页面照常渲染，仅不显示需权限的元素）。
func permContextMiddleware(authz admincontract.AuthzContextService) gin.HandlerFunc {
	return func(c *gin.Context) {
		set := map[string]bool{}
		if authz != nil {
			if v, ok := c.Get("user_id"); ok {
				if uid, ok := v.(int64); ok && uid > 0 {
					codes, aerr := authz.EffectivePermissionCodes(c.Request.Context(), uint64(uid))
					if aerr != nil {
						// 失败即降级为空权限集：本页所有需要权限的按钮与菜单都会消失，
						// 功能上等同于只读。用户看到的是「按钮不见了」，必须留痕才能定位。
						logger.Scene("dashboard").With("userId", uid).
							Error(aerr, "权限上下文查询失败，本页按空权限集渲染")
					}
					for _, code := range codes {
						set[code] = true
					}
				}
			}
		}
		c.Set(permSetKey, set)
		c.Next()
	}
}

// jsonSafe 转义 JSON 字符串中的 script 闭合序列，防止用户可编辑的 Page Document
// 注入 </script> 提前闭合 <script type="application/json"> 数据岛造成存储型 XSS。
// JSON 中 \/ 是合法转义（JSON.parse 会还原为 /，不影响前端解析），
// 仅处理 </ 与 <!-- 两个闭合点：其余 < 在 JSON 字符串内合法且不会闭合 script。
func jsonSafe(s string) string {
	s = strings.ReplaceAll(s, "</", `<\/`)
	s = strings.ReplaceAll(s, "<!--", `<\!--`)
	return s
}

// fieldValue 取 PostForm 值并去掉首尾空白；无值返回空串。
func fieldValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// parseUint 解析非负整数；空串或非法返回 0。
func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

// parseStatus 解析状态；空串返回 0（多数域 0=禁用，service 默认启用由各 create 处理）。
func parseStatus(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

// parseStatusPtr 解析状态为 *int（RoleCreate 用 nil 表示未传即启用）。
func parseStatusPtr(s string) *int {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	v, _ := strconv.Atoi(t)
	return &v
}

// adminWriteFailed 统一的页面写操作失败响应：直接透出 service 返回的模块枚举错误消息，
// 不经由内部细节；必填缺失返回 BadRequest，其余按 422 处理。
func adminWriteFailed(c *gin.Context, err error) {
	if err == nil {
		return
	}
	logger.Scene("admin-page").With("path", c.Request.URL.Path).Error(err, "管理页写操作失败")
	pageErrorBadRequest(c, "admin", err)
}

// --- 管理员 administrators ---

// filterBaseURL 拼出「路径 + 非空筛选参数」作为分页链接前缀，翻页时保留筛选条件。
func filterBaseURL(path string, filters map[string]string) string {
	q := url.Values{}
	for k, v := range filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// pageParams 读取分页查询参数（?page=&limit=），缺省第 1 页、每页 20 条。
// limit 上限 100（与各模块 GetLimit() 的上限一致）。
func pageParams(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page"))
	limit, _ = strconv.Atoi(c.Query("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
