package core

import (
	"strconv"
	"strings"
	"testing"
)

func TestCompileSurface(t *testing.T) {
	b := &CSSBuckets{}
	CompileSurface(".t", SurfaceGlass, b)
	css := b.String()
	for _, want := range []string{"backdrop-filter: blur(12px)", "var(--sky-glass-bg"} {
		if !strings.Contains(css, want) {
			t.Errorf("glass 缺少 %q\n%s", want, css)
		}
	}

	b2 := &CSSBuckets{}
	CompileSurface(".t", SurfaceLiquid, b2)
	css2 := b2.String()
	for _, want := range []string{"blur(16px) saturate(1.6)", "inset 0 1px 1px rgba(255,255,255,.65)"} {
		if !strings.Contains(css2, want) {
			t.Errorf("liquid 缺少 %q\n%s", want, css2)
		}
	}

	if !allowedSurface[""] || !allowedSurface[SurfaceGlass] || !allowedSurface[SurfaceLiquid] {
		t.Errorf("表面质感白名单不符")
	}
	if allowedSurface["frosted"] {
		t.Errorf("frosted 不应在白名单内")
	}
}

func TestBorderGradientFlow(t *testing.T) {
	if !strings.Contains(BorderFlowAngleProperty, "@property --sky-flow-angle") {
		t.Errorf("@property 块不符")
	}

	if keyframeIndex["sky-border-flow"] == "" || keyframeIndex["sky-bg-flow"] == "" {
		t.Errorf("sky-border-flow / sky-bg-flow 应在通用 keyframes 表中")
	}
}

func TestFocusAndBackgroundFX(t *testing.T) {
	ring := FocusRingDecls()
	joined := strings.Join(ring, "\n")
	for _, want := range []string{"box-shadow: 0 0 0 3px var(--sky-focus-ring", "outline: none"} {
		if !strings.Contains(joined, want) {
			t.Errorf("焦点光晕缺少 %q", want)
		}
	}
	if !strings.Contains(FocusTransitionDecl(), "transition: border-color") {
		t.Errorf("焦点过渡声明不符")
	}
	flow := BackgroundFlowDecls()
	if !strings.Contains(strings.Join(flow, "\n"), "animation: sky-bg-flow 8s ease infinite") {
		t.Errorf("背景流动声明不符")
	}
	tg := TextGradientDecls("linear-gradient(90deg,#f00,#00f)")
	if !strings.Contains(strings.Join(tg, "\n"), "background-clip: text") {
		t.Errorf("渐变文字声明不符")
	}
}

func TestHoverImgZoom(t *testing.T) {
	if err := ValidateInteraction(InteractionProps{HoverEffect: "img-zoom"}); err != nil {
		t.Fatalf("img-zoom 应在悬浮效果白名单内: %v", err)
	}
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "img-zoom"}, b)
	css := b.String()
	for _, want := range []string{".t img {", "transition: transform .3s ease", ".t:hover img {", "transform: scale(1.06)"} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}

func TestHoverImgGray(t *testing.T) {
	if err := ValidateInteraction(InteractionProps{HoverEffect: "img-gray"}); err != nil {
		t.Fatalf("img-gray 应在悬浮效果白名单内: %v", err)
	}
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "img-gray"}, b)
	css := b.String()
	for _, want := range []string{"filter: grayscale(1)", "filter: grayscale(0)"} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}

func TestHoverShine(t *testing.T) {
	if err := ValidateInteraction(InteractionProps{HoverEffect: "shine"}); err != nil {
		t.Fatalf("shine 应在悬浮效果白名单内: %v", err)
	}
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "shine"}, b)
	css := b.String()
	for _, want := range []string{
		"overflow: hidden",
		".t::after {",
		"linear-gradient(120deg, transparent, rgba(255,255,255,.55), transparent)",
		".t:hover::after {",
		"left: 125%",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}

func TestNeonShadow(t *testing.T) {
	neon, ok := ShadowPresets["neon"]
	if !ok || !strings.Contains(neon, "rgba(59,130,246") {
		t.Fatalf("neon 预设缺失或不符: %q", neon)
	}
	b := &CSSBuckets{}
	adv := &AdvancedProps{Shadow: "neon"}
	CompileAdvanced("n1", adv, b)
	if !strings.Contains(b.String(), neon) {
		t.Errorf("编译产物缺少 neon 阴影\n%s", b.String())
	}
}

func TestBackgroundPatternDecls(t *testing.T) {
	dots := strings.Join(BackgroundPatternDecls("", ""), "\n")
	if !strings.Contains(dots, "radial-gradient") {
		t.Errorf("dots 图案不符:\n%s", dots)
	}
	grid := strings.Join(BackgroundPatternDecls("grid", "rgba(255,255,255,.1)"), "\n")
	if !strings.Contains(grid, "linear-gradient(90deg, rgba(255,255,255,.1) 1px") {
		t.Errorf("grid 图案不符:\n%s", grid)
	}
	stroke := strings.Join(TextStrokeDecls("1px", "#000"), "\n")
	if !strings.Contains(stroke, "-webkit-text-stroke: 1px #000") {
		t.Errorf("文字描边声明不符:\n%s", stroke)
	}
}

