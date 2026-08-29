// Package button 实现 core.button 按钮与行动召唤组件（规范《02-C5 按钮与 CTA 组件规范》）。
//
// 设计范式要点：
//   - 单层语义化标签：按动作类型智能编译为 <a>（跳转/原生协议）或 <button>（弹窗/表单触发），
//     杜绝 Elementor 的 wrapper 冗余嵌套；
//   - 统一链接协议：internal（站内路径）/ external（外链+target+rel nofollow/sponsored）/
//     anchor（锚点平滑滚动）/ native（tel://mailto:）/ modal（按钮触发）五种动作，
//     支持 CMS 动态链接绑定；
//   - 文案 + 图标（内置白名单 SVG / 媒体库 SVG 内联）+ 双态外观（normal/hover）为
//     后续卡片/横幅等复合预设复用。
package button

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
)

// Type 组件类型标识。
const Type = "core.button"

// 动作类型。
const (
	ActionInternal = "internal" // 站内路径
	ActionExternal = "external" // 外部 URL
	ActionAnchor   = "anchor"   // 锚点滚动
	ActionNative   = "native"   // tel://mailto:
	ActionModal    = "modal"    // 唤起弹窗（<button>）
	ActionLink     = "link"     // 动态 CMS 链接绑定
)

// 尺寸预设。
const (
	SizeXS = "xs"
	SizeSM = "sm"
	SizeMD = "md"
	SizeLG = "lg"
	SizeXL = "xl"
)

// 变体。
const (
	VariantSolid   = "solid"   // 实色填充
	VariantOutline = "outline" // 轮廓描边（悬停填充）
	VariantGhost   = "ghost"   // 幽灵文本
)

// 尺寸预设表：padding + 基准字号。
var sizePresets = map[string][2]string{
	SizeXS: {"4px 10px", "0.75rem"},
	SizeSM: {"6px 14px", "0.875rem"},
	SizeMD: {"10px 20px", "1rem"},
	SizeLG: {"12px 26px", "1.125rem"},
	SizeXL: {"16px 34px", "1.25rem"},
}

// Arrows/等内置图标（24 viewBox，stroke currentColor，白名单）。
var builtinIcons = map[string]string{
	"arrow-right":   `<path d="M14 5l7 7m0 0l-7 7m7-7H3" stroke-linecap="round"/>`,
	"arrow-left":    `<path d="M10 5l-7 7m0 0l7 7m-7-7h21" stroke-linecap="round"/>`,
	"arrow-up":      `<path d="M19 14l-7-7m0 0l-7 7m7-7v21" stroke-linecap="round"/>`,
	"arrow-down":    `<path d="M19 10l-7 7m0 0l-7-7m7 7V3" stroke-linecap="round"/>`,
	"check":         `<path d="M20 6L9 17l-5-5" stroke-linecap="round" stroke-linejoin="round"/>`,
	"chevron-right": `<path d="M9 6l6 6-6 6" stroke-linecap="round" stroke-linejoin="round"/>`,
	"phone":         `<path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6A19.79 19.79 0 0 1 2.08 4.18 2 2 0 0 1 4.06 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/>`,
	"mail":          `<path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z" stroke-linecap="round"/><path d="M22 6l-10 7L2 6" stroke-linecap="round"/>`,
}

