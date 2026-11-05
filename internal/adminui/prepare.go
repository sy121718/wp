package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/text"
	viewpkg "go_wp/pkg/view"
	jetpkg "go_wp/pkg/view/jet"
)

const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

func CSRF(c *gin.Context) string {
	tok, err := builtin.EnsureCSRFToken(c)
	if err != nil {
		return ""
	}
	return tok
}

func Prepare(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}

	data = text.Inject(c, data)

	data["csrf_token"] = CSRF(c)

	set := map[string]bool{}
	if v, ok := c.Get(adminctx.PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set

	buttons := map[string]bool{}
	if v, ok := c.Get(adminctx.ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			buttons = m
		}
	}
	data["Buttons"] = buttons

	// 只交原始菜单树与当前路径；激活/展开/首链接由公共模板函数计算。
	navItems := NavItemsOf(navTreeOf(c))
	data["NavItems"] = navItems
	data["NavCurrentPath"] = c.Request.URL.Path

	open := sidebarOpen(c)
	pinned := sidebarPinned(c)

	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned

	// HasSubnav 只表达「有没有可展开的目录」，不参与激活判断。
	data["HasSubnav"] = hasNavChildren(navItems)

	return data
}

func PrepareView(c *gin.Context, view any) gin.H {
	data := Prepare(c, ViewMap(view))
	injectPageList(data, view)
	return data
}

func injectPageList(data gin.H, view any) {
	viewpkg.AttachPagination(data, view)
}

func ViewMap(view any) gin.H {
	return gin.H(viewpkg.MapOf(view))
}

func PrepareBare(c *gin.Context, data gin.H) gin.H {
	return text.Inject(c, data)
}

func translateFor(c *gin.Context) func(key, fallback string) string {
	return text.TranslateFunc(c)
}

func navTreeOf(c *gin.Context) []admindto.MenuTreeNode {
	if v, ok := c.Get(adminctx.NavTreeKey); ok {
		if tree, ok := v.([]admindto.MenuTreeNode); ok {
			return tree
		}
	}
	return nil
}

func requestURIOf(c *gin.Context) string {
	return text.RequestURI(c)
}

func hasNavChildren(items []jetpkg.NavItem) bool {
	for _, item := range items {
		if len(item.Children) > 0 {
			return true
		}
	}
	return false
}

func sidebarOpen(c *gin.Context) bool {
	v, err := c.Cookie(sidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

func sidebarPinned(c *gin.Context) bool {
	v, _ := c.Cookie(sidebarCookiePinned)
	return v == "1"
}
