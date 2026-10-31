package permission

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

type Perm string

const Exempt Perm = ""

type spec struct {
	module string
	name   string
}

type RouteSpec struct {
	Method string
	Path   string
	Perm   Perm
	Module string
	Name   string
}

var (
	regMu        sync.Mutex
	declared     []RouteSpec
	declaredKeys map[string]Perm
	exemptRoutes []string

	apiPermPaths map[Perm]string
)

func Declare(method, path string, p Perm) {
	regMu.Lock()
	defer regMu.Unlock()
	if declaredKeys == nil {
		declaredKeys = map[string]Perm{}
	}
	key := method + " " + path
	if prev, dup := declaredKeys[key]; dup {
		if prev == p {
			return
		}
		if p == Exempt {
			panic("同一路由既声明了权限点又声明了豁免：" + key + " → " + string(prev) + " / Exempt")
		}
		if prev == Exempt {
			panic("同一路由既声明了豁免又声明了权限点：" + key + " → Exempt / " + string(p))
		}
		panic("同一路由被声明了两种权限点：" + key + " → " + string(prev) + " / " + string(p))
	}
	if p == Exempt {
		declaredKeys[key] = Exempt
		exemptRoutes = append(exemptRoutes, key)
		return
	}
	s, ok := specs[p]
	if !ok {
		panic("权限点未登记：路由 " + key + " 声明了 " + string(p) +
			"，但 internal/permission/codes.go 里没有这条常量（拼写错误？新增权限点要同时加常量）")
	}
	if strings.HasPrefix(path, "/api/") {
		if apiPermPaths == nil {
			apiPermPaths = map[Perm]string{}
		}
		if prev, used := apiPermPaths[p]; used && prev != key {
			panic("同一权限点绑定到多条 API 路由：" + string(p) + " → " + prev + " / " + key +
				"（sys_permission.permission_code 唯一，只有一条路径能生成超管策略，" +
				"其余路径对所有人 403 —— 每条 API 路由要各自一个权限点）")
		}
		apiPermPaths[p] = key
	}
	declaredKeys[key] = p
	declared = append(declared, RouteSpec{Method: method, Path: path, Perm: p, Module: s.module, Name: s.name})
}

func Known(p Perm) bool {
	if p == Exempt {
		return false
	}
	_, ok := specs[p]
	return ok
}

func Label(p Perm) (module, name string, ok bool) {
	sp, ok := specs[p]
	if !ok {
		return "", "", false
	}
	return sp.module, sp.name, true
}

func All() []Perm {
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

func RoutesOf(p Perm) []RouteSpec {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]RouteSpec, 0, 2)
	for _, r := range declared {
		if r.Perm == p {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b RouteSpec) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return out
}

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

func Exempts() []string {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]string, len(exemptRoutes))
	copy(out, exemptRoutes)
	slices.Sort(out)
	return out
}

func Reset() {
	regMu.Lock()
	defer regMu.Unlock()
	declared = nil
	declaredKeys = nil
	exemptRoutes = nil
	apiPermPaths = nil
}
