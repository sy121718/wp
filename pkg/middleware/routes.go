package middleware

import (
	"cmp"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// RouteKind 是路由登记的真实用途，由创建该路由组的装配方显式给定，
// 不从 URL、方法、Accept 或 HX 头推断。
type RouteKind string

const (
	// RouteProtected 是需要鉴权的受保护操作（默认鉴权对象为自身）。
	RouteProtected RouteKind = "protected"
	// RoutePage 是页面 GET，可复用别的操作的权限对象/方法。
	RoutePage RouteKind = "page"
	// RouteExempt 是明确豁免后台权限目录的入口（仍执行其自身的认证与 CSRF）。
	RouteExempt RouteKind = "exempt"
)

// RouteSpec 是一条已登记的路由绑定事实。
type RouteSpec struct {
	Method string
	Path   string
	// Perm 是权限标识；豁免入口为空串。
	Perm string
	Kind RouteKind
	// AuthObject / AuthMethod 是实际被检查的鉴权目标；
	// 默认等于自身的 Path / Method，页面或 HTML 写入口复用别的权限时显式填写。
	AuthObject string
	AuthMethod string
}

const exemptPerm = ""

var (
	regMu        sync.Mutex
	declared     []RouteSpec
	declaredKeys map[string]RouteSpec
	exemptRoutes []string
	apiPermPaths map[string]string
)

// Declare 登记一条路由绑定；同一路由重复声明不同权限会显式 panic。
func Declare(method, path, perm string, kind RouteKind) {
	regMu.Lock()
	defer regMu.Unlock()
	declareLocked(method, path, perm, kind)
}

// DeclareRoute 登记完整绑定（含复用的鉴权对象/方法）。
func DeclareRoute(spec RouteSpec) {
	if spec.AuthObject == "" {
		spec.AuthObject = spec.Path
	}
	if spec.AuthMethod == "" {
		spec.AuthMethod = spec.Method
	}
	if spec.Kind == "" {
		if spec.Perm == exemptPerm {
			spec.Kind = RouteExempt
		} else {
			spec.Kind = RouteProtected
		}
	}
	regMu.Lock()
	defer regMu.Unlock()
	final, added := declaredOrPanicLocked(spec)
	if added {
		declared = append(declared, final)
	}
}

// declaredOrPanicLocked 登记一条绑定；返回 (最终生效的绑定, 是否为新增)。
//
// 同一路由以相同权限重复声明是幂等操作（装配可能被测试重复调用），不重复追加。
func declaredOrPanicLocked(spec RouteSpec) (RouteSpec, bool) {
	if declaredKeys == nil {
		declaredKeys = map[string]RouteSpec{}
	}
	key := spec.Method + " " + spec.Path
	if prev, dup := declaredKeys[key]; dup {
		if prev.Perm == spec.Perm {
			return prev, false
		}
		if spec.Perm == exemptPerm || prev.Perm == exemptPerm {
			panic("同一路由既声明了权限点又声明了豁免：" + key)
		}
		panic("同一路由被声明了两种权限点：" + key + " → " + prev.Perm + " / " + spec.Perm)
	}
	if spec.Perm == exemptPerm {
		declaredKeys[key] = spec
		exemptRoutes = append(exemptRoutes, key)
		return spec, true
	}
	if strings.HasPrefix(spec.Path, "/api/") {
		// API 路由必须各自一个权限点：策略表按权限码唯一映射一条路径。
		if apiPermPaths == nil {
			apiPermPaths = map[string]string{}
		}
		if prev, used := apiPermPaths[spec.Perm]; used && prev != key {
			panic("同一权限点绑定到多条 API 路由：" + spec.Perm + " → " + prev + " / " + key)
		}
		apiPermPaths[spec.Perm] = key
	}
	declaredKeys[key] = spec
	return spec, true
}

func declareLocked(method, path, perm string, kind RouteKind) {
	spec := RouteSpec{Method: method, Path: path, Perm: perm, Kind: kind, AuthObject: path, AuthMethod: method}
	final, added := declaredOrPanicLocked(spec)
	if added {
		declared = append(declared, final)
	}
}

// RoutesOf 返回某权限点绑定的全部路由。
func RoutesOf(perm string) []RouteSpec {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]RouteSpec, 0, 2)
	for _, r := range declared {
		if r.Perm == perm {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b RouteSpec) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return out
}

// Snapshot 返回已登记绑定的副本（只读视图）。
func Snapshot() []RouteSpec {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]RouteSpec, len(declared))
	copy(out, declared)
	slices.SortFunc(out, func(a, b RouteSpec) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return out
}

// Exempts 返回明确豁免的路由键。
func Exempts() []string {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]string, len(exemptRoutes))
	copy(out, exemptRoutes)
	slices.Sort(out)
	return out
}

// Reset 清空登记；只允许装配与测试使用，不能在服务期间清授权。
func Reset() {
	regMu.Lock()
	defer regMu.Unlock()
	declared = nil
	declaredKeys = nil
	exemptRoutes = nil
	apiPermPaths = nil
}

// RouteGroup 是一个受保护操作组：注册即登记 METHOD + 实际路径 + 权限标识。
type RouteGroup struct {
	*gin.RouterGroup
	Kind RouteKind
}

// NewRouteGroup 由应用装配方指定本组用途；子组继承。
func NewRouteGroup(rg *gin.RouterGroup, kind RouteKind) *RouteGroup {
	if rg == nil {
		panic("middleware.NewRouteGroup 收到 nil 路由组（装配缺陷）")
	}
	return &RouteGroup{RouterGroup: rg, Kind: kind}
}

func (g *RouteGroup) Group(relativePath string, handlers ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{RouterGroup: g.RouterGroup.Group(relativePath, handlers...), Kind: g.Kind}
}

func (g *RouteGroup) Use(middleware ...gin.HandlerFunc) *RouteGroup {
	g.RouterGroup.Use(middleware...)
	return g
}

func (g *RouteGroup) GET(relativePath, perm string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodGet, relativePath, perm, handlers...)
}

func (g *RouteGroup) POST(relativePath, perm string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareAndRegister(http.MethodPost, relativePath, perm, handlers...)
}

func (g *RouteGroup) declareAndRegister(method, relativePath, perm string, handlers ...gin.HandlerFunc) gin.IRoutes {
	Declare(method, joinRoutePaths(g.BasePath(), relativePath), perm, g.Kind)
	if method == http.MethodGet {
		return g.RouterGroup.GET(relativePath, handlers...)
	}
	return g.RouterGroup.POST(relativePath, handlers...)
}

func joinRoutePaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if lastRouteChar(relativePath) == '/' && lastRouteChar(finalPath) != '/' {
		return finalPath + "/"
	}
	return finalPath
}

func lastRouteChar(s string) uint8 {
	if s == "" {
		return 0
	}
	return s[len(s)-1]
}
