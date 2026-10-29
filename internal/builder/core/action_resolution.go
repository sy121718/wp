package core

import "fmt"

// ActionResolver 由构建器注入，组件只能读取已按工程/语言/文档验证的动作结果。
type ActionResolver func(node *Node, key string) (ResolvedAction, error)

func (ctx *RenderContext) SetActionResolver(resolve ActionResolver) { ctx.actionResolver = resolve }
func (ctx *RenderContext) ResolveAction(node *Node, key string) (ResolvedAction, error) {
	if ctx == nil || ctx.actionResolver == nil {
		return ResolvedAction{}, fmt.Errorf("%w: 动作%s尚未配置或解析", ErrIncompleteNode, key)
	}
	return ctx.actionResolver(node, key)
}
