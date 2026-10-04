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
// 三条 fail-fast 校验，都是装配缺陷、必须当场炸掉：
//   - perm 不在常量表里（含拼写错误的字面量）→ panic，而不是安静地建一个没人认识的权限点；
//   - 同一 method+path 被声明了**两种不同**的权限点（或豁免与权限点混用）→ panic
//     ——两份注册点抢同一条路径却要不同权限，无论哪份生效都有一半是错的；
//   - 重复登记**同一个**权限点是幂等的（见下）。
//
// 幂等这条不是宽容，而是必需：同一进程内装配两次是常见场景 —— feature 测试每个用例都会
// 装配一次路由，而声明表是包级全局的 —— 把它当错误会让「多跑一次装配」直接 panic。
// 实测触发形态：全量测试按包并发时每个用例各装配一次，报「同一路由被声明了两次权限点：
// GET /api/analytics/summary → analytics:view / analytics:view」（两个值相同，根本不是冲突）；
// 而单跑 -run 一个用例反而全绿，很容易被当成 flaky 忽略过去。
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
	declaredKeys[key] = p
	declared = append(declared, RouteSpec{Method: method, Path: path, Perm: p, Module: s.module, Name: s.name})
}

// Known 判断权限点是否已在常量表（codes.go 的 specs）里登记。
//
// 用途：**外部输入**里的权限点要先过这一关 —— 令牌的 scope、配置里的权限点清单都来自
// 字符串数组，拼错的字面量（"order:lst"）或已下线的旧值必须当场拒掉。
// 静默忽略的代价是「生成了一个比申请人以为的更弱的令牌」，问题要到第一次调用时才暴露。
//
// 注意它不看声明表（declared）：声明发生在装配期，而这一步是**校验输入**，
// 判据应该是「这个权限点存在吗」而不是「它现在挂上路由了吗」。
func Known(p Perm) bool {
	if p == Exempt {
		return false
	}
	_, ok := specs[p]
	return ok
}

// Label 返回权限点的中文名与所属模块（未登记时 ok=false）。
//
// 供后台把权限点渲染成可读的选项（令牌 scope 选择器、角色授权树）——
// 让调用方去读 codes.go 的私有 map 是不可能的，而把中文名抄进调用方就是第二份真源。
func Label(p Perm) (module, name string, ok bool) {
	sp, ok := specs[p]
	if !ok {
		return "", "", false
	}
	return sp.module, sp.name, true
}

// All 返回全部已登记的权限点（按模块 + 中文名排序，顺序稳定）。
//
// 排序固定是为了**展示与比对都可复现**：渲染成选项列表时顺序不该随 map 遍历变化，
// 用例断言清单时也不该需要先排序。
func All() []Perm {
	out := make([]Perm, 0, len(specs))
	for p := range specs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		mi, ni, _ := Label(out[i])
		mj, nj, _ := Label(out[j])
		if mi != mj {
			return mi < mj
		}
		if ni != nj {
			return ni < nj
		}
		return out[i] < out[j]
	})
	return out
}

// RoutesOf 返回声明了权限点 p 的路由（按 路径 + 方法 排序，结果稳定）。
//
// 用途：**没有 HTTP 请求上下文**的调用点（AI 工具、后台任务、导出）手里只有权限点，
// 而 Casbin 策略的 obj 是路由路径。把这层换算收在这里，判定口径就与路由中间件同源
// （同一份 declared 表）—— 自己另写一份「权限点 → 策略」的映射等于给同一件事两套真源，
// 而分叉的表现是「页面上看不见的东西，AI 却查得到」。
//
// 空结果表示这个权限点还没被任何路由声明过：调用方应当按**无权限**处理（fail closed），
// 而不是跳过判定。请注意一个权限点通常声明在多条路由上（一条 API + 若干后台页面路由），
// 判定时**任一条通过即可**（它们同属一个能力）。
func RoutesOf(p Perm) []RouteSpec {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]RouteSpec, 0, 2)
	for _, r := range declared {
		if r.Perm == p {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
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
