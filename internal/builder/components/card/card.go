// Package card 实现 core.card 卡片组件（对标 GrapesJS Card 组件生态）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：
// 标题 + 正文 + 可选图片 + 可选按钮（链接），编译期 CSS 生成卡片布局，零客户端 JS。
package card

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.card"

// Props core.card 卡片属性。
type Props struct {
	// Title 标题（非空输出 <h3>）。
	Title string `json:"title,omitempty" ct:"text,maxlen=200,sec=content,label=标题"`
	// Text 正文（富文本 HTML 片段，构建期白名单清洗；存量纯文本转义后按段落包装）。
	Text string `json:"text,omitempty" ct:"richtext,maxlen=1000,sec=content,label=正文"`
	// ImageSrc 顶部图片（媒体库选择回填 URL 或外部绝对 URL）。
	ImageSrc string `json:"imageSrc,omitempty" ct:"media,maxlen=500,sec=content,label=图片"`
	// ButtonText 按钮文字（非空且 ButtonLink 非空时输出 <a>）。
	ButtonText string `json:"buttonText,omitempty" ct:"text,maxlen=50,sec=content,label=按钮文字"`
	// ButtonLink 按钮链接（href）。
	ButtonLink string `json:"buttonLink,omitempty" ct:"text,maxlen=500,sec=content,label=按钮链接"`
	// Loading 图片加载策略三态：空=默认（继承主题「图片管理」）/ on=开启懒加载 / off=关闭。
	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`
	// FetchPriority 资源提示优先级：空=auto（不输出属性）/ high=首屏优先 / low=次要。
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：按钮链接协议白名单。
// ButtonLink 使用 text 控件（编辑期自由输入），协议安全在此统一收紧。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.ButtonLink != "" && !core.IsSafeURL(p.ButtonLink) {
		return fmt.Errorf("按钮链接协议非法: %q", p.ButtonLink)
	}
	return nil
}

// compileCSS 卡片样式：布局 + 图片 + 标题 + 正文 + 按钮。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex",
		"flex-direction: column",
		"overflow: hidden",
		"background: var(--wp-c-surface, #fff)",
		"border: 1px solid rgba(0,0,0,0.1)",
		"border-radius: 12px",
		"padding: 16px",
	})
	b.Add(core.BreakpointDesktop, sel+" img", []string{
		"width: 100%",
		"display: block",
		"border-radius: 8px",
		"margin-bottom: 12px",
		"object-fit: cover",
	})
	b.Add(core.BreakpointDesktop, sel+" h3", []string{
		"margin: 0 0 8px",
		"font-size: 18px",
		"line-height: 1.4",
	})
	b.Add(core.BreakpointDesktop, sel+" p", []string{
		"margin: 0",
		"color: rgba(0,0,0,0.65)",
		"line-height: 1.6",
	})
	b.Add(core.BreakpointDesktop, sel+" a.wp-card-btn", []string{
		"margin-top: 16px",
		"align-self: flex-start",
		"display: inline-block",
		"padding: 8px 16px",
		"border-radius: 6px",
		"background: var(--wp-btn-bg, var(--wp-c-primary, #2563eb))",
		"color: #fff",
		"text-decoration: none",
	})
}

// init 注册卡片组件。
func init() {
	core.Register(Widget)
}
