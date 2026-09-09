package core

import (
	"fmt"
	"strings"
)

// 响应式断点标识。
const (
	BreakpointDesktop = "desktop" // 默认样式，无媒体查询
	BreakpointTablet  = "tablet"  // @media (max-width: 1024px)
	BreakpointMobile  = "mobile"  // @media (max-width: 767px)
)

// breakpointMedia 断点媒体查询（desktop-first）。
var breakpointMedia = map[string]string{
	BreakpointTablet: "@media (max-width: 1024px)",
	BreakpointMobile: "@media (max-width: 767px)",
}

// keyframesCSS 通用动效关键帧（入场 17 种 + 循环 4 种），仅实际被使用时输出。
var keyframesCSS = map[string]string{
	// 入场（entrance）
	"wp-fade-in":     "@keyframes wp-fade-in {\n  from { opacity: 0 }\n  to { opacity: 1 }\n}",
	"wp-fade-up":     "@keyframes wp-fade-up {\n  from { opacity: 0; transform: translateY(16px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-fade-down":   "@keyframes wp-fade-down {\n  from { opacity: 0; transform: translateY(-16px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-fade-left":   "@keyframes wp-fade-left {\n  from { opacity: 0; transform: translateX(16px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-fade-right":  "@keyframes wp-fade-right {\n  from { opacity: 0; transform: translateX(-16px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-zoom-in":     "@keyframes wp-zoom-in {\n  from { opacity: 0; transform: scale(.92) }\n  to { opacity: 1; transform: none }\n}",
	"wp-zoom-out":    "@keyframes wp-zoom-out {\n  from { opacity: 0; transform: scale(1.08) }\n  to { opacity: 1; transform: none }\n}",
	"wp-slide-up":    "@keyframes wp-slide-up {\n  from { opacity: 0; transform: translateY(24px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-slide-down":  "@keyframes wp-slide-down {\n  from { opacity: 0; transform: translateY(-24px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-slide-left":  "@keyframes wp-slide-left {\n  from { opacity: 0; transform: translateX(24px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-slide-right": "@keyframes wp-slide-right {\n  from { opacity: 0; transform: translateX(-24px) }\n  to { opacity: 1; transform: none }\n}",
	"wp-flip-x":      "@keyframes wp-flip-x {\n  from { opacity: 0; transform: perspective(600px) rotateX(-12deg) }\n  to { opacity: 1; transform: none }\n}",
	"wp-flip-y":      "@keyframes wp-flip-y {\n  from { opacity: 0; transform: perspective(600px) rotateY(-12deg) }\n  to { opacity: 1; transform: none }\n}",
	"wp-blur-in":     "@keyframes wp-blur-in {\n  from { opacity: 0; filter: blur(8px) }\n  to { opacity: 1; filter: none }\n}",
	"wp-bounce-in":   "@keyframes wp-bounce-in {\n  0% { opacity: 0; transform: scale(.8) }\n  60% { opacity: 1; transform: scale(1.04) }\n  100% { opacity: 1; transform: none }\n}",
	"wp-rotate-in":   "@keyframes wp-rotate-in {\n  from { opacity: 0; transform: rotate(-6deg) scale(.96) }\n  to { opacity: 1; transform: none }\n}",
	// 循环（attention）
	"wp-loop-pulse": "@keyframes wp-loop-pulse {\n  0%, 100% { transform: scale(1) }\n  50% { transform: scale(1.03) }\n}",
	"wp-loop-float": "@keyframes wp-loop-float {\n  0%, 100% { transform: translateY(0) }\n  50% { transform: translateY(-8px) }\n}",
	"wp-loop-glow":  "@keyframes wp-loop-glow {\n  0%, 100% { box-shadow: 0 0 0 rgba(59,130,246,0) }\n  50% { box-shadow: 0 0 16px rgba(59,130,246,.35) }\n}",
	"wp-loop-spin":  "@keyframes wp-loop-spin {\n  from { transform: rotate(0deg) }\n  to { transform: rotate(360deg) }\n}",
}