func TestSpringEasing(t *testing.T) {

	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in"}, b)
	css := b.String()
	if !strings.Contains(css, "animation-timing-function: "+SpringStandardCurve) {
		t.Errorf("默认应输出标准弹簧曲线\n%s", css)
	}

	if !strings.Contains(css, "animation: sky-fade-in 0.6s ease backwards") {
		t.Errorf("简写 ease 兜底缺失\n%s", css)
	}

	for _, tc := range []struct {
		easing, curve string
	}{{"soft", SpringSoftCurve}, {"bouncy", SpringBouncyCurve}} {
		b2 := &CSSBuckets{}
		CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: tc.easing}, b2)
		if !strings.Contains(b2.String(), "animation-timing-function: "+tc.curve) {
			t.Errorf("%s 曲线未输出", tc.easing)
		}
	}

	b3 := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: "classic"}, b3)
	if strings.Contains(b3.String(), "animation-timing-function: linear(") {
		t.Errorf("classic 不应输出弹簧曲线")
	}

	for _, easing := range []string{"circ-out", "circ-in-out", "back-out", "quint-in-out"} {
		b4 := &CSSBuckets{}
		CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: easing}, b4)
		out := b4.String()
		if !strings.Contains(out, "cubic-bezier") {
			t.Errorf("%s 应产出 cubic-bezier 覆盖声明，实际:\n%s", easing, out)
		}
	}

	if err := ValidateInteraction(InteractionProps{Entrance: "fade-in", EntranceEasing: "expo"}); err == nil {
		t.Errorf("非法入场缓动应被拒绝")
	}

	b4 := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in", LoopEffect: "pulse"}, b4)
	css4 := b4.String()
	if !strings.Contains(css4, "animation-timing-function: "+SpringStandardCurve+", ease-in-out") {
		t.Errorf("入场+循环并存时 timing 应为两值\n%s", css4)
	}
	if !strings.Contains(css4, "animation: sky-fade-in 0.6s ease backwards, sky-loop-pulse 2.4s ease-in-out infinite") {
		t.Errorf("入场与循环应并接为动画列表\n%s", css4)
	}
}

func TestScrollStory(t *testing.T) {
	for _, word := range []string{"zoom", "rise", "fade"} {
		t.Run(word, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{ScrollStory: word}); err != nil {
				t.Fatalf("%s 应在滚动叙事白名单内: %v", word, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{ScrollStory: word}, b)
			css := b.String()
			for _, want := range []string{
				"animation: sky-story-" + word + " linear both",
				"animation-timeline: view()",
				"animation-range: entry 0% exit 100%",
				"@keyframes sky-story-" + word,
			} {
				if !strings.Contains(css, want) {
					t.Errorf("CSS 缺少 %q\n%s", want, css)
				}
			}
		})
	}

	if err := ValidateInteraction(InteractionProps{ScrollStory: "zoom", Entrance: "fade-in"}); err == nil {
		t.Errorf("滚动叙事与入场动效应互斥")
	}

	if err := ValidateInteraction(InteractionProps{ScrollStory: "zoom", LoopEffect: "pulse"}); err == nil {
		t.Errorf("滚动叙事与循环动效应互斥")
	}

	if err := ValidateInteraction(InteractionProps{ScrollStory: "parallax"}); err == nil {
		t.Errorf("非法滚动叙事应被拒绝")
	}
}

func TestHoverTouchGovernance(t *testing.T) {
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "lift"}, b)
	css := b.String()
	for _, want := range []string{
		"@media (hover: hover) {",
		".t:hover {",
		"transform: translateY(-6px)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("触屏治理 CSS 缺少 %q\n%s", want, css)
		}
	}

	if !strings.Contains(css, "transition: transform 0.25s ease") {
		t.Errorf("基础过渡声明缺失\n%s", css)
	}
}

func TestSafeAreaDecls(t *testing.T) {
	d := strings.Join(SafeAreaDecls("bottom"), "\n")
	if !strings.Contains(d, "padding-bottom: env(safe-area-inset-bottom, 0px)") {
		t.Errorf("安全区声明不符:\n%s", d)
	}
}

func TestStickyAutoZIndex(t *testing.T) {
	b := &CSSBuckets{}
	CompileAdvanced("n1", &AdvancedProps{Interaction: InteractionProps{Sticky: true}}, b)
	if !strings.Contains(b.String(), "z-index: 10") {
		t.Errorf("吸顶未自动抬升层叠\n%s", b.String())
	}

	b2 := &CSSBuckets{}
	CompileAdvanced("n2", &AdvancedProps{ZIndex: 50, Interaction: InteractionProps{Sticky: true}}, b2)
	css2 := b2.String()
	if !strings.Contains(css2, "z-index: 50") {
		t.Errorf("用户 ZIndex 未生效\n%s", css2)
	}
	if strings.Contains(css2, "z-index: 10") {
		t.Errorf("用户 ZIndex 不应被吸顶缺省覆盖\n%s", css2)
	}
}

