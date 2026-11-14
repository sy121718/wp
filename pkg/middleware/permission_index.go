package middleware

import "slices"

// PermIndex 是一次装配在**验证通过后**建立的不可变权限查询。
//
// 它由 routers 在「登记冻结 → 目录核对通过」之后用 Registry 快照构造，
// 构造后不再变更：查询只读、返回副本，执行期不存在「运行中重建索引」的窗口。
// 索引按权限码分组，查询是 O(1) 映射读取，不是线性扫描。
//
// 未构造（nil）或权限码没有绑定时返回空 —— 调用方必须按拒绝处理，
// 缺索引不能变成放行。
type PermIndex struct {
	byPerm map[string][]RouteSpec
}

// BuildPermIndex 用一份已冻结的登记快照建立索引。
//
// 只保留受保护操作（RouteProtected）：页面与后台写入口复用别的权限对象，
// 它们的存在不改变「这个权限码能做什么」的判定；工具执行只关心受保护操作。
func BuildPermIndex(specs []RouteSpec) *PermIndex {
	byPerm := make(map[string][]RouteSpec, len(specs))
	for _, spec := range specs {
		if spec.Perm == "" || spec.Kind != RouteProtected {
			continue
		}
		byPerm[spec.Perm] = append(byPerm[spec.Perm], spec)
	}
	return &PermIndex{byPerm: byPerm}
}

// Of 返回某权限码绑定的全部受保护操作；未命中返回空切片。
func (i *PermIndex) Of(perm string) []RouteSpec {
	if i == nil {
		return nil
	}
	routes := i.byPerm[perm]
	if len(routes) == 0 {
		return nil
	}
	out := make([]RouteSpec, len(routes))
	copy(out, routes)
	return out
}

// Has 报告权限码是否有绑定的受保护操作。
func (i *PermIndex) Has(perm string) bool {
	if i == nil {
		return false
	}
	_, ok := i.byPerm[perm]
	return ok
}

// Perms 返回索引里的全部权限码（排序，供审计与快照比对）。
func (i *PermIndex) Perms() []string {
	if i == nil {
		return nil
	}
	out := make([]string, 0, len(i.byPerm))
	for perm := range i.byPerm {
		out = append(out, perm)
	}
	slices.Sort(out)
	return out
}
