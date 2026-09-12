// Package quote 实现 core.quote 引用组件（对标 GrapesJS Quote/Blockquote 组件生态）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：
// 引用内容 + 可选作者（可带出处链接）+ 对齐方式，编译期 CSS 生成引用样式，零客户端 JS。
package quote

import (
	_ "embed" // quote.css 经 //go:embed 打进二进制
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.quote"

// 对齐方式。
const (
	AlignLeft   = "left"   // 左对齐（默认）
	AlignCenter = "center" // 居中
)

// Props core.quote 引用属性。
type Props struct {
	// Text 引用内容（富文本 HTML 片段，构建期白名单清洗；存量纯文本转义后按段落包装）。
	Text string `json:"text,omitempty" ct:"richtext,maxlen=1000,sec=content,label=引用内容"`
	// Author 作者（非空输出 <cite>）。
	Author string `json:"author,omitempty" ct:"text,maxlen=100,sec=content,label=作者"`
	// Source 出处链接（Author 非空时包裹 <a href>）。
	Source string `json:"source,omitempty" ct:"text,maxlen=500,sec=content,label=出处链接"`
	// Align 对齐：left / center（默认 left）。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,sec=style,label=对齐"`
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
		Translatable: []string{"text", "author"},
	},
}

// validateExtra 关系性校验：引用内容非空、出处链接协议白名单。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" {
		return fmt.Errorf("必须提供引用内容")
	}
	if p.Source != "" && !core.IsSafeURL(p.Source) {
		return fmt.Errorf("出处链接协议非法: %q", p.Source)
	}
	return nil
}

// quoteCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed quote.css
var quoteCSS string

// compileCSS 引用样式：左边框 + 斜体 + 对齐 + cite。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"center": core.BoolVar(p.Align == AlignCenter)}
	if err := core.ApplyComponentCSSTmpl(b, sel, quoteCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("quote 组件样式解析失败: %v", err))
	}
}

// init 注册引用组件。
func init() {
	core.Register(Widget)
}
