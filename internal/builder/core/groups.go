package core

import (
	"fmt"
	"regexp"
)

// 共享排版组（对标 Elementor Group_Control_Typography，但声明式实现）。
// heading/text 等文本类组件嵌入 TextStyle，使用同一份校验与 CSS 生成，
// 消除组件间重复定义。组内字段（fontSize/lineHeight/textAlign）为组件通用
// 组合件，专有字段（如 heading 的字重/转换）仍留在组件自己的 Props。
type TextStyle struct {
	Desktop TextStyleValue `json:"desktop,omitempty"`
	Tablet  TextStyleValue `json:"tablet,omitempty"`
	Mobile  TextStyleValue `json:"mobile,omitempty"`
}

// TextStyleValue 单端排版值。
type TextStyleValue struct {
	// FontSize 字号：px/rem/em/vw 或 clamp() 流式字号。
	FontSize string `json:"fontSize,omitempty"`
	// LineHeight 行高：倍数（如 1.2）或长度值。
	LineHeight string `json:"lineHeight,omitempty"`
	// TextAlign 文字对齐：left/center/right/justify。
	TextAlign string `json:"textAlign,omitempty"`
}

// 共享排版白名单。
var (
	typographyLenRe    = regexp.MustCompile(`^[0-9.]+(px|rem|em|vw|%)?$`)
	typographyClampRe  = regexp.MustCompile(`^clamp\([0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw)\)$`)
	typographyAlignMap = map[string]bool{"left": true, "center": true, "right": true, "justify": true}
)

// ValidateTextStyle 校验三端排版值（共享组校验，多组件复用）。
func ValidateTextStyle(nodeID string, ts *TextStyle) (err error) {
	for bp, v := range map[string]TextStyleValue{
		"desktop": ts.Desktop, "tablet": ts.Tablet, "mobile": ts.Mobile,
	} {
		if v.FontSize != "" && !isTypographyValue(v.FontSize) {
			return fmt.Errorf("节点 %s: 无效的 %s 端字号: %q", nodeID, bp, v.FontSize)
		}
		if v.LineHeight != "" && !isTypographyValue(v.LineHeight) {
			return fmt.Errorf("节点 %s: 无效的 %s 端行高: %q", nodeID, bp, v.LineHeight)
		}
		if v.TextAlign != "" && !typographyAlignMap[v.TextAlign] {
			return fmt.Errorf("节点 %s: 无效的 %s 端对齐: %q", nodeID, bp, v.TextAlign)
		}
	}
	return nil
}

// isTypographyValue 长度值或 clamp() 流式字号白名单。
func isTypographyValue(v string) bool {
	return len(v) <= 40 && (typographyLenRe.MatchString(v) || typographyClampRe.MatchString(v))
}

// Decls 单端排版值 → CSS 声明列表（组件 compileCSS 直接 append，共享生成逻辑）。
func (v TextStyleValue) Decls() []string {
	var decls []string
	if v.FontSize != "" {
		decls = append(decls, "font-size: "+v.FontSize)
	}
	if v.LineHeight != "" {
		decls = append(decls, "line-height: "+v.LineHeight)
	}
	if v.TextAlign != "" {
		decls = append(decls, "text-align: "+v.TextAlign)
	}
	return decls
}

// BreakpointDecls 按断点取声明列表（桌面/平板/手机，供三端 bucket 拼接）。
func (ts TextStyle) BreakpointDecls(bp string) []string {
	switch bp {
	case BreakpointDesktop:
		return ts.Desktop.Decls()
	case BreakpointTablet:
		return ts.Tablet.Decls()
	case BreakpointMobile:
		return ts.Mobile.Decls()
	}
	return nil
}

// ---------- 共享交互/动效组（docs/04 §1.1，0-D 前置） ----------

