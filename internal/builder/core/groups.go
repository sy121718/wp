package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TextStyle 三端文字排版共享组（标题/文本等排版组件复用）。
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
	// fade-right / zoom-in / zoom-out / slide-* / flip-x / flip-y / blur-in /
	// bounce-in / rotate-in / back-in-* / bounce-in-* / fade-in-* 角向 /
	// light-speed-in-* / roll-in / jack-in-the-box / zoom-in-* 方向 / rotate-in-* 方向。
	Entrance string `json:"entrance,omitempty"`
	// EntranceDelay 入场延迟（秒，0~3，一位小数），编排多元素先后入场。
	EntranceDelay float64 `json:"entranceDelay,omitempty"`
	// EntranceDuration 入场时长档位：fast(0.3s) / normal(0.6s，默认) / slow(1s)。
	EntranceDuration string `json:"entranceDuration,omitempty"`
	// EntranceEasing 入场缓动："" 弹簧（默认，linear() 采样，Apple 式过冲回弹，
	// 老浏览器回退 ease）/ soft 柔和弹簧 / bouncy 弹跳弹簧 / classic 经典 ease。
	EntranceEasing string `json:"entranceEasing,omitempty"`
	// ScrollReveal 滚动到视口时触发入场（CSS animation-timeline: view()，
	// 零 JS；旧浏览器降级为直接入场）。"" 关闭 / reveal。
	ScrollReveal string `json:"scrollReveal,omitempty"`
	// HoverEffect 悬浮效果："" 无 / lift 上浮 / scale 放大 / glow 发光 /
	// shadow 阴影加深 / underline 下划线生长。
	HoverEffect string `json:"hoverEffect,omitempty"`
	// LoopEffect 循环动画："" 无 / pulse 脉冲 / float 漂浮 / drift 横向漂移 /
	// shake 震动 / jello 果冻 / heartbeat 心跳 / blob 液态形变 /
	// flash 闪烁 / rubber-band 橡皮筋 / swing 摇摆 / tada 挥舞 / wobble 摇晃 /
	// head-shake 摇头 / bounce 弹跳 / glow 呼吸发光 / spin 旋转（装饰元素用，内容区慎用）。
	LoopEffect string `json:"loopEffect,omitempty"`
	// ScrollStory 滚动叙事（view() 进度连续绑定：滚动多少动画走多少，可逆跟手；
	// Apple 产品页式叙事）：zoom 放大 / rise 上滑 / fade 渐显。独占 animation
	// 声明，与 Entrance / ScrollReveal / LoopEffect 互斥。旧浏览器降级为静态终态。
	ScrollStory string `json:"scrollStory,omitempty"`
	// ActiveEffect 按压反馈（:active 触发）："" 无 / press 按下缩小 / sink 按下下沉 /
	// pop 按下放大 / glow 按下发光。
	//
	// 与 HoverEffect 的区别是触发时机：:active 在触屏上同样生效（手指按下即触发），
	// 是移动端唯一可靠的按下反馈 —— 所以它**不**走 @media (hover: hover) 包裹。
	ActiveEffect string `json:"activeEffect,omitempty" ct:"select,=无,press=按下缩小,sink=按下下沉,pop=按下放大,glow=按下发光,sec=motion,label=按压反馈"`
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
		// Animate.css 拆解扩充（keyframes_animate.go）：
		"back-in-up": true, "back-in-down": true, "back-in-left": true, "back-in-right": true,
		"bounce-in-down": true, "bounce-in-up": true, "bounce-in-left": true, "bounce-in-right": true,
		"fade-in-top-left": true, "fade-in-top-right": true,
		"fade-in-bottom-left": true, "fade-in-bottom-right": true,
		"light-speed-in-left": true, "light-speed-in-right": true,
		"roll-in": true, "jack-in-the-box": true,
		"zoom-in-down": true, "zoom-in-up": true, "zoom-in-left": true, "zoom-in-right": true,
		"rotate-in-down-left": true, "rotate-in-down-right": true,
		"rotate-in-up-left": true, "rotate-in-up-right": true,
		"flip-in-x": true, "flip-in-y": true,
	}
	allowedEntranceDuration = map[string]bool{"": true, "fast": true, "normal": true, "slow": true}
	allowedEntranceEasing   = map[string]bool{"": true, "spring": true, "soft": true, "bouncy": true, "classic": true}
	allowedScrollReveal     = map[string]bool{"": true, "reveal": true}
	allowedScrollStory      = map[string]bool{"": true, "zoom": true, "rise": true, "fade": true}
	allowedHoverEffect      = map[string]bool{"": true, "lift": true, "scale": true, "glow": true, "shadow": true, "underline": true, "shine": true,
		"sink": true, "grow": true, "border-glow": true, "text-glow": true, "skew": true,
		"img-zoom": true, "img-zoom-out": true, "img-gray": true, "img-blur": true, "img-bright": true, "img-sepia": true, "img-rotate": true, "img-flip": true,
		"img-hue": true}
	allowedLoopEffect   = map[string]bool{"": true, "pulse": true, "float": true, "drift": true, "shake": true, "jello": true, "heartbeat": true, "blob": true, "flash": true, "rubber-band": true, "swing": true, "tada": true, "wobble": true, "head-shake": true, "bounce": true, "glow": true, "spin": true}
	allowedActiveEffect = map[string]bool{"": true, "press": true, "sink": true, "pop": true, "glow": true}
)

