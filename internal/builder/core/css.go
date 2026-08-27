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

// keyframesCSS 通用入场动效关键帧，仅在实际被使用时输出。
var keyframesCSS = map[string]string{
	"wp-fade-in":  "@keyframes wp-fade-in {\n  from { opacity: 0 }\n  to { opacity: 1 }\n}",
	"wp-slide-up": "@keyframes wp-slide-up {\n  from { opacity: 0; transform: translateY(24px) }\n  to { opacity: 1; transform: none }\n}",
}

// keyframesOrder 关键帧输出顺序（保证确定性）。
var keyframesOrder = []string{"wp-fade-in", "wp-slide-up"}

// CSSBuckets 三端 CSS 规则集合。
// 规则按文档序（前序遍历）追加，最终按 关键帧 → 桌面 → 平板 → 手机 的固定顺序拼接，
// 保证确定性输出（同一 Page Document 产生相同字节）。
type CSSBuckets struct {
	desktop   []string
	tablet    []string
	mobile    []string
	keyframes map[string]bool
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
