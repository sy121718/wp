package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type TextStyle struct {
	Desktop TextStyleValue `json:"desktop,omitempty"`
	Tablet  TextStyleValue `json:"tablet,omitempty"`
	Mobile  TextStyleValue `json:"mobile,omitempty"`
}

type TextStyleValue struct {
	FontSize string `json:"fontSize,omitempty"`

	LineHeight string `json:"lineHeight,omitempty"`

	TextAlign string `json:"textAlign,omitempty"`
}

var (
	typographyLenRe    = regexp.MustCompile(`^[0-9.]+(px|rem|em|vw|%)?$`)
	typographyClampRe  = regexp.MustCompile(`^clamp\([0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw)\)$`)
	typographyAlignMap = map[string]bool{"left": true, "center": true, "right": true, "justify": true}
)

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

func isTypographyValue(v string) bool {
	return len(v) <= 40 && (typographyLenRe.MatchString(v) || typographyClampRe.MatchString(v))
}

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

type InteractionProps struct {
	Entrance string `json:"entrance,omitempty"`

	EntranceDelay float64 `json:"entranceDelay,omitempty"`

	EntranceDuration string `json:"entranceDuration,omitempty"`

	EntranceEasing string `json:"entranceEasing,omitempty"`

	ScrollReveal string `json:"scrollReveal,omitempty"`

	HoverEffect string `json:"hoverEffect,omitempty"`

	LoopEffect string `json:"loopEffect,omitempty"`

	ScrollStory string `json:"scrollStory,omitempty"`

	ActiveEffect string `json:"activeEffect,omitempty" ct:"select,=无,press=按下缩小,sink=按下下沉,pop=按下放大,glow=按下发光,sec=motion,label=按压反馈"`

	HoverLift bool `json:"hoverLift,omitempty"`

	Sticky bool `json:"sticky,omitempty"`

	StickyTop string `json:"stickyTop,omitempty"`
}

