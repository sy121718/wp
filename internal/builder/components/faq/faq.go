// Package faq 实现 core.faq 常见问题组件（对标 GrapesJS FAQ/Accordion 组件生态）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：
// Items 数组驱动，原生 <details>/<summary> 展开收起，零客户端 JS。
package faq

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.faq"

// 常量上限。
const (
	maxFaqItems    = 50   // FAQ 条目上限
	maxQuestionLen = 200  // 问题长度上限
	maxAnswerLen   = 2000 // 答案长度上限
)

// FaqItem 单条常见问题。
type FaqItem struct {
	// Question 问题文本。
	Question string `json:"question"`
	// Answer 答案文本（富文本 HTML 片段，构建期白名单清洗；存量纯文本转义后按段落包装）。
	// 数组元素不参与 schema 反射（Items 无 ct tag，不递归展开），
	// 该 ct 标签用于声明控件契约：编辑器由前端 faqPanel 的 Trix 富文本承担。
	Answer string `json:"answer" ct:"richtext,maxlen=2000,label=答案"`
	// Open 默认展开。
	Open bool `json:"open,omitempty"`
}

// Props core.faq 常见问题属性。
type Props struct {
	// Items 常见问题条目（数组字段，无 ct tag，边界由 validateExtra 校验）。
	Items []FaqItem `json:"items,omitempty"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"question", "answer"},
	},
}

// validateExtra 关系性校验：条目非空、数量上限、问题/答案非空与长度上限。
func validateExtra(p *Props, nodeID string) (err error) {
	if len(p.Items) == 0 {
		return fmt.Errorf("常见问题至少需要一条")
	}
	if len(p.Items) > maxFaqItems {
		return fmt.Errorf("常见问题条数超上限（%d）: %d", maxFaqItems, len(p.Items))
	}
	for i, it := range p.Items {
		if it.Question == "" {
			return fmt.Errorf("第 %d 条缺少问题", i+1)
		}
		if len(it.Question) > maxQuestionLen {
			return fmt.Errorf("第 %d 条问题超长（上限 %d）", i+1, maxQuestionLen)
		}
		if it.Answer == "" {
			return fmt.Errorf("第 %d 条缺少答案", i+1)
		}
		if len(it.Answer) > maxAnswerLen {
			return fmt.Errorf("第 %d 条答案超长（上限 %d）", i+1, maxAnswerLen)
		}
	}
	return nil
}

// compileCSS 常见问题样式：条目容器 + summary 展开箭头。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex",
		"flex-direction: column",
		"gap: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" details", []string{
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,0.1))",
		"border-radius: 10px",
		"background: var(--sky-c-surface, #fff)",
	})
	b.Add(core.BreakpointDesktop, sel+" summary", []string{
		"list-style: none",
		"cursor: pointer",
		"user-select: none",
		"display: flex",
		"align-items: center",
		"justify-content: space-between",
		"padding: 14px 18px",
		"font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, sel+" summary::-webkit-details-marker", []string{"display: none"})
	b.Add(core.BreakpointDesktop, sel+" summary::after", []string{
		"content: '＋'",
		"font-size: 14px",
		"color: rgba(0,0,0,0.4)",
		"transition: transform .2s",
	})
	b.Add(core.BreakpointDesktop, sel+" details[open] summary::after", []string{"transform: rotate(45deg)"})
	b.Add(core.BreakpointDesktop, sel+" .sky-faq-answer", []string{
		"padding: 0 18px 14px",
		"color: rgba(0,0,0,0.65)",
		"line-height: 1.6",
	})
}

// init 注册常见问题组件。
func init() {
	core.Register(Widget)
}
