package publish

import (
	"sort"
	"strings"
)

// ActionTargetKind 是「页面/实例选择器」的动作目标类别。
type ActionTargetKind string

const (
	ActionTargetPage         ActionTargetKind = "page"
	ActionTargetPresentation ActionTargetKind = "presentation"
)

// UseActionTarget 记录一次动作目标引用（页面或实例）。
func (u *CompileUsage) UseActionTarget(kind ActionTargetKind, id string) {
	id = strings.TrimSpace(id)
	if u == nil || id == "" ||
		(kind != ActionTargetPage && kind != ActionTargetPresentation) {
		return
	}
	if u.ActionTargets == nil {
		u.ActionTargets = map[string]map[string]bool{}
	}
	if u.ActionTargets[string(kind)] == nil {
		u.ActionTargets[string(kind)] = map[string]bool{}
	}
	u.ActionTargets[string(kind)][id] = true
}

// ActionTargetDependencies 把已记录的动作目标引用转成发布依赖。
func ActionTargetDependencies(u *CompileUsage) []Dependency {
	if u == nil {
		return nil
	}
	var out []Dependency
	for kind, ids := range u.ActionTargets {
		for id, used := range ids {
			if !used {
				continue
			}
			var key DepKey
			switch kind {
			case string(ActionTargetPage):
				key = DepKey{Kind: DepKindPageTarget, Key: "page_target:" + id}
			case string(ActionTargetPresentation):
				key = DepKey{Kind: DepKindPresentationTarget, Key: "presentation_target:" + id}
			default:
				continue
			}
			out = append(out, Dependency{Kind: key.Kind, Key: key.Key})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Key < out[j].Key
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