// keyframesOrder 关键帧输出顺序（保证确定性）。
var keyframesOrder = []string{
	"wp-fade-in", "wp-fade-up", "wp-fade-down", "wp-fade-left", "wp-fade-right",
	"wp-zoom-in", "wp-zoom-out",
	"wp-slide-up", "wp-slide-down", "wp-slide-left", "wp-slide-right",
	"wp-flip-x", "wp-flip-y", "wp-blur-in", "wp-bounce-in", "wp-rotate-in",
	"wp-loop-pulse", "wp-loop-float", "wp-loop-glow", "wp-loop-spin",
}

// CSSBuckets 三端 CSS 规则集合。
// 规则按文档序（前序遍历）追加，最终按 关键帧 → 桌面 → 平板 → 手机 的固定顺序拼接，
// 保证确定性输出（同一 Page Document 产生相同字节）。
// CSSDecl 构造单条 CSS 声明：property: v1 v2 v3。
// 跳过空值（空值不参与拼接，避免产生 "prop:  " 这类无效声明）。
// 例：CSSDecl("border-top", "1px", "solid", "red") → "border-top: 1px solid red"
func CSSDecl(prop string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return prop + ": " + strings.Join(parts, " ")
}

type CSSBuckets struct {
	desktop   []string
	tablet    []string
	mobile    []string
	keyframes map[string]bool
	// seen 已输出的规则（断点+规则体），重复规则只保留首份：
	// 多张图共享同一骨架规则时不产生重复 CSS，确定性不受影响。
	seen map[string]bool
	// 组件自定义关键帧（如容器背景轮播）：按加入顺序输出，同名只输出一次。
	customKeyframes map[string]string
	customOrder     []string
}

// AddKeyframes 追加组件自定义关键帧（name 唯一，重复调用只保留首份，保证确定性）。
func (b *CSSBuckets) AddKeyframes(name, css string) {
	if b.customKeyframes == nil {
		b.customKeyframes = map[string]string{}
	}
	if _, ok := b.customKeyframes[name]; ok {
		return
	}
	b.customKeyframes[name] = css
	b.customOrder = append(b.customOrder, name)
}

// Add 向指定断点追加一条规则；空声明被忽略，无有效声明的规则不输出。
func (b *CSSBuckets) Add(breakpoint, selector string, decls []string) {
	filtered := make([]string, 0, len(decls))
	for _, d := range decls {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		return
	}
	rule := selector + " {\n" + indentDecl(filtered) + "}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := breakpoint + "\x00" + rule
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	switch breakpoint {
	case BreakpointDesktop:
		b.desktop = append(b.desktop, rule)
	case BreakpointTablet:
		b.tablet = append(b.tablet, rule)
	case BreakpointMobile:
		b.mobile = append(b.mobile, rule)
	}
}

// NeedKeyframes 标记需要输出的关键帧。
func (b *CSSBuckets) NeedKeyframes(name string) {
	if b.keyframes == nil {
		b.keyframes = map[string]bool{}
	}
	b.keyframes[name] = true
}

// String 按固定顺序拼接全部 CSS。
func (b *CSSBuckets) String() string {
	parts := make([]string, 0, len(b.desktop)+len(b.tablet)+len(b.mobile)+2)
	for _, name := range keyframesOrder {
		if b.keyframes[name] {
			parts = append(parts, keyframesCSS[name])
		}
	}
	for _, name := range b.customOrder {
		parts = append(parts, b.customKeyframes[name])
	}
	parts = append(parts, b.desktop...)
	if len(b.tablet) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointTablet], strings.Join(b.tablet, "\n")))
	}
	if len(b.mobile) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointMobile], strings.Join(b.mobile, "\n")))
	}
	return strings.Join(parts, "\n\n")
}

// NodeClass 节点 CSS 类名。
func NodeClass(id string) string {
	return "wp-c-" + id
}

// indentDecl 声明列表缩进格式化。
func indentDecl(decls []string) string {
	var sb strings.Builder
	for _, d := range decls {
		sb.WriteString("  ")
		sb.WriteString(d)
		sb.WriteString(";\n")
	}
	return sb.String()
}
