package pipeline

import (
	"go_wp/pkg/publish"
	"sort"
	"strings"

	"go_wp/pkg/builder/core"
)

func PageTargetKey(id string) publish.DepKey {
	return publish.DepKey{Kind: publish.DepKindPageTarget, Key: "page_target:" + id}
}
func PresentationTargetKey(id string) publish.DepKey {
	return publish.DepKey{Kind: publish.DepKindPresentationTarget, Key: "presentation_target:" + id}
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

func ActionTargetDependencies(u *CompileUsage) []publish.Dependency {
	if u == nil {
		return nil
	}
	var out []publish.Dependency
	for kind, ids := range u.ActionTargets {
		for id, used := range ids {
			if !used {
				continue
			}
			var key publish.DepKey
			switch kind {
			case core.ActionPage:
				key = PageTargetKey(id)
			case core.ActionPresentation:
				key = PresentationTargetKey(id)
			default:
				continue
			}
			out = append(out, publish.Dependency{Kind: key.Kind, Key: key.Key})
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