// timingOverride 组合缓动覆盖声明：存在并接的循环动画时给出两个值
// （入场用弹簧曲线、循环保持 ease-in-out），避免单值扩展到全部动画。
func timingOverride(spring string, withLoop bool) string {
	if withLoop {
		return "animation-timing-function: " + spring + ", ease-in-out"
	}
	return "animation-timing-function: " + spring
}

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

// effectKind 交互动效的类别。三类各自独立命名空间，关键帧名互不占用。
//
// 与悬浮 / 按压效果的区别：那两类走过渡与滤镜（transition / filter），
// **不产生关键帧**，所以不在这里，也没有前缀。
type EffectKind string

// 三个动效类别。跨包使用（组件把属性值映射成词汇名时），故导出。
const (
	KindEntrance EffectKind = "entrance"
	KindLoop     EffectKind = "loop"
	KindStory    EffectKind = "story"
)

// effectKindPrefix 类别 → 关键帧名前缀。
//
// **这是「名字 → 关键帧」的唯一规则**：白名单校验、关键帧激活（NeedKeyframes）、
// animation 声明全部经 effectKeyframeName 取名字，没有任何地方再手拼前缀。
// 前缀散落在多处时，改一次命名空间要翻遍编译逻辑，漏掉一处就是产物里
// 引用一个不存在的动画 —— 页面上只表现为「不动」，没有任何报错。
var effectKindPrefix = map[EffectKind]string{
	KindEntrance: "sky-",
	KindLoop:     "sky-loop-",
	KindStory:    "sky-story-",
}

// effectNames 类别 → 该类别允许的名字（即三张白名单；空串＝不启用，处处合法）。
//
// 校验与拼名共用它，于是不会出现「校验放行的名字、编译时拼不出来」这种错位。
var effectNames = map[EffectKind]map[string]bool{
	KindEntrance: allowedEntrance,
	KindLoop:     allowedLoopEffect,
	KindStory:    allowedScrollStory,
}

// EffectKeyframeName 交互动效词汇对应的关键帧名；名字为空返回空串。
//
// 不做白名单判断：校验与编译各自决定要不要拒绝非法名字（编译路径由 Validate 先把关）。
// 导出是为了让「把属性值映射成词汇名」的组件（cardstack 的逐卡错落效果）也不必
// 手写完整关键帧名 —— 前缀只有一处定义，改命名空间时不会有人掉队。
func EffectKeyframeName(kind EffectKind, name string) string {
	if name == "" {
		return ""
	}
	return effectKindPrefix[kind] + name
}

