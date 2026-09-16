// group.go — 声明式权限路由组。
//
// RouteGroup 包装 *gin.RouterGroup，把「注册路由」与「声明该路由所需权限点」合成
// **同一个动作**：路径由 group 的 BasePath 与相对路径算出（和 gin 实际注册用的是同一套
// 规则），所以声明出来的路径与运行时请求路径永远一致 —— 这正是审计 SEC-011 要的
// 「路由与权限点天然一致」。
//
// 方法遮蔽说明：本类型内嵌 *gin.RouterGroup 并**重定义了 GET / POST / Group / Use**。
// Go 的方法解析优先取外层定义，所以模块里写 g.POST("/x", perm.ProductCreate, h) 走的是
// 声明式注册；而 g.PUT(...) 之类本仓禁用的动词仍会落到内嵌 gin 方法上（不涉及权限点，
// 路由约定由 AGENTS.md「只用 GET 和 POST」管）。
package permission

import (
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
)

// RouteGroup 声明式权限路由组。
type RouteGroup struct {
	*gin.RouterGroup
}

// NewRouteGroup 包装一个已装配好中间件的 gin 路由组。
//
// nil 直接 panic：整个授权面的入口包装成 nil 意味着所有路由都失去权限声明，
// 这是装配缺陷，不能靠后面某个请求 403 才发现。
func NewRouteGroup(rg *gin.RouterGroup) *RouteGroup {
	if rg == nil {
		panic("permission.NewRouteGroup 收到 nil 路由组（装配缺陷）")
	}
	return &RouteGroup{RouterGroup: rg}
}

// Group 派生子组，保留权限声明能力。
func (g *RouteGroup) Group(relativePath string, handlers ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{RouterGroup: g.RouterGroup.Group(relativePath, handlers...)}
}

// Use 追加中间件，返回包装类型（避免链式调用后丢掉声明能力）。
func (g *RouteGroup) Use(middleware ...gin.HandlerFunc) *RouteGroup {
	g.RouterGroup.Use(middleware...)
	return g
}

// GET 注册 GET 路由并声明所需权限点（p 为 Exempt 表示显式豁免）。
func (g *RouteGroup) GET(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodGet, relativePath, p, handlers...)
}

// POST 注册 POST 路由并声明所需权限点（p 为 Exempt 表示显式豁免）。
func (g *RouteGroup) POST(relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodPost, relativePath, p, handlers...)
}

// declareAndRegister 先登记权限点再注册路由。
//
// 顺序是刻意的：Declare 里的校验（权限点未登记 / 重复声明）先于 gin 的路由冲突检查执行，
// 装配报错时先看到的是「权限点声明的问题」而不是 gin 的 panic 文案。
func (g *RouteGroup) declareAndRegister(method, relativePath string, p Perm, handlers ...gin.HandlerFunc) gin.IRoutes {
	Declare(method, joinPaths(g.BasePath(), relativePath), p)
	if method == http.MethodGet {
		return g.RouterGroup.GET(relativePath, handlers...)
	}
	return g.RouterGroup.POST(relativePath, handlers...)
}

// joinPaths 与 gin 内部实现同语义：拼接 base 与相对路径，并保留相对路径的尾斜杠。
// （不接受基础库的私有函数，所以在这里保持一份等价实现；差异会表现为声明路径
//
//	与 gin 实际注册路径不一致，routes_snapshot_test 与 check-permission-gaps.sh
//	会立刻抓到，不会静默。）
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

// lastChar 返回字符串最后一个字节（空串返回 0）。
func lastChar(str string) uint8 {
	if str == "" {
		return 0
	}
	return str[len(str)-1]
}
