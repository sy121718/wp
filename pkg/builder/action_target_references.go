package builder

import (
	"go_wp/pkg/builder/core"
	json "go_wp/pkg/jsonx"
)

func ReferencesActionTarget(raw json.RawMessage, kind, id string) (bool, error) {
	page, err := ParsePage(raw)
	if err != nil {
		return false, err
	}
	var visit func([]*core.Node) bool
	visit = func(nodes []*core.Node) bool {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			for _, target := range node.Actions {
				if kind == string(core.ActionPage) && target.Kind == core.ActionPage && target.PageID == id {
					return true
				}
				if kind == string(core.ActionPresentation) && target.Kind == core.ActionPresentation && target.InstanceID == id {
					return true
				}
			}
			if visit(node.Children) {
				return true
			}
		}
		return false
	}
	return visit(page.Root), nil
}
