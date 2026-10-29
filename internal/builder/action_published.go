package builder

import (
	"fmt"
	"go_wp/internal/builder/core"
)

// WithPublishedActionPaths 只把两种已发布事实查询适配为编译器动作解析。
// 工程与语言由装配闭包固定，业务模块不接收节点或解释动作协议。
func WithPublishedActionPaths(pagePath, instancePath func(string) (string, error)) CompileOption {
	return WithActionResolver(func(node *core.Node, key string) (core.ResolvedAction, error) {
		target := node.Actions[key]
		var lookup func(string) (string, error)
		var id string
		switch target.Kind {
		case core.ActionPage:
			lookup, id = pagePath, target.PageID
		case core.ActionPresentation:
			lookup, id = instancePath, target.InstanceID
		default:
			return core.ResolvedAction{}, fmt.Errorf("已发布目标解析器不接受动作类型%s", target.Kind)
		}
		if lookup == nil {
			return core.ResolvedAction{}, fmt.Errorf("已发布%s目标解析未装配", target.Kind)
		}
		path, err := lookup(id)
		if err != nil {
			return core.ResolvedAction{}, err
		}
		if path == "" {
			return core.ResolvedAction{}, fmt.Errorf("%w: 动作%s的目标尚未在当前语言发布", core.ErrIncompleteNode, key)
		}
		return core.ResolvedAction{Kind: target.Kind, URL: path}, nil
	})
}
