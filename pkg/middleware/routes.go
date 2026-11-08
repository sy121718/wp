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
	frozen       bool
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
	if frozen {
		panic("路由登记已冻结：启动后不得再注册（" + spec.Method + " " + spec.Path + "）")
	}
	// 空权限只允许用于明确豁免；非豁免声明必须带权限码，否则等于静默公开。
	if spec.Perm == exemptPerm && spec.Kind != RouteExempt {
		panic("空权限只能用于明确豁免的声明：" + spec.Method + " " + spec.Path)
	}
	if spec.Perm != exemptPerm && spec.Kind == RouteExempt {
		panic("豁免声明不得携带权限点：" + spec.Method + " " + spec.Path)
	}
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

// Reset 清空登记并解冻；只允许装配与测试使用，不能在服务期间清授权。
func Reset() {
	regMu.Lock()
	defer regMu.Unlock()
	declared = nil
	declaredKeys = nil
	exemptRoutes = nil
	apiPermPaths = nil
	frozen = false
}

// Freeze 在全部路由登记完成后冻结索引：迟到的登记明确 panic，
// 不实现热加载或运行中重建。Reset 只给装配与测试重新开始用。
func Freeze() {
	regMu.Lock()
	defer regMu.Unlock()
	frozen = true
}

// Frozen 报告登记是否已冻结。
func Frozen() bool {
	regMu.Lock()
	defer regMu.Unlock()
	return frozen
}

// RouteGroup 是一个受保护操作组：注册即登记 METHOD + 实际路径 + 权限标识。
type RouteGroup struct {
	*gin.RouterGroup
	Kind RouteKind
}

// NewRouteGroup 由应用装配方指定本组用途；子组继承。
func NewRouteGroup(rg *gin.RouterGroup, kind RouteKind) *RouteGroup {
	if rg == nil {
		panic("mw.NewRouteGroup 收到 nil 路由组（装配缺陷）")
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

// DeclareRouteIn 登记一条不走本组默认登记的入口（如复用别的操作鉴权目标的页面/写入口）。
//
// 与 DeclareRoute 的区别只在调用点：它把「注册真实路由」和「登记权限绑定」分开写，
// 用于真实路径与鉴权目标不同、或该路径不经过本组 declareAndRegister 的情形。
func (g *RouteGroup) DeclareRouteIn(method, relativePath string, spec RouteSpec) {
	if spec.Path == "" {
		spec.Path = joinRoutePaths(g.BasePath(), relativePath)
	}
	if spec.Method == "" {
		spec.Method = method
	}
	if spec.Kind == "" {
		spec.Kind = g.Kind
	}
	DeclareRoute(spec)
}

// ExemptGET / ExemptPOST 在受保护组内声明明确豁免的入口：
// 仍走该组自身的认证与 CSRF，只是不进后台权限目录。
func (g *RouteGroup) ExemptGET(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareExemptAndRegister(http.MethodGet, relativePath, handlers...)
}

func (g *RouteGroup) ExemptPOST(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return g.declareExemptAndRegister(http.MethodPost, relativePath, handlers...)
}

func (g *RouteGroup) declareExemptAndRegister(method, relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes {
	Declare(method, joinRoutePaths(g.BasePath(), relativePath), "", RouteExempt)
	if method == http.MethodGet {
		return g.RouterGroup.GET(relativePath, handlers...)
	}
	return g.RouterGroup.POST(relativePath, handlers...)
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
