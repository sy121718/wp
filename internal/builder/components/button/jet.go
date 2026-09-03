// Package button — Jet 渲染路径辅助导出（Phase 0 样板）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 渲染数据计算保留在 Go，
// HTML 拼装交给 button.jet 模板。render 函数保持不变（旧输出），本文件只做
// 最小导出与等价的数据准备，供 builder/jetview.go 的 nodeView 转换层复用。
package button

import (
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出按钮样式编译（复用 render 内部的 compileCSS，CSS 字节与旧路径一致）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View button 渲染视图数据（供 button.jet 模板使用）。
type View struct {
	// Tag 语义标签：a（跳转）或 button（弹窗触发）。
	Tag string
	// Attrs 前导空格 + 属性串（href/target/rel/data-modal-target，均已转义）。
	Attrs string
	// Text 按钮文本（模板输出时由 Jet 默认转义）。
	Text string
	// IconPrefix 前缀图标 HTML（空则无，原样输出）。
	IconPrefix string
	// IconSuffix 后缀图标 HTML（空则无，原样输出）。
	IconSuffix string
}

// BuildView 生成按钮渲染视图：标签选择 + 链接协议 + 图标（与 render 输出结构一致）。
func BuildView(p *Props, content core.ContentResolver, media core.MediaResolver) (View, error) {
	iconHTML, err := renderIcon(p, &core.AtomRender{Media: media})
	if err != nil {
		return View{}, err
	}
	tag, attrs, err := buildAttrs(p, content)
	if err != nil {
		return View{}, err
	}
	v := View{Tag: tag, Attrs: attrs, Text: p.Text}
	if p.Icon != nil {
		if p.Icon.Position == "suffix" {
			v.IconSuffix = iconHTML
		} else {
			v.IconPrefix = iconHTML
		}
	}
	return v, nil
}

// buildAttrs 标签与属性选择（与 render 内联逻辑逐字一致，保持旧输出不变）。
func buildAttrs(p *Props, content core.ContentResolver) (tag, attrs string, err error) {
	tag = "a"
	switch p.Action {
	case ActionModal:
		tag = "button"
		attrs = ` type="button" data-modal-target="` + html.EscapeString(p.Value) + `"`
	case ActionLink:
		if content == nil {
			return "", "", fmt.Errorf("编译上下文缺少内容解析器，无法解析动态链接")
		}
		v, e := content.ResolveString(p.Binding.Field)
		if e != nil {
			return "", "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, e)
		}
		if v == "" {
			return "", "", fmt.Errorf("绑定字段 %q 为空，无 fallback 兜底", p.Binding.Field)
		}
		attrs = ` href="` + html.EscapeString(v) + `"`
	case ActionAnchor:
		attrs = ` href="#` + html.EscapeString(p.Value) + `"`
	case ActionNative, ActionInternal:
		attrs = ` href="` + html.EscapeString(p.Value) + `"`
	default: // external
		attrs = ` href="` + html.EscapeString(p.Value) + `"`
		relParts := []string{}
		if p.Target == "blank" {
			attrs += ` target="_blank"`
			relParts = append(relParts, "noopener", "noreferrer")
		}
		if p.Rel == "nofollow" || p.Rel == "sponsored" {
			relParts = append(relParts, p.Rel)
		}
		if len(relParts) > 0 {
			attrs += ` rel="` + strings.Join(relParts, " ") + `"`
		}
	}
	return tag, attrs, nil
}