// InteractionProps 交互状态与动效（全组件共享组，效果基本库）。
// 嵌入 AdvancedProps.Interaction 后所有组件自动获得动效能力。
type InteractionProps struct {
	// Entrance 入场动效："" 关闭 / fade-in / fade-up / fade-down / fade-left /
	// fade-right / zoom-in / zoom-out / slide-up / slide-down / slide-left /
	// slide-right / flip-x / flip-y / blur-in / bounce-in / rotate-in。
	Entrance string `json:"entrance,omitempty"`
	// EntranceDelay 入场延迟（秒，0~3，一位小数），编排多元素先后入场。
	EntranceDelay float64 `json:"entranceDelay,omitempty"`
	// EntranceDuration 入场时长档位：fast(0.3s) / normal(0.6s，默认) / slow(1s)。
	EntranceDuration string `json:"entranceDuration,omitempty"`
	// ScrollReveal 滚动到视口时触发入场（CSS animation-timeline: view()，
	// 零 JS；旧浏览器降级为直接入场）。"" 关闭 / reveal。
	ScrollReveal string `json:"scrollReveal,omitempty"`
	// HoverEffect 悬浮效果："" 无 / lift 上浮 / scale 放大 / glow 发光 /
	// shadow 阴影加深 / underline 下划线生长。
	HoverEffect string `json:"hoverEffect,omitempty"`
	// LoopEffect 循环动画："" 无 / pulse 脉冲 / float 漂浮 / glow 呼吸发光 /
	// spin 旋转（装饰元素用，内容区慎用）。
	LoopEffect string `json:"loopEffect,omitempty"`
	// HoverLift 悬浮上浮（兼容旧字段，等效 HoverEffect=lift）。
	HoverLift bool `json:"hoverLift,omitempty"`
	// Sticky 滚动吸顶定位。
	Sticky bool `json:"sticky,omitempty"`
	// StickyTop 吸顶偏移（CSS 长度值），默认 0。
	StickyTop string `json:"stickyTop,omitempty"`
}

// 动效枚举白名单（效果基本库词汇表）。
var (
	allowedEntrance = map[string]bool{
		"": true, "fade-in": true, "fade-up": true, "fade-down": true,
		"fade-left": true, "fade-right": true,
		"zoom-in": true, "zoom-out": true,
		"slide-up": true, "slide-down": true, "slide-left": true, "slide-right": true,
		"flip-x": true, "flip-y": true, "blur-in": true, "bounce-in": true, "rotate-in": true,
	}
	allowedEntranceDuration = map[string]bool{"": true, "fast": true, "normal": true, "slow": true}
	allowedScrollReveal     = map[string]bool{"": true, "reveal": true}
	allowedHoverEffect      = map[string]bool{"": true, "lift": true, "scale": true, "glow": true, "shadow": true, "underline": true}
	allowedLoopEffect       = map[string]bool{"": true, "pulse": true, "float": true, "glow": true, "spin": true}
)

// entranceDurationCSS 时长档位 → CSS 值。
func entranceDurationCSS(d string) string {
	switch d {
	case "fast":
		return "0.3s"
	case "slow":
		return "1s"
	default:
		return "0.6s"
	}
}

// ValidateInteraction 交互组校验（效果基本库白名单 + 限幅）。
func ValidateInteraction(p InteractionProps) (err error) {
	if !allowedEntrance[p.Entrance] {
		return fmt.Errorf("无效的入场动效: %q", p.Entrance)
	}
	if p.EntranceDelay < 0 || p.EntranceDelay > 3 {
		return fmt.Errorf("入场延迟 %.1fs 超限（0~3）", p.EntranceDelay)
	}
	if !allowedEntranceDuration[p.EntranceDuration] {
		return fmt.Errorf("无效的入场时长档位: %q（fast/normal/slow）", p.EntranceDuration)
	}
	if !allowedScrollReveal[p.ScrollReveal] {
		return fmt.Errorf("无效的滚动触发: %q", p.ScrollReveal)
	}
	if !allowedHoverEffect[p.HoverEffect] {
		return fmt.Errorf("无效的悬浮效果: %q（lift/scale/glow/shadow/underline）", p.HoverEffect)
	}
	if !allowedLoopEffect[p.LoopEffect] {
		return fmt.Errorf("无效的循环动画: %q（pulse/float/glow/spin）", p.LoopEffect)
	}
	return nil
}