var (
	allowedEntrance = map[string]bool{
		"": true, "fade-in": true, "fade-in-up": true, "fade-in-down": true,
		"fade-in-left": true, "fade-in-right": true,
		"zoom-in": true, "zoom-in-out": true,
		"slide-in-up": true, "slide-in-down": true, "slide-in-left": true, "slide-in-right": true,
		"blur-in": true, "bounce-in": true, "rotate-in": true,

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

	allowedEntranceEasing = map[string]bool{
		"": true,

		"spring": true, "soft": true, "bouncy": true,

		"classic":  true,
		"circ-out": true, "circ-in-out": true, "back-out": true, "quint-in-out": true,
	}
	allowedScrollReveal = map[string]bool{"": true, "reveal": true}
	allowedScrollStory  = map[string]bool{"": true, "zoom": true, "rise": true, "fade": true}
	allowedHoverEffect  = map[string]bool{"": true, "lift": true, "scale": true, "glow": true, "shadow": true, "underline": true, "shine": true,
		"sink": true, "grow": true, "border-glow": true, "text-glow": true, "skew": true,
		"img-zoom": true, "img-zoom-out": true, "img-gray": true, "img-blur": true, "img-bright": true, "img-sepia": true, "img-rotate": true, "img-flip": true,
		"img-hue": true}
	allowedLoopEffect   = map[string]bool{"": true, "pulse": true, "float": true, "drift": true, "shake": true, "jello": true, "heartbeat": true, "blob": true, "flash": true, "rubber-band": true, "swing": true, "tada": true, "wobble": true, "head-shake": true, "bounce": true, "glow": true, "spin": true}
	allowedActiveEffect = map[string]bool{"": true, "press": true, "sink": true, "pop": true, "glow": true}
)

func timingOverride(spring string, withLoop bool) string {
	if withLoop {
		return "animation-timing-function: " + spring + ", ease-in-out"
	}
	return "animation-timing-function: " + spring
}

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

type EffectKind string

const (
	KindEntrance EffectKind = "entrance"
	KindLoop     EffectKind = "loop"
	KindStory    EffectKind = "story"
)

var effectKindPrefix = map[EffectKind]string{
	KindEntrance: "sky-",
	KindLoop:     "sky-loop-",
	KindStory:    "sky-story-",
}

var effectNames = map[EffectKind]map[string]bool{
	KindEntrance: allowedEntrance,
	KindLoop:     allowedLoopEffect,
	KindStory:    allowedScrollStory,
}

func EffectKeyframeName(kind EffectKind, name string) string {
	if name == "" {
		return ""
	}
	return effectKindPrefix[kind] + name
}

func EffectAllowed(kind EffectKind, name string) bool {
	allowed, ok := effectNames[kind]
	if !ok {
		return false
	}
	return allowed[name]
}

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

func ValidateInteraction(p InteractionProps) (err error) {
	for _, check := range interactionFieldChecks {
		if msg := check(p); msg != "" {
			return errors.New(msg)
		}
	}
	return validateInteractionRelations(p)
}

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

func CompileInteraction(sel string, p InteractionProps, b *CSSBuckets) {

	hover := p.HoverEffect
	if p.HoverLift && hover == "" {
		hover = "lift"
	}
	var decls []string

	if p.Entrance != "" {
		dur := entranceDurationCSS(p.EntranceDuration)
		kf := EffectKeyframeName(KindEntrance, p.Entrance)
		anim := fmt.Sprintf("animation: %s %s ease backwards", kf, dur)
		if p.EntranceDelay > 0 {
			anim = fmt.Sprintf("animation: %s %s ease %.1fs backwards", kf, dur, p.EntranceDelay)
		}
		decls = append(decls, anim)

		withLoop := p.LoopEffect != ""
		switch p.EntranceEasing {
		case "classic":
		case "soft":
			decls = append(decls, timingOverride(SpringSoftCurve, withLoop))
		case "bouncy":
			decls = append(decls, timingOverride(SpringBouncyCurve, withLoop))

		case "circ-out":
			decls = append(decls, timingOverride(EaseOutCirc, withLoop))
		case "circ-in-out":
			decls = append(decls, timingOverride(EaseInOutCirc, withLoop))
		case "back-out":
			decls = append(decls, timingOverride(EaseOutBack, withLoop))
		case "quint-in-out":
			decls = append(decls, timingOverride(EaseInOutQuint, withLoop))
		default:
			decls = append(decls, timingOverride(SpringStandardCurve, withLoop))
		}
		b.NeedKeyframes(kf)

		if p.ScrollReveal == "reveal" {
			decls = append(decls,
				"animation-timeline: view()",
				"animation-range: entry 0% entry 60%")
		}
	}

	if p.LoopEffect != "" {
		loop := fmt.Sprintf("animation: %s 2.4s ease-in-out infinite", EffectKeyframeName(KindLoop, p.LoopEffect))
		if p.Entrance != "" {

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

	if p.ScrollStory != "" {
		decls = append(decls,
			"animation: "+EffectKeyframeName(KindStory, p.ScrollStory)+" linear both",
			"animation-timeline: view()",
			"animation-range: entry 0% exit 100%")
		b.NeedKeyframes(EffectKeyframeName(KindStory, p.ScrollStory))
	}

	if p.Sticky {
		top := p.StickyTop
		if top == "" {
			top = "0"
		}
		decls = append(decls, "position: sticky", "top: "+top)
	}

	needTransition := (hover != "" && hover != "underline" && hover != "shine" && !strings.HasPrefix(hover, "img-")) || p.ActiveEffect != ""
	if needTransition {
		decls = append(decls, "transition: transform 0.25s ease, box-shadow 0.25s ease")
	}
	if len(decls) > 0 {
		b.Add(BreakpointDesktop, sel, decls)
	}

	if hover != "" {

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

			b.Add(BreakpointDesktop, sel+" img", []string{"transition: filter .5s ease"})
			b.AddHover(sel+":hover img", []string{"filter: hue-rotate(120deg)"})
		case "img-rotate":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: transform .35s ease"})
			b.AddHover(sel+":hover img", []string{"transform: scale(1.08) rotate(3deg)"})
		case "img-flip":
			b.Add(BreakpointDesktop, sel+" img", []string{"transition: transform .5s ease"})
			b.AddHover(sel+":hover img", []string{"transform: perspective(600px) rotateY(180deg)"})
		case "shine":

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

		if hover == "underline" {
			b.Add(BreakpointDesktop, sel+"::after", []string{
				"content: ''", "display: block", "height: 2px",
				"width: 0", "background: currentColor",
				"transition: width 0.3s ease",
			})
			b.AddHover(sel+":hover::after", []string{"width: 100%"})
		}
	}

	if fx := p.ActiveEffect; fx != "" {
		if decls := activeTriggerDecls(fx); len(decls) > 0 {
			b.AddActive(sel+":active", decls)
		}
	}
}

const glowColorFallback = "#3b82f6"

func glowRing(alpha string) string {
	return "0 0 0 3px color-mix(in srgb, var(--sky-c-primary, " + glowColorFallback + ") " + trimAlpha(alpha) + ", transparent)"
}

func glowHalo(alpha string, blur ...string) string {
	b := "24px"
	if len(blur) > 0 {
		b = blur[0]
	}
	return "0 0 " + b + " color-mix(in srgb, var(--sky-c-primary, " + glowColorFallback + ") " + trimAlpha(alpha) + ", transparent)"
}

func trimAlpha(a string) string {
	if v, err := strconv.ParseFloat(a, 64); err == nil {
		return strconv.FormatFloat(v*100, 'f', -1, 64) + "%"
	}
	return "35%"
}

func hoverTriggerDecls(effect string) []string {
	switch effect {
	case "lift":
		return []string{"transform: translateY(-6px)", "box-shadow: 0 12px 24px rgba(0,0,0,.12)"}
	case "scale":
		return []string{"transform: scale(1.04)"}
	case "glow":

		return []string{"box-shadow: " + glowRing("0.35") + ", " + glowHalo("0.25")}
	case "shadow":
		return []string{"box-shadow: 0 16px 40px rgba(0,0,0,.18)"}
	case "sink":
		return []string{"transform: translateY(4px)"}
	case "grow":
		return []string{"transform: scale(1.06)"}
	case "border-glow":
		return []string{"box-shadow: " + glowRing("0.28")}
	case "text-glow":
		return []string{"text-shadow: 0 0 12px currentColor"}
	case "skew":
		return []string{"transform: skewX(-4deg)"}
	case "underline":
		return nil
	}
	return nil
}

func activeTriggerDecls(effect string) []string {
	switch effect {
	case "press":
		return []string{"transform: scale(0.96)"}
	case "sink":
		return []string{"transform: translateY(2px)"}
	case "pop":
		return []string{"transform: scale(1.03)"}
	case "glow":
		return []string{"box-shadow: " + glowRing("0.4") + ", " + glowHalo("0.3", "20px")}
	}
	return nil
}

var InteractionControlFields = []string{"interaction.entrance", "interaction.entranceDelay", "interaction.entranceEasing", "interaction.scrollStory", "interaction.hoverLift"}
