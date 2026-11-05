package permission

import (
	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"
)

// RouteGroup 是受保护操作组：注册即登记 METHOD + 实际路径 + 权限标识。
type RouteGroup struct {
	*mw.RouteGroup
}

// NewRouteGroup 由应用装配方指定本组用途；子组继承。
func NewRouteGroup(rg *gin.RouterGroup) *RouteGroup {
	return &RouteGroup{RouteGroup: mw.NewRouteGroup(rg, mw.RouteProtected)}
}

func (g *RouteGroup) Group(relativePath string, handlers ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{RouteGroup: g.RouteGroup.Group(relativePath, handlers...)}
}

func (g *RouteGroup) Use(middleware ...gin.HandlerFunc) *RouteGroup {
	g.RouteGroup.Use(middleware...)
	return g
}

func (g *RouteGroup) GET(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.RouteGroup.GET(relativePath, p, handlers...)
}

func (g *RouteGroup) POST(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.RouteGroup.POST(relativePath, p, handlers...)
}
