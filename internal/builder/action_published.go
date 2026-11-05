package builder

import (
	"errors"
	"fmt"
	"go_wp/pkg/builder/core"
)

var ErrPublishedActionUnavailable = errors.New("已发布动作目标当前不可用")

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
			return core.ResolvedAction{}, fmt.Errorf("%w: %w", ErrPublishedActionUnavailable, err)
		}
		if path == "" {
			return core.ResolvedAction{}, fmt.Errorf("%w: 动作%s的目标尚未在当前语言发布", core.ErrIncompleteNode, key)
		}
		return core.ResolvedAction{Kind: target.Kind, URL: path}, nil
	})
}