// Icon 图标配置。
type Icon struct {
	// Source 图标源：builtin（内置白名单）/ media（媒体库 SVG，assetId）。
	Source string `json:"source,omitempty" ct:"select,builtin=内置图标,media=媒体库图片,sec=content,label=图标来源"`
	// Name builtin 图标名（source=builtin）。
	Name string `json:"name,omitempty" ct:"select,arrow-right=右箭头,arrow-left=左箭头,arrow-up=上箭头,arrow-down=下箭头,check=对勾,chevron-right=右尖括号,phone=电话,mail=邮件,sec=content,label=图标样式"`
	// AssetID 媒体库 SVG assetId（source=media）。
	AssetID string `json:"assetId,omitempty" ct:"regex,sec=content,label=图标图片" ctRegex:"^[A-Za-z0-9_-]{1,64}$"`
	// Position 位置：prefix（前置）/ suffix（后置）。
	Position string `json:"position,omitempty" ct:"select,prefix=图标在前,suffix=图标在后,sec=content,label=图标位置"`
	// Spacing 图标与文案间距。
	Spacing string `json:"spacing,omitempty" ct:"safe,maxlen=20,sec=style"`
	// HoverShift 悬停时图标水平位移动画值（如 "4px"）。
	HoverShift string `json:"hoverShift,omitempty" ct:"safe,maxlen=20,sec=style"`
	// Size 图标尺寸。
	Size string `json:"size,omitempty" ct:"safe,maxlen=20,sec=style"`
}

// State 正常/悬浮双态外观。
type State struct {
	Background string `json:"background,omitempty" ct:"safe,maxlen=200,sec=style"`
	Color      string `json:"color,omitempty" ct:"safe,maxlen=200,sec=style"`
	Border     string `json:"border,omitempty" ct:"safe,maxlen=200,sec=style"` // 边框颜色
	Shadow     string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=style,label=阴影级别"`
}

// Block 三端块级铺满。
type Block struct {
	Desktop bool `json:"desktop,omitempty"`
	Tablet  bool `json:"tablet,omitempty"`
	Mobile  bool `json:"mobile,omitempty"`
}

// Binding 动态链接绑定。
type Binding struct {
	Field string `json:"field,omitempty"`
}

