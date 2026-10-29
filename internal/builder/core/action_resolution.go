package core

import "fmt"

// ActionResolver 由构建器注入，组件只能读取已按工程/语言/文档验证的动作结果。
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

// ActionTargetUsageRecorder 是跨页动作消费的可选窄口，既有组件记录器无需伪造此能力。
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
