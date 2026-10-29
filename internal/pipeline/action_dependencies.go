package pipeline

import (
	"sort"
	"strings"

	"go_wp/internal/builder/core"
)

func PageTargetKey(id string) DepKey {
	return DepKey{Kind: DepKindPageTarget, Key: "page_target:" + id}
}
func PresentationTargetKey(id string) DepKey {
	return DepKey{Kind: DepKindPresentationTarget, Key: "presentation_target:" + id}
}

func (u *CompileUsage) UseActionTarget(kind core.ActionKind, id string) {
	id = strings.TrimSpace(id)
	if u == nil || id == "" || (kind != core.ActionPage && kind != core.ActionPresentation) {
		return
	}
	if u.ActionTargets == nil {
		u.ActionTargets = map[core.ActionKind]map[string]bool{}
	}
	if u.ActionTargets[kind] == nil {
		u.ActionTargets[kind] = map[string]bool{}
	}
	u.ActionTargets[kind][id] = true
}

// ActionTargetDependencies 仅投影真实消费成功的目标，Page/Presentation共用此唯一形状。
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
			case core.ActionPage:
				key = PageTargetKey(id)
			case core.ActionPresentation:
				key = PresentationTargetKey(id)
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