// Props button 属性。
type Props struct {
	// Text 按钮文本（或绑定）。
	Text string `json:"text,omitempty" ct:"text,maxlen=200,sec=content"`
	// Binding 动态 CMS 链接绑定（如 post.permalink）。
	Binding *Binding `json:"binding,omitempty"`
	// Action 动作类型：internal/external/anchor/native/modal/link（默认 external）。
	Action string `json:"action,omitempty" ct:"select,internal=站内链接,external=外部链接,anchor=页内锚点,native=电话/邮件,modal=弹窗,link=自定义链接,default=external,sec=content,label=点击动作"`
	// Value 动作值：internal 站内路径 / external URL / anchor 元素ID / native tel-mailto / modal 目标ID。
	Value string `json:"value,omitempty" ct:"safe,maxlen=500,sec=content"`
	// Target 外部链接打开方式：self / blank（blank 自动 rel=noopener noreferrer）。
	Target string `json:"target,omitempty" ct:"select,self=当前窗口,blank=新窗口,sec=content,label=打开方式"`
	// Rel SEO 策略：none / nofollow / sponsored。
	Rel string `json:"rel,omitempty" ct:"select,none=默认,nofollow=加 nofollow,sponsored=赞助链接,sec=content,label=链接关系"`

	// --- 文本排版 ---
	FontSize      string `json:"fontSize,omitempty" ct:"safe,maxlen=30,sec=style"`
	FontWeight    string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,800=特粗,sec=style,label=字重"`
	LetterSpacing string `json:"letterSpacing,omitempty" ct:"safe,maxlen=20,sec=style"`
	Transform     string `json:"transform,omitempty" ct:"select,none=无,uppercase=全大写,lowercase=全小写,capitalize=首字母大写,sec=style,label=大小写转换"`
	// Icon 图标配置（可选）。
	Icon *Icon `json:"icon,omitempty"`

	// --- 尺寸/变体/双态外观 ---
	Size    string `json:"size,omitempty" ct:"select,xs=特小,sm=小,md=中,lg=大,xl=特大,default=md,sec=style,label=按钮尺寸"`
	Block   Block  `json:"block,omitempty"`
	Variant string `json:"variant,omitempty" ct:"select,solid,outline,ghost,default=solid,sec=style"`
	Radius  string `json:"radius,omitempty" ct:"select,0,6,8,9999,default=8,sec=style"`
	// Normal 正常态；Hover 悬浮/聚焦态（缺省继承 Normal）。
	Normal State `json:"normal,omitempty"`
	Hover  State `json:"hover,omitempty"`
	// HoverLift 悬浮上浮距离（如 "-2px"）。
	HoverLift string `json:"hoverLift,omitempty" ct:"safe,maxlen=20,sec=style"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		Render:        render,
	},
}

var (
	internalPathRe = regexp.MustCompile(`^/[A-Za-z0-9/-]{0,200}$`)
	anchorIDRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	natualRe       = regexp.MustCompile(`^(tel:|mailto:)[^ \x00-\x20]{3,200}$`)
	fieldPathRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

// validateExtra 关系性校验：动作值/图标/绑定路径。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("必须提供按钮文本或动态链接绑定")
	}
	if p.Binding != nil && p.Binding.Field != "" && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
	}
	if p.Binding != nil && p.Binding.Field != "" && p.Action != ActionLink {
		return fmt.Errorf("动态链接绑定仅支持 link 动作")
	}
	switch p.Action {
	case ActionInternal:
		if p.Value == "" || !internalPathRe.MatchString(p.Value) {
			return fmt.Errorf("内部页面必须提供站内路径（如 /products/xxx）")
		}
	case ActionExternal:
		if p.Value == "" || !isSafeURL(p.Value) {
			return fmt.Errorf("外部链接非法: %q", p.Value)
		}
	case ActionAnchor:
		if !anchorIDRe.MatchString(p.Value) {
			return fmt.Errorf("锚点必须提供合法元素 ID: %q", p.Value)
		}
	case ActionNative:
		if !natualRe.MatchString(p.Value) {
			return fmt.Errorf("原生动作仅支持 tel:/mailto: 协议: %q", p.Value)
		}
	case ActionModal:
		if !anchorIDRe.MatchString(p.Value) {
			return fmt.Errorf("弹窗绑定必须提供目标组件 ID: %q", p.Value)
		}
	case ActionLink:
		// 绑定场景，无需 Value。
	default:
		return fmt.Errorf("无效的动作类型: %q", p.Action)
	}
	if p.Icon != nil {
		if p.Icon.Source == "builtin" {
			if _, ok := builtinIcons[p.Icon.Name]; !ok {
				return fmt.Errorf("无效的内置图标: %q（白名单见规范）", p.Icon.Name)
			}
		} else if p.Icon.Source == "media" {
			if !assetIDRe.MatchString(p.Icon.AssetID) {
				return fmt.Errorf("无效的图标 assetId: %q", p.Icon.AssetID)
			}
		} else {
			return fmt.Errorf("无效的图标源: %q", p.Icon.Source)
		}
		if p.Icon.Position != "" && p.Icon.Position != "prefix" && p.Icon.Position != "suffix" {
			return fmt.Errorf("图标位置仅支持 prefix/suffix: %q", p.Icon.Position)
		}
	}
	return nil
}

// assetIDRe 资产白名单。
var assetIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)

// render 标签选择（a/button）+ 链接协议 → 文案 + 图标 + CSS 编译。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	// 图标 HTML。
	iconHTML, err := renderIcon(p, h)
	if err != nil {
		return "", err
	}

	// 标签与属性选择：modal → <button>；其余 → <a href>。
	tag := "a"
	var attrsStr string
	switch p.Action {
	case ActionModal:
		tag = "button"
		attrsStr = ` type="button" data-modal-target="` + html.EscapeString(p.Value) + `"`
	case ActionLink:
		// 动态链接绑定。
		if h.Content == nil {
			return "", fmt.Errorf("编译上下文缺少内容解析器，无法解析动态链接")
		}
		v, err := h.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v == "" {
			return "", fmt.Errorf("绑定字段 %q 为空，无 fallback 兜底", p.Binding.Field)
		}
		attrsStr = ` href="` + html.EscapeString(v) + `"`
	case ActionAnchor:
		attrsStr = ` href="#` + html.EscapeString(p.Value) + `"`
	case ActionNative, ActionInternal:
		attrsStr = ` href="` + html.EscapeString(p.Value) + `"`
	default: // external
		attrsStr = ` href="` + html.EscapeString(p.Value) + `"`
		relParts := []string{}
		if p.Target == "blank" {
			attrsStr += ` target="_blank"`
			relParts = append(relParts, "noopener", "noreferrer")
		}
		if p.Rel == "nofollow" || p.Rel == "sponsored" {
			relParts = append(relParts, p.Rel)
		}
		if len(relParts) > 0 {
			attrsStr += ` rel="` + strings.Join(relParts, " ") + `"`
		}
	}

	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(tag)
	sb.WriteString(` class="`)
	sb.WriteString(h.Classes)
	sb.WriteString(`"`)
	if h.CustomID != "" {
		sb.WriteString(` id="`)
		sb.WriteString(h.CustomID)
		sb.WriteString(`"`)
	}
	sb.WriteString(attrsStr)
	sb.WriteString(">")

	// 前缀图标 + 文案 + 后缀图标。
	if p.Icon != nil && p.Icon.Position != "suffix" {
		sb.WriteString(iconHTML)
	}
	sb.WriteString(`<span class="bt-text">`)
	sb.WriteString(html.EscapeString(p.Text))
	sb.WriteString(`</span>`)
	if p.Icon != nil && p.Icon.Position == "suffix" {
		sb.WriteString(iconHTML)
	}
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteString(">")

	compileCSS(node.ID, p, h.CSS)
	return sb.String(), nil
}

