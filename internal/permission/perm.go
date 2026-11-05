package permission

import (
	"cmp"
	"slices"
	"sync"

	mw "go_wp/pkg/middleware"
)

// Perm 是权限标识；终态为无类型字符串，由各操作属主声明常量。
type Perm = string

// Exempt 表示明确豁免后台权限目录。
const Exempt Perm = ""

// RouteSpec 是已登记的路由绑定事实（复用 pkg 的中立形状）。
type RouteSpec = mw.RouteSpec

// 权限名称/模块的目录由数据库提供，这里只保留进程内登记。
type spec struct {
	module string
	name   string
}

var regMu sync.Mutex

// RegisterSpec 登记权限点的模块与名称（由 admin 业务在装配时提供，不来自代码目录）。
func RegisterSpec(p Perm, module, name string) {
	if p == Exempt {
		return
	}
	regMu.Lock()
	defer regMu.Unlock()
	if specs == nil {
		specs = map[Perm]spec{}
	}
	specs[p] = spec{module: module, name: name}
}

// Declare 登记一条路由绑定。
func Declare(method, path string, p Perm) {
	kind := mw.RouteProtected
	if p == Exempt {
		kind = mw.RouteExempt
	}
	mw.Declare(method, path, p, kind)
}

// Known 报告权限点是否在已登记目录中。
func Known(p Perm) bool {
	if p == Exempt {
		return false
	}
	regMu.Lock()
	defer regMu.Unlock()
	_, ok := specs[p]
	return ok
}

// Label 返回权限点的模块与名称。
func Label(p Perm) (module, name string, ok bool) {
	regMu.Lock()
	defer regMu.Unlock()
	sp, ok := specs[p]
	if !ok {
		return "", "", false
	}
	return sp.module, sp.name, true
}

// All 返回已登记目录里的全部权限点。
func All() []Perm {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Perm, 0, len(specs))
	for p := range specs {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Perm) int {
		ma, na, _ := Label(a)
		mb, nb, _ := Label(b)
		return cmp.Or(cmp.Compare(ma, mb), cmp.Compare(na, nb), cmp.Compare(a, b))
	})
	return out
}

// RoutesOf 返回某权限点绑定的全部路由。
func RoutesOf(p Perm) []RouteSpec { return mw.RoutesOf(p) }

// Snapshot 返回已登记绑定的只读副本。
func Snapshot() []RouteSpec { return mw.Snapshot() }

// Exempts 返回明确豁免的路由键。
func Exempts() []string { return mw.Exempts() }

// Reset 清空登记与目录；只允许装配与测试使用。
func Reset() {
	mw.Reset()
	regMu.Lock()
	defer regMu.Unlock()
	specs = nil
}