// EffectAllowed 名字是否在该类别的词汇表里（空串恒为真 —— 它表示不启用）。
func EffectAllowed(kind EffectKind, name string) bool {
	allowed, ok := effectNames[kind]
	if !ok {
		return false
	}
	return allowed[name]
}

// ValidateInteraction 交互组校验（效果基本库白名单 + 限幅）。
// interactionFieldChecks 字段级校验表（表驱动：新增动效字段只需加一行校验器，
// 返回非空字符串 = 校验失败详情）。文案与词表由各自白名单维护。
var interactionFieldChecks = []func(p InteractionProps) string{
	func(p InteractionProps) string {
		if !EffectAllowed(KindEntrance, p.Entrance) {
			return fmt.Sprintf("无效的入场动效: %q", p.Entrance)
		}
		return ""
	},
	func(p InteractionProps) string {
		if p.EntranceDelay < 0 || p.EntranceDelay > 3 {
			return fmt.Sprintf("入场延迟 %.1fs 超限（0~3）", p.EntranceDelay)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !allowedEntranceDuration[p.EntranceDuration] {
			return fmt.Sprintf("无效的入场时长档位: %q（fast/normal/slow）", p.EntranceDuration)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !allowedEntranceEasing[p.EntranceEasing] {
			return fmt.Sprintf("无效的入场缓动: %q（spring/soft/bouncy/classic）", p.EntranceEasing)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !allowedScrollReveal[p.ScrollReveal] {
			return fmt.Sprintf("无效的滚动触发: %q", p.ScrollReveal)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !EffectAllowed(KindStory, p.ScrollStory) {
			return fmt.Sprintf("无效的滚动叙事: %q（zoom/rise/fade）", p.ScrollStory)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !allowedHoverEffect[p.HoverEffect] {
			return fmt.Sprintf("无效的悬浮效果: %q（lift/scale/glow/shadow/underline）", p.HoverEffect)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !allowedActiveEffect[p.ActiveEffect] {
			return fmt.Sprintf("无效的按压反馈: %q（press/sink/pop/glow）", p.ActiveEffect)
		}
		return ""
	},
	func(p InteractionProps) string {
		if !EffectAllowed(KindLoop, p.LoopEffect) {
			return fmt.Sprintf("无效的循环动画: %q（pulse/float/glow/spin）", p.LoopEffect)
		}
		return ""
	},
	func(p InteractionProps) string {
		if p.StickyTop != "" && !IsSafeCSSValue(p.StickyTop) {
			return fmt.Sprintf("无效的吸顶偏移: %q", p.StickyTop)
		}
		return ""
	},
}

// ValidateInteraction 交互组校验（字段级表驱动 + 关系性互斥规则）。
func ValidateInteraction(p InteractionProps) (err error) {
	for _, check := range interactionFieldChecks {
		if msg := check(p); msg != "" {
			return errors.New(msg)
		}
	}
	return validateInteractionRelations(p)
}

// validateInteractionRelations 关系性校验（跨字段互斥）：滚动叙事独占 animation 声明，
// 与入场/滚动触发/循环同时设置会互相覆盖，直接拒绝。
func validateInteractionRelations(p InteractionProps) (err error) {
	if p.ScrollStory == "" {
		return nil
	}
	switch {
	case p.Entrance != "":
		return errors.New("滚动叙事与入场动效互斥，请只选一种")
	case p.ScrollReveal != "":
		return errors.New("滚动叙事与滚动触发互斥，请只选一种")
	case p.LoopEffect != "":
		return errors.New("滚动叙事与循环动画互斥，请只选一种")
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
		kf := EffectKeyframeName(KindEntrance, p.Entrance)
		anim := fmt.Sprintf("animation: %s %s ease backwards", kf, dur)
		if p.EntranceDelay > 0 {
			anim = fmt.Sprintf("animation: %s %s ease %.1fs backwards", kf, dur, p.EntranceDelay)
		}
		decls = append(decls, anim)
		// 默认弹簧缓动（Apple 式过冲回弹）：timing 覆盖声明在简写之后，
		// 老浏览器忽略 linear() 自动回退简写内的 ease（渐进增强，产物不坏）。
		// 与循环并接时必须给足两个值：单值会扩展到全部动画，弹簧曲线会误伤
		// 循环节奏（pulse/heartbeat 等会出现「弹一下停住」）。
		withLoop := p.LoopEffect != ""
		switch p.EntranceEasing {
		case "classic": // 显式回退经典 ease，无覆盖声明
		case "soft":
			decls = append(decls, timingOverride(SpringSoftCurve, withLoop))
		case "bouncy":
			decls = append(decls, timingOverride(SpringBouncyCurve, withLoop))
		default: // "" 与 "spring" 均走标准弹簧
			decls = append(decls, timingOverride(SpringStandardCurve, withLoop))
		}
		b.NeedKeyframes(kf)
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
		loop := fmt.Sprintf("animation: %s 2.4s ease-in-out infinite", EffectKeyframeName(KindLoop, p.LoopEffect))
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
		b.NeedKeyframes(EffectKeyframeName(KindLoop, p.LoopEffect))
	}
	// 滚动叙事（view() 进度连续绑定）：linear + both 保证进度可逆跟手；
	// 区间覆盖「进入视口 → 离开视口」全程。旧浏览器忽略 timeline 后
	// 保留静态 from 帧（opacity/位移初值），仍优于无效果。
	if p.ScrollStory != "" {
		decls = append(decls,
			"animation: "+EffectKeyframeName(KindStory, p.ScrollStory)+" linear both",
			"animation-timeline: view()",
			"animation-range: entry 0% exit 100%")
		b.NeedKeyframes(EffectKeyframeName(KindStory, p.ScrollStory))
	}
	// 滚动吸顶（全组件共享；StickyTop 未设置时缺省 0）。
	if p.Sticky {
		top := p.StickyTop
		if top == "" {
			top = "0"
		}
		decls = append(decls, "position: sticky", "top: "+top)
	}
	// 悬浮/按压过渡声明：hover 或 active 都需要 transform/box-shadow 过渡，
	// 否则按下后瞬间回弹、无平滑（按压效果全落在 transform/box-shadow 上）。
	needTransition := (hover != "" && hover != "underline" && hover != "shine" && !strings.HasPrefix(hover, "img-")) || p.ActiveEffect != ""
	if needTransition {
		decls = append(decls, "transition: transform 0.25s ease, box-shadow 0.25s ease")
	}
	if len(decls) > 0 {
		b.Add(BreakpointDesktop, sel, decls)
	}
	// 悬浮触发态。
	if hover != "" {
		// 子元素/伪元素特判（结构型悬浮效果，见 effects.go 分类目录）：
		// img-zoom 图片悬停缩放、img-gray 灰度→彩色、shine 光泽扫过（按钮/卡片装饰）。
		// 全部走 AddHover（@media hover:hover 包裹，H5 触屏治理——触屏不粘滞 hover 态）。
		switch hover {
		case "img-zoom":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: transform .3s ease"})
			b.AddHover(sel+":hover img", []string{"transform: scale(1.06)"})
		case "img-gray":
			b.Add(BreakpointDesktop, sel+" img", []string{"filter: grayscale(1)", "transition: filter .4s ease"})
			b.AddHover(sel+":hover img", []string{"filter: grayscale(0)"})
		case "img-zoom-out":
			b.Add(BreakpointDesktop, sel+" img", []string{"transform: scale(1.1)", "transition: transform .35s ease"})
			b.AddHover(sel+":hover img", []string{"transform: scale(1)"})
		case "img-blur":
			b.Add(BreakpointDesktop, sel+" img", []string{"filter: blur(3px)", "transition: filter .35s ease"})
			b.AddHover(sel+":hover img", []string{"filter: blur(0)"})
		case "img-bright":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: filter .35s ease"})
			b.AddHover(sel+":hover img", []string{"filter: brightness(1.15) saturate(1.08)"})
		case "img-sepia":
			b.Add(BreakpointDesktop, sel+" img", []string{"filter: sepia(.75)", "transition: filter .4s ease"})
			b.AddHover(sel+":hover img", []string{"filter: none"})
		case "img-hue":
			// 色相流动：悬停时整幅图色相旋转（纯 filter，不触发布局）。
			// 与 Advanced.HueRotate（静态色相偏移）的区别是触发时机，不是能力。
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: filter .5s ease"})
			b.AddHover(sel+":hover img", []string{"filter: hue-rotate(120deg)"})
		case "img-rotate":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: transform .35s ease"})
			b.AddHover(sel+":hover img", []string{"transform: scale(1.08) rotate(3deg)"})
		case "img-flip":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: transform .5s ease"})
			b.AddHover(sel+":hover img", []string{"transform: perspective(600px) rotateY(180deg)"})
		case "shine":
			// 光斑需裁剪于元素内（Absolute 定位组件慎用，注释见分类目录按钮 FX）。
			b.Add(BreakpointDesktop, sel, []string{"position: relative", "overflow: hidden"})
			b.Add(BreakpointDesktop, sel+"::after", []string{
				"content: ''", "position: absolute", "top: 0", "left: -75%",
				"width: 50%", "height: 100%",
				"background: linear-gradient(120deg, transparent, rgba(255,255,255,.55), transparent)",
				"transform: skewX(-20deg)", "transition: left .6s ease",
			})
			b.AddHover(sel+":hover::after", []string{"left: 125%"})
		}
		hoverDecls := hoverTriggerDecls(hover)
		if len(hoverDecls) > 0 {
			b.AddHover(sel+":hover", hoverDecls)
		}
		// underline 需要基础声明（伪元素线宽 0 → hover 100%）。
		if hover == "underline" {
			b.Add(BreakpointDesktop, sel+"::after", []string{
				"content: ''", "display: block", "height: 2px",
				"width: 0", "background: currentColor",
				"transition: width 0.3s ease",
			})
			b.AddHover(sel+":hover::after", []string{"width: 100%"})
		}
	}
	// 按压反馈（:active，触屏同样生效，不包 hover:hover）。
	if fx := p.ActiveEffect; fx != "" {
		if decls := activeTriggerDecls(fx); len(decls) > 0 {
			b.AddActive(sel+":active", decls)
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
	case "sink":
		return []string{"transform: translateY(4px)"}
	case "grow":
		return []string{"transform: scale(1.06)"}
	case "border-glow":
		return []string{"box-shadow: 0 0 0 3px rgba(59,130,246,.28)"}
	case "text-glow":
		return []string{"text-shadow: 0 0 12px currentColor"}
	case "skew":
		return []string{"transform: skewX(-4deg)"}
	case "underline":
		return nil // ::after 处理
	}
	return nil
}

// activeTriggerDecls 按压反馈触发态声明（:active）。
// 与 hoverTriggerDecls 平行的表驱动：新增按压词只需加一行。
func activeTriggerDecls(effect string) []string {
	switch effect {
	case "press":
		return []string{"transform: scale(0.96)"}
	case "sink":
		return []string{"transform: translateY(2px)"}
	case "pop":
		return []string{"transform: scale(1.03)"}
	case "glow":
		return []string{"box-shadow: 0 0 0 3px rgba(59,130,246,.4), 0 0 20px rgba(59,130,246,.3)"}
	}
	return nil
}

// InteractionControlFields 面板字段键（workbench 检查器「动效」分组渲染用）。
var InteractionControlFields = []string{"interaction.entrance", "interaction.entranceDelay", "interaction.entranceEasing", "interaction.scrollStory", "interaction.hoverLift"}