// CompileInteraction 编译交互/动效声明进 CSS 桶（Advanced 管线调用）。
// entrance 用 backwards（动画结束释放终态，避免压制 hover transform）；
// scrollReveal 用 animation-timeline: view()（滚动到视口触发，零 JS）；
// hoverEffect 输出过渡声明 + :hover 触发态；loopEffect 独立 animation 声明。
func CompileInteraction(sel string, p InteractionProps, b *CSSBuckets) {
	// 兼容：HoverLift 等效 HoverEffect=lift。
	hover := p.HoverEffect
	if p.HoverLift && hover == "" {
		hover = "lift"
	}
	var decls []string
	// 入场动效（含时长/延迟/滚动触发）。
	if p.Entrance != "" {
		dur := entranceDurationCSS(p.EntranceDuration)
		anim := fmt.Sprintf("animation: wp-%s %s ease backwards", p.Entrance, dur)
		if p.EntranceDelay > 0 {
			anim = fmt.Sprintf("animation: wp-%s %s ease %.1fs backwards", p.Entrance, dur, p.EntranceDelay)
		}
		decls = append(decls, anim)
		b.NeedKeyframes("wp-" + p.Entrance)
		// 滚动触发：视口进入时播放（现代浏览器；旧浏览器不识别 timeline 即直接入场）。
		if p.ScrollReveal == "reveal" {
			decls = append(decls,
				"animation-timeline: view()",
				"animation-range: entry 0% entry 60%")
		}
	}
	// 循环动画（attention；与入场互不冲突——loop 走独立 animation 名，
	// 同元素多动画用逗号并接）。
	if p.LoopEffect != "" {
		loop := fmt.Sprintf("animation: wp-loop-%s 2.4s ease-in-out infinite", p.LoopEffect)
		if p.Entrance != "" {
			// 已有入场动画：并接（入场结束后循环接管）。
			for i, d := range decls {
				if len(d) > 10 && d[:10] == "animation:" {
					decls[i] = d + ", " + loop[len("animation: "):]
				}
			}
		} else {
			decls = append(decls, loop)
		}
		b.NeedKeyframes("wp-loop-" + p.LoopEffect)
	}
	// 悬浮过渡声明。
	if hover != "" && hover != "underline" {
		decls = append(decls, "transition: transform 0.25s ease, box-shadow 0.25s ease")
	}
	if len(decls) > 0 {
		b.Add(BreakpointDesktop, sel, decls)
	}
	// 悬浮触发态。
	if hover != "" {
		hoverDecls := hoverTriggerDecls(hover)
		if len(hoverDecls) > 0 {
			b.Add(BreakpointDesktop, sel+":hover", hoverDecls)
		}
		// underline 需要基础声明（伪元素线宽 0 → hover 100%）。
		if hover == "underline" {
			b.Add(BreakpointDesktop, sel+"::after", []string{
				"content: ''", "display: block", "height: 2px",
				"width: 0", "background: currentColor",
				"transition: width 0.3s ease",
			})
			b.Add(BreakpointDesktop, sel+":hover::after", []string{"width: 100%"})
		}
	}
}

// hoverTriggerDecls 悬浮效果 → :hover 触发态声明。
func hoverTriggerDecls(effect string) []string {
	switch effect {
	case "lift":
		return []string{"transform: translateY(-6px)", "box-shadow: 0 12px 24px rgba(0,0,0,.12)"}
	case "scale":
		return []string{"transform: scale(1.04)"}
	case "glow":
		return []string{"box-shadow: 0 0 0 3px rgba(59,130,246,.35), 0 0 24px rgba(59,130,246,.25)"}
	case "shadow":
		return []string{"box-shadow: 0 16px 40px rgba(0,0,0,.18)"}
	case "underline":
		return nil // ::after 处理
	}
	return nil
}

// InteractionControlFields 面板字段键（workbench 检查器「动效」分组渲染用）。
var InteractionControlFields = []string{"interaction.entrance", "interaction.entranceDelay", "interaction.hoverLift"}
