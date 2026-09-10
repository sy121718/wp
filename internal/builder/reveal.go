package builder

// reveal.go — 滚动显现分层开关的注入层（H5「滚动过去才出内容」）。
//
// 分层：主题（ScrollRevealDefault 全站开关）→ 页面（待立项）→ 容器（StyleEx.Reveal
// 子树覆盖，栈式）→ 组件（显式设置永远优先）。
//
// 实现为结构体层注入（零 JSON 解析开销）：Atom 基座组件在 advancedClasses 注入
//（AdvancedProps.Interaction），container 在自身 viewOf 注入（顶层 Interaction）。
// 老浏览器忽略 animation-timeline 自动降级为直接显示，产物永不丢内容。

import (
	"go_wp/internal/builder/core"
)

// applyScrollReveal 未显式配置动效时注入默认入场 + 滚动触发。
// 显式优先：entrance / scrollStory / scrollReveal 任一非空即不注入（作者语义优先）。
func applyScrollReveal(inter *core.InteractionProps, defaultEntrance string) {
	if inter.Entrance != "" || inter.ScrollStory != "" || inter.ScrollReveal != "" {
		return
	}
	inter.Entrance = defaultEntrance
	inter.ScrollReveal = "reveal"
}

// revealActive 当前上下文是否启用滚动显现注入。
func revealActive(ctx *core.RenderContext) bool {
	return ctx != nil && ctx.RevealInherit == "on" && ctx.RevealDefaultEntrance != ""
}
