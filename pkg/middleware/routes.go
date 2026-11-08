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

// Registry 是一次装配实例的路由权限登记索引。
//
// 为什么是实例而不是包级全局：登记只在启动期完成、完成后冻结，但同一进程里的
// 测试装配与生产装配会各自建立索引。全局态会让「测试 Reset」清掉正在服务实例的
// 索引，也无法证明多 router 实例互不污染。这里只持有本次装配的事实，
// 查询返回副本，调用方不能改内部状态。
type Registry struct {
	mu           sync.Mutex
	declared     []RouteSpec
	declaredKeys map[string]RouteSpec
	exemptRoutes []string
	apiPermPaths map[string]string
	frozen       bool
}

// NewRegistry 建立一个空登记索引；每次路由装配建一个，不共享。
func NewRegistry() *Registry { return &Registry{} }

// Declare 登记一条路由绑定；同一路由重复声明不同权限会显式 panic。
func (r *Registry) Declare(method, path, perm string, kind RouteKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.declareLocked(RouteSpec{Method: method, Path: path, Perm: perm, Kind: kind, AuthObject: path, AuthMethod: method})
}

// DeclareRoute 登记完整绑定（含复用的鉴权对象/方法）。
func (r *Registry) DeclareRoute(spec RouteSpec) {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	final, added := r.declaredOrPanicLocked(spec)
	if added {
		r.declared = append(r.declared, final)
	}
}

// declareLocked 是 Declare 的内部实现；调用方已持锁。
func (r *Registry) declareLocked(spec RouteSpec) {
	final, added := r.declaredOrPanicLocked(spec)
	if added {
		r.declared = append(r.declared, final)
	}
}

// declaredOrPanicLocked 登记一条绑定；返回 (最终生效的绑定, 是否为新增)。
//
// 同一路由以相同权限重复声明是幂等操作（装配可能被测试重复调用），不重复追加。
func (r *Registry) declaredOrPanicLocked(spec RouteSpec) (RouteSpec, bool) {
	if r.frozen {
		panic("路由登记已冻结：启动后不得再注册（" + spec.Method + " " + spec.Path + "）")
	}
	// 空权限只允许用于明确豁免；非豁免声明必须带权限码，否则等于静默公开。
	if spec.Perm == exemptPerm && spec.Kind != RouteExempt {
		panic("空权限只能用于明确豁免的声明：" + spec.Method + " " + spec.Path)
	}
	if spec.Perm != exemptPerm && spec.Kind == RouteExempt {
		panic("豁免声明不得携带权限点：" + spec.Method + " " + spec.Path)
	}
	if r.declaredKeys == nil {
		r.declaredKeys = map[string]RouteSpec{}
	}
	key := spec.Method + " " + spec.Path
	if prev, dup := r.declaredKeys[key]; dup {
		if prev.Perm == spec.Perm {
			return prev, false
		}
		if spec.Perm == exemptPerm || prev.Perm == exemptPerm {
			panic("同一路由既声明了权限点又声明了豁免：" + key)
		}
		panic("同一路由被声明了两种权限点：" + key + " → " + prev.Perm + " / " + spec.Perm)
	}
	if spec.Perm == exemptPerm {
		r.declaredKeys[key] = spec
		r.exemptRoutes = append(r.exemptRoutes, key)
		return spec, true
	}
	if strings.HasPrefix(spec.Path, "/api/") {
		// API 路由必须各自一个权限点：策略表按权限码唯一映射一条路径。
		if r.apiPermPaths == nil {
			r.apiPermPaths = map[string]string{}
		}
		if prev, used := r.apiPermPaths[spec.Perm]; used && prev != key {
			panic("同一权限点绑定到多条 API 路由：" + spec.Perm + " → " + prev + " / " + key)
		}
		r.apiPermPaths[spec.Perm] = key
	}
	r.declaredKeys[key] = spec
	return spec, true
}

// RoutesOf 返回某权限点绑定的全部路由。
func (r *Registry) RoutesOf(perm string) []RouteSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RouteSpec, 0, 2)
	for _, spec := range r.declared {
		if spec.Perm == perm {
			out = append(out, spec)
		}
	}
	slices.SortFunc(out, func(a, b RouteSpec) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return out
}