func TestHoverEffectAll(t *testing.T) {
	all := []string{"lift", "scale", "glow", "shadow", "underline", "shine", "sink", "grow",
		"border-glow", "text-glow", "skew", "img-zoom", "img-zoom-out", "img-gray", "img-blur",
		"img-bright", "img-sepia", "img-rotate", "img-flip", "img-hue"}
	for _, fx := range all {
		t.Run(fx, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{HoverEffect: fx}); err != nil {
				t.Fatalf("%s 应在悬浮白名单内: %v", fx, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{HoverEffect: fx}, b)
			css := b.String()
			if !strings.Contains(css, "@media (hover: hover)") {
				t.Errorf("%s 缺少触屏治理包裹\n%s", fx, css)
			}
		})
	}

	if err := ValidateInteraction(InteractionProps{HoverEffect: "wiggle"}); err == nil {
		t.Errorf("非法悬浮效果应被拒绝")
	}
}

func TestHueRotate(t *testing.T) {
	valid := func(deg int) error { return ValidateAdvanced(&AdvancedProps{HueRotate: deg}, "t", map[string]bool{}) }

	b := &CSSBuckets{}
	if err := valid(90); err != nil {
		t.Fatalf("HueRotate=90 应通过校验: %v", err)
	}
	CompileAdvanced("t", &AdvancedProps{HueRotate: 90}, b)
	if css := b.String(); !strings.Contains(css, "filter: hue-rotate(90deg)") {
		t.Errorf("缺少色相偏移声明\n%s", css)
	}

	for _, deg := range []int{-360, -120, 360} {
		if err := valid(deg); err != nil {
			t.Errorf("HueRotate=%d 应通过校验: %v", deg, err)
		}
		bb := &CSSBuckets{}
		CompileAdvanced("t", &AdvancedProps{HueRotate: deg}, bb)
		want := "filter: hue-rotate(" + strconv.Itoa(deg) + "deg)"
		if css := bb.String(); !strings.Contains(css, want) {
			t.Errorf("HueRotate=%d 缺少 %q\n%s", deg, want, css)
		}
	}

	z := &CSSBuckets{}
	CompileAdvanced("t", &AdvancedProps{HueRotate: 0}, z)
	if css := z.String(); strings.Contains(css, "hue-rotate") {
		t.Errorf("HueRotate=0 不应输出 filter\n%s", css)
	}

	for _, deg := range []int{-361, 361} {
		if err := valid(deg); err == nil {
			t.Errorf("HueRotate=%d 应被拒绝", deg)
		}
	}
}

func TestImgHueHover(t *testing.T) {
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "img-hue"}, b)
	css := b.String()
	wants := []string{".t img", "transition: filter .5s ease", "@media (hover: hover)", "filter: hue-rotate(120deg)"}
	for _, want := range wants {
		if !strings.Contains(css, want) {
			t.Errorf("img-hue 缺少 %q\n%s", want, css)
		}
	}
}

func TestActiveEffect(t *testing.T) {
	all := []string{"press", "sink", "pop", "glow"}
	for _, fx := range all {
		t.Run(fx, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{ActiveEffect: fx}); err != nil {
				t.Fatalf("%s 应在按压白名单内: %v", fx, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{ActiveEffect: fx}, b)
			css := b.String()
			if !strings.Contains(css, ".t:active") {
				t.Errorf("%s 缺少 :active 规则\n%s", fx, css)
			}
			if !strings.Contains(css, "transition: transform 0.25s ease") {
				t.Errorf("%s 缺少过渡声明（按下回弹需平滑）\n%s", fx, css)
			}
			if strings.Contains(css, "@media (hover: hover)") {
				t.Errorf("%s 不应包 hover:hover（:active 触屏同样生效）\n%s", fx, css)
			}
		})
	}

	if err := ValidateInteraction(InteractionProps{ActiveEffect: "wiggle"}); err == nil {
		t.Errorf("非法按压效果应被拒绝")
	}

	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{HoverEffect: "lift", ActiveEffect: "press"}, b)
	css := b.String()
	hoverIdx := strings.Index(css, ".t:hover")
	activeIdx := strings.Index(css, ".t:active")
	if hoverIdx == -1 || activeIdx == -1 || activeIdx < hoverIdx {
		t.Errorf("hover 与 active 并存时顺序错误（:active 应在 :hover 之后）\nhoverIdx=%d activeIdx=%d\n%s", hoverIdx, activeIdx, css)
	}
}
