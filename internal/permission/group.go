package permission

import (
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
)

type RouteGroup struct {
	*gin.RouterGroup
}

func NewRouteGroup(rg *gin.RouterGroup) *RouteGroup {
	if rg == nil {
		panic("permission.NewRouteGroup 收到 nil 路由组（装配缺陷）")
	}
	return &RouteGroup{RouterGroup: rg}
}

func (g *RouteGroup) Group(relativePath string, handlers ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{RouterGroup: g.RouterGroup.Group(relativePath, handlers...)}
}

func (g *RouteGroup) Use(middleware ...gin.HandlerFunc) *RouteGroup {
	g.RouterGroup.Use(middleware...)
	return g
}

func (g *RouteGroup) GET(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodGet, relativePath, p, handlers...)
}

func (g *RouteGroup) POST(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodPost, relativePath, p, handlers...)
}

func (g *RouteGroup) declareAndRegister(method, relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	Declare(method, joinPaths(g.BasePath(), relativePath), p)
	if method == http.MethodGet {
		return g.RouterGroup.GET(relativePath, handlers...)
	}
	return g.RouterGroup.POST(relativePath, handlers...)
}

func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if lastChar(relativePath) == '/' && lastChar(finalPath) != '/' {
		return finalPath + "/"
	}
	return finalPath
}

func lastChar(str string) uint8 {
	if str == "" {
		return 0
	}
	return str[len(str)-1]
}