// renderIcon 图标输出：内置 SVG 或媒体库 SVG 内联。
func renderIcon(p *Props, h *core.AtomRender) (string, error) {
	if p.Icon == nil {
		return "", nil
	}
	size := p.Icon.Size
	if size == "" {
		size = "1em"
	}
	var inner string
	if p.Icon.Source == "builtin" {
		path, ok := builtinIcons[p.Icon.Name]
		if !ok {
			return "", fmt.Errorf("无效的内置图标: %q", p.Icon.Name)
		}
		inner = path
	} else {
		if h.Media == nil {
			return "", fmt.Errorf("编译上下文缺少媒体解析器，无法解析图标 assetId")
		}
		meta, err := h.Media.ResolveMedia(p.Icon.AssetID, media.VariantOriginal)
		if err != nil {
			return "", fmt.Errorf("图标解析失败: %w", err)
		}
		if meta.SrcHTML == "" {
			// 非内联源：img 直引。
			return `<img class="bt-icon" src="` + html.EscapeString(meta.URL) + `" alt="" style="width:` + html.EscapeString(size) + `;height:` + html.EscapeString(size) + `">`, nil
		}
		inner = meta.SrcHTML
	}
	class := "bt-icon"
	if p.Icon.HoverShift != "" {
		class += " bt-icon-shift"
	}
	return `<svg class="` + class + `" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="` + html.EscapeString(size) + `" height="` + html.EscapeString(size) + `" aria-hidden="true">` + inner + `</svg>`, nil
}

