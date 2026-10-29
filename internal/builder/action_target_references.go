package builder

import (
	"go_wp/internal/builder/core"
	json "go_wp/pkg/jsonx"
)

// ReferencesActionTarget 仅提取既有五分支合同中的受控页面/实例引用。
// 数据源模块不另行解释 AST，也不把任意 props 字符串当作引用。
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
