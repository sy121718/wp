package core

import "fmt"

type ActionResolver func(node *Node, key string) (ResolvedAction, error)

func (ctx *RenderContext) SetActionResolver(resolve ActionResolver) { ctx.actionResolver = resolve }
func (ctx *RenderContext) ResolveAction(node *Node, key string) (ResolvedAction, error) {
	if ctx == nil || ctx.actionResolver == nil {
		return ResolvedAction{}, fmt.Errorf("%w: 动作%s尚未配置或解析", ErrIncompleteNode, key)
	}
	resolved, err := ctx.actionResolver(node, key)
	if err == nil && node != nil {
		target := node.Actions[key]
		if target.Kind == resolved.Kind {
			switch target.Kind {
			case ActionPage:
				ctx.UseActionTarget(target.Kind, target.PageID)
			case ActionPresentation:
				ctx.UseActionTarget(target.Kind, target.InstanceID)
			}
		}
	}
	return resolved, err
}

type ActionTargetUsageRecorder interface {
	UseActionTarget(kind ActionKind, id string)
}

func (ctx *RenderContext) UseActionTarget(kind ActionKind, id string) {
	if ctx == nil || id == "" || (kind != ActionPage && kind != ActionPresentation) {
		return
	}
	if usage, ok := ctx.usage.(ActionTargetUsageRecorder); ok {
		usage.UseActionTarget(kind, id)
	}
}