// compileCSS 按钮样式：尺寸/变体/双态/图标动效/块级。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var base []string
	base = append(base,
		"display: inline-flex",
		"align-items: center",
		"justify-content: center",
		"text-decoration: none",
		"cursor: pointer",
		"border: none",
	)
	if preset, ok := sizePresets[p.Size]; ok {
		base = append(base, "padding: "+preset[0], "font-size: "+preset[1])
	} else {
		base = append(base, "padding: 10px 20px", "font-size: 1rem")
	}
	if p.FontSize != "" {
		base = append(base, "font-size: "+p.FontSize)
	}
	if p.FontWeight != "" {
		base = append(base, "font-weight: "+p.FontWeight)
	}
	if p.LetterSpacing != "" {
		base = append(base, "letter-spacing: "+p.LetterSpacing)
	}
	if p.Transform != "" && p.Transform != "none" {
		base = append(base, "text-transform: "+p.Transform)
	}
	switch p.Radius {
	case "0":
		base = append(base, "border-radius: 0")
	case "9999":
		base = append(base, "border-radius: 9999px")
	default:
		base = append(base, "border-radius: "+p.Radius+"px")
	}

	// 图标间距。
	if p.Icon != nil && p.Icon.Spacing != "" {
		gap := p.Icon.Spacing
		base = append(base, "gap: "+gap)
	}

	// 变体基础。
	switch p.Variant {
	case VariantGhost:
		base = append(base, "background: transparent")
		if p.Text == "" {
			base = append(base, "padding: 6px 0")
		}
	case VariantOutline:
		base = append(base, "background: transparent")
		if p.Normal.Border != "" {
			base = append(base, "border: 1px solid "+p.Normal.Border)
		} else {
			base = append(base, "border: 1px solid currentColor")
		}
		if p.Normal.Color != "" {
			base = append(base, "color: "+p.Normal.Color)
		}
	default: // solid
		if p.Normal.Background != "" {
			base = append(base, "background: "+p.Normal.Background)
		}
		if p.Normal.Color != "" {
			base = append(base, "color: "+p.Normal.Color)
		}
		if p.Normal.Border != "" {
			base = append(base, "border: 1px solid "+p.Normal.Border)
		}
	}
	if v, ok := core.ShadowPresets[p.Normal.Shadow]; ok {
		base = append(base, "box-shadow: "+v)
	}
	b.Add(core.BreakpointDesktop, sel, base)

	// 悬浮/聚焦态。
	var hoverDecls []string
	hoverBase := p.Hover
	switch p.Variant {
	case VariantOutline:
		if hoverBase.Background == "" {
			hoverBase.Background = p.Normal.Color
		}
	default:
		if hoverBase.Background == "" {
			hoverBase.Background = p.Normal.Background
		}
	}
	if hoverBase.Background != "" {
		hoverDecls = append(hoverDecls, "background: "+hoverBase.Background)
	}
	if hoverBase.Color != "" {
		hoverDecls = append(hoverDecls, "color: "+hoverBase.Color)
	}
	if hoverBase.Border != "" {
		hoverDecls = append(hoverDecls, "border-color: "+hoverBase.Border)
	}
	if hoverBase.Shadow != "" {
		if v, ok := core.ShadowPresets[hoverBase.Shadow]; ok {
			hoverDecls = append(hoverDecls, "box-shadow: "+v)
		}
	}
	if p.HoverLift != "" {
		hoverDecls = append(hoverDecls, "transform: translateY("+p.HoverLift+")")
	}
	if len(hoverDecls) > 0 {
		b.Add(core.BreakpointDesktop, sel, []string{"transition: all 0.2s ease"})
		b.Add(core.BreakpointDesktop, sel+":hover, "+sel+":focus", hoverDecls)
	}

	// 图标悬停位移。
	if p.Icon != nil && p.Icon.HoverShift != "" {
		b.Add(core.BreakpointDesktop, sel+" .bt-icon-shift", []string{"transition: transform 0.2s ease"})
		b.Add(core.BreakpointDesktop, sel+":hover .bt-icon-shift", []string{"transform: translateX(" + p.Icon.HoverShift + ")"})
	}

	// 块级铺满（三端）。
	if p.Block.Desktop {
		b.Add(core.BreakpointDesktop, sel, []string{"display: flex", "width: 100%"})
	}
	if p.Block.Tablet {
		b.Add(core.BreakpointTablet, sel, []string{"display: flex", "width: 100%"})
	}
	if p.Block.Mobile {
		b.Add(core.BreakpointMobile, sel, []string{"display: flex", "width: 100%"})
	}
}

// isSafeURL 外链白名单（与 url 控件同规则）。
func isSafeURL(s string) bool {
	if len(s) > 500 {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("./:?=&%~#+_@-", r)) {
			return false
		}
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// init 注册按钮组件。
func init() {
	core.Register(Widget)
}
