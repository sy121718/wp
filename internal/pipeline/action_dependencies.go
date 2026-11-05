package pipeline

import (
	"go_wp/pkg/builder/core"
	"go_wp/pkg/publish"
)

// PageTargetKey / PresentationTargetKey 是应用侧依赖键构造；
// 依赖键本身是发布内核的中立形状。
func PageTargetKey(id string) publish.DepKey {
	return publish.DepKey{Kind: publish.DepKindPageTarget, Key: "page_target:" + id}
}
func PresentationTargetKey(id string) publish.DepKey {
	return publish.DepKey{Kind: publish.DepKindPresentationTarget, Key: "presentation_target:" + id}
}

// ActionUsage 把发布内核的用量记录适配成构建器认识的接口。
//
// 构建器的 core.ActionKind 是它自己的字符串类型；pkg/publish 不认识 builder，
// 由应用在接线处做一次显式转换，避免为了一个方法把 builder 拖进发布内核。
type ActionUsage struct {
	*publish.CompileUsage
}

// NewActionUsage 返回同时满足构建器 UsageRecorder 与动作目标记录的用量对象。
//
// 传入值既可能是发布内核的用量对象，也可能是别的 UsageRecorder 实现；
// 显式断言失败时原样返回调用方的 recorder，不悄悄换成另一份空用量。
func NewActionUsage(u core.UsageRecorder) core.UsageRecorder {
	if u == nil {
		return &ActionUsage{CompileUsage: &publish.CompileUsage{}}
	}
	if cu, ok := u.(*publish.CompileUsage); ok {
		return &ActionUsage{CompileUsage: cu}
	}
	return u
}

// UseActionTarget 实现 core.ActionTargetUsageRecorder。
func (u *ActionUsage) UseActionTarget(kind core.ActionKind, id string) {
	if u == nil || u.CompileUsage == nil {
		return
	}
	var mapped publish.ActionTargetKind
	switch kind {
	case core.ActionPage:
		mapped = publish.ActionTargetPage
	case core.ActionPresentation:
		mapped = publish.ActionTargetPresentation
	default:
		return
	}
	u.CompileUsage.UseActionTarget(mapped, id)
}
