// perm.go — 权限点声明注册表。
//
// 背景（审计 SEC-011）：authorizedAPI 组统一挂 CasbinMiddleware()，它按**实际请求路径**
// enforce；权限点却是数据库里的 seed 数据，路由是编译期定义 —— 两者的一致性过去只能靠
// scripts/check-permission-gaps.sh 在运行时比对（072/077/078/079/151 各踩过一次：
// 新增接口漏 seed 权限点 → 没有任何策略能匹配 → **含超管在内全员 403**）。
//
// 本包把「这条路由需要哪个权限点」变成**注册动作的一部分**：路由在 permission.RouteGroup
// 上注册时必须给出 Perm，路径由注册动作自身（group 的 BasePath + 相对路径）算出，
// 于是「路由 ↔ 权限点」天然一致，不存在第二份清单可以漂移。
//
// 三层分工，互为兜底：
//  1. 编译期：RouteGroup.GET/POST 的第二个参数是 Perm，漏写就是编译错误；
//  2. 装配期：Declare 校验 perm 必须是已登记的常量（拼错 → panic），同一路由重复声明 → panic；
//     装配末尾 SyncToDB 把声明幂等 upsert 进 sys_permission 与超管策略；
//  3. 运行时：scripts/check-permission-gaps.sh 仍保留，抓「路由表与库不一致」（例如人工在
//     库里删了某条权限点），与声明式注册互补。
package permission

import (
	"sort"
	"sync"
)

// Perm 权限点代码（sys_permission.permission_code）。
//
// 用具名类型而不是裸 string：注册点的签名因此不可省略，也不会被「顺手传个变量」蒙混过关。
// 写成字面量（permission.Perm("product:lst")）会在 Declare 阶段因「不在常量表里」panic ——
// 拼错不可能静默上线。
type Perm string

// Exempt 显式豁免：该路由挂在 Casbin 组下但不声明权限点（例如登录后人人可用的自用接口）。
//
// 用 Exempt 而不是省略参数：豁免必须是**看得见的选择**，而不是忘记写。装配末尾会把豁免
// 清单打进启动日志，scripts/check-permission-gaps.sh 的 EXEMPT 数组是同一份名单。
const Exempt Perm = ""

// spec 一条权限点的元数据（来自 codes.go 的常量表）。
type spec struct {
	module string
	name   string
}

// RouteSpec 一条已声明的授权路由（Path 是**绝对路径**，等于运行时真实注册的路径）。
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
)

// Declare 登记一条授权路由（由 RouteGroup 的 GET/POST 调用，业务代码不直接调）。
//
// 两条 fail-fast 校验，都是装配缺陷、必须当场炸掉：
//   - perm 不在常量表里（含拼写错误的字面量）→ panic，而不是安静地建一个没人认识的权限点；
//   - 同一 method+path 被声明两次 → panic（同一路由的两个权限点等于没有确定语义）。
func Declare(method, path string, p Perm) {
	regMu.Lock()
	defer regMu.Unlock()
	if declaredKeys == nil {
		declaredKeys = map[string]Perm{}
	}
	key := method + " " + path
	if p == Exempt {
		if _, dup := declaredKeys[key]; !dup {
			declaredKeys[key] = Exempt
			exemptRoutes = append(exemptRoutes, key)
		}
		return
	}
	s, ok := specs[p]
	if !ok {
		panic("权限点未登记：路由 " + key + " 声明了 " + string(p) +
			"，但 internal/permission/codes.go 里没有这条常量（拼写错误？新增权限点要同时加常量）")
	}
	if prev, dup := declaredKeys[key]; dup {
		panic("同一路由被声明了两次权限点：" + key + " → " + string(prev) + " / " + string(p))
	}
	declaredKeys[key] = p
	declared = append(declared, RouteSpec{Method: method, Path: path, Perm: p, Module: s.module, Name: s.name})
}

// Snapshot 返回已声明路由的副本（按 路径 + 方法 排序，保证启动日志与测试输出稳定）。
func Snapshot() []RouteSpec {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]RouteSpec, len(declared))
	copy(out, declared)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Exempts 返回显式豁免的路由清单（"METHOD /path"，已排序）。
func Exempts() []string {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]string, len(exemptRoutes))
	copy(out, exemptRoutes)
	sort.Strings(out)
	return out
}

// Reset 清空注册表（仅测试用：同一进程内需要重新采集装配结果时调用）。
func Reset() {
	regMu.Lock()
	defer regMu.Unlock()
	declared = nil
	declaredKeys = nil
	exemptRoutes = nil
}