// Snapshot 返回已登记绑定的副本（只读视图）。
func (r *Registry) Snapshot() []RouteSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RouteSpec, len(r.declared))
	copy(out, r.declared)
	slices.SortFunc(out, func(a, b RouteSpec) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return out
}

// Exempts 返回明确豁免的路由键。
func (r *Registry) Exempts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.exemptRoutes))
	copy(out, r.exemptRoutes)
	slices.Sort(out)
	return out
}

// Reset 清空**本实例**的登记并解冻。
//
// 它只给「重新装配的测试实例与脚手架」用：应用装配持有自己的 Registry，
// 对默认实例或另一个实例调用 Reset，不会清掉正在服务实例的索引。
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.declared = nil
	r.declaredKeys = nil
	r.exemptRoutes = nil
	r.apiPermPaths = nil
	r.frozen = false
}

// Freeze 在全部路由登记完成后冻结索引：迟到的登记明确 panic，
// 不实现热加载或运行中重建。Reset 只给装配与测试重新开始用。
func (r *Registry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Frozen 报告登记是否已冻结。
func (r *Registry) Frozen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frozen
}

// defaultRegistry 是「没有装配索引的模块单测与脚手架」使用的默认实例。
//
// 应用装配不走向它：每次 SetupRoutes 建一个 Registry 并随 RouteGroup 下发，
// 因此默认实例上的 Reset 不会影响正在服务的实例。
var defaultRegistry = NewRegistry()

// 包级入口转发到默认实例，保留模块单测与脚手架的既有调用形状。
func Declare(method, path, perm string, kind RouteKind) {
	defaultRegistry.Declare(method, path, perm, kind)
}

func DeclareRoute(spec RouteSpec) { defaultRegistry.DeclareRoute(spec) }

func RoutesOf(perm string) []RouteSpec { return defaultRegistry.RoutesOf(perm) }

func Snapshot() []RouteSpec { return defaultRegistry.Snapshot() }

func Exempts() []string { return defaultRegistry.Exempts() }

func Reset() { defaultRegistry.Reset() }

func Freeze() { defaultRegistry.Freeze() }

func Frozen() bool { return defaultRegistry.Frozen() }

// RouteGroup 是一个受保护操作组：注册即登记 METHOD + 实际路径 + 权限标识。
//
// 组持有本次装配的 Registry；子组继承同一个索引，不产生第二个权限服务。
type RouteGroup struct {
	*gin.RouterGroup
	Kind RouteKind
	reg  *Registry
}

// NewRouteGroup 由应用装配方指定本组用途；子组继承。
//
// 没有装配索引时（模块单测、脚手架）用默认实例；应用装配请用 NewRouteGroupIn。
func NewRouteGroup(rg *gin.RouterGroup, kind RouteKind) *RouteGroup {
	return NewRouteGroupIn(defaultRegistry, rg, kind)
}

// NewRouteGroupIn 用本次装配持有的索引建立路由组。
func NewRouteGroupIn(reg *Registry, rg *gin.RouterGroup, kind RouteKind) *RouteGroup {
	if rg == nil {
		panic("mw.NewRouteGroup 收到 nil 路由组（装配缺陷）")
	}
	if reg == nil {
		panic("mw.NewRouteGroupIn 收到 nil 登记索引（装配缺陷）")
	}
	return &RouteGroup{RouterGroup: rg, Kind: kind, reg: reg}
}

// Registry 返回本组登记的索引；装配层用它取快照、冻结与核对。
func (g *RouteGroup) Registry() *Registry { return g.reg }

func (g *RouteGroup) Group(relativePath string, handlers ...gin.HandlerFunc) *RouteGroup {
	return &RouteGroup{RouterGroup: g.RouterGroup.Group(relativePath, handlers...), Kind: g.Kind, reg: g.reg}
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
	g.reg.DeclareRoute(spec)
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
	g.reg.Declare(method, joinRoutePaths(g.BasePath(), relativePath), "", RouteExempt)
	if method == http.MethodGet {
		return g.RouterGroup.GET(relativePath, handlers...)
	}
	return g.RouterGroup.POST(relativePath, handlers...)
}

func (g *RouteGroup) declareAndRegister(method, relativePath, perm string, handlers ...gin.HandlerFunc) gin.IRoutes {
	g.reg.Declare(method, joinRoutePaths(g.BasePath(), relativePath), perm, g.Kind)
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
