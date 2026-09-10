package core

import (
	"strings"
	"testing"
)

// TestCompileSurface 表面质感：glass/liquid 声明与白名单。
func TestCompileSurface(t *testing.T) {
	b := &CSSBuckets{}
	CompileSurface(".t", SurfaceGlass, b)
	css := b.String()
	for _, want := range []string{"backdrop-filter: blur(12px)", "var(--wp-glass-bg"} {
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

	// 非法值由 ValidateAdvanced 白名单拦截（allowedSurface）。
	if !allowedSurface[""] || !allowedSurface[SurfaceGlass] || !allowedSurface[SurfaceLiquid] {
		t.Errorf("表面质感白名单不符")
	}
	if allowedSurface["frosted"] {
		t.Errorf("frosted 不应在白名单内")
	}
}

// TestBorderGradientFlow 渐变边框与流动（Advanced 管线集成行为经 CompileAdvanced，
// 此处验证校验规则与 @property/keyframes 资源存在性）。
func TestBorderGradientFlow(t *testing.T) {
	if !strings.Contains(BorderFlowAngleProperty, "@property --wp-flow-angle") {
		t.Errorf("@property 块不符")
	}
	// keyframes 通用表含边框流动帧。
	if keyframeIndex["wp-border-flow"] == "" || keyframeIndex["wp-bg-flow"] == "" {
		t.Errorf("wp-border-flow / wp-bg-flow 应在通用 keyframes 表中")
	}
}

// TestFocusAndBackgroundFX 焦点光晕与背景流动声明（分类效果库出口函数）。
func TestFocusAndBackgroundFX(t *testing.T) {
	ring := FocusRingDecls()
	joined := strings.Join(ring, "\n")
	for _, want := range []string{"box-shadow: 0 0 0 3px var(--wp-focus-ring", "outline: none"} {
		if !strings.Contains(joined, want) {
			t.Errorf("焦点光晕缺少 %q", want)
		}
	}
	if !strings.Contains(FocusTransitionDecl(), "transition: border-color") {
		t.Errorf("焦点过渡声明不符")
	}
	flow := BackgroundFlowDecls()
	if !strings.Contains(strings.Join(flow, "\n"), "animation: wp-bg-flow 8s ease infinite") {
		t.Errorf("背景流动声明不符")
	}
	tg := TextGradientDecls("linear-gradient(90deg,#f00,#00f)")
	if !strings.Contains(strings.Join(tg, "\n"), "background-clip: text") {
		t.Errorf("渐变文字声明不符")
	}
}

// TestHoverImgZoom 图片悬停缩放：白名单放行 + 子 img 规则（基础 transition + hover scale）。
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

// TestHoverImgGray 图片灰度→彩色：子 img filter 规则。
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

// TestHoverShine 光泽扫过：裁剪上下文 + ::after 光斑 + hover 扫出。
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

// TestNeonShadow 霓虹阴影预设存在且可编译。
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

// TestBackgroundPatternDecls 图案背景出口：dots 默认 / grid 网格。
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

// TestSpringEasing 弹簧缓动（默认）：标准弹簧覆盖声明 + 档位曲线 + classic 回退。
func TestSpringEasing(t *testing.T) {
	// 默认（空 = spring）：输出标准弹簧 timing 覆盖。
	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in"}, b)
	css := b.String()
	if !strings.Contains(css, "animation-timing-function: "+SpringStandardCurve) {
		t.Errorf("默认应输出标准弹簧曲线\n%s", css)
	}
	// 简写内 ease 仍保留（老浏览器回退路径）。
	if !strings.Contains(css, "animation: wp-fade-in 0.6s ease backwards") {
		t.Errorf("简写 ease 兜底缺失\n%s", css)
	}

	// soft/bouncy 档位各走各的曲线。
	for _, tc := range []struct {
		easing, curve string
	}{{"soft", SpringSoftCurve}, {"bouncy", SpringBouncyCurve}} {
		b2 := &CSSBuckets{}
		CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: tc.easing}, b2)
		if !strings.Contains(b2.String(), "animation-timing-function: "+tc.curve) {
			t.Errorf("%s 曲线未输出", tc.easing)
		}
	}

	// classic 显式回退：无 timing 覆盖声明。
	b3 := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: "classic"}, b3)
	if strings.Contains(b3.String(), "animation-timing-function: linear(") {
		t.Errorf("classic 不应输出弹簧曲线")
	}

	// 非法缓动拒绝。
	if err := ValidateInteraction(InteractionProps{Entrance: "fade-in", EntranceEasing: "expo"}); err == nil {
		t.Errorf("非法入场缓动应被拒绝")
	}

	// 与循环并存：timing 覆盖必须两值（否则弹簧曲线误伤循环节奏）。
	b4 := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{Entrance: "fade-in", LoopEffect: "pulse"}, b4)
	css4 := b4.String()
	if !strings.Contains(css4, "animation-timing-function: "+SpringStandardCurve+", ease-in-out") {
		t.Errorf("入场+循环并存时 timing 应为两值\n%s", css4)
	}
	if !strings.Contains(css4, "animation: wp-fade-in 0.6s ease backwards, wp-loop-pulse 2.4s ease-in-out infinite") {
		t.Errorf("入场与循环应并接为动画列表\n%s", css4)
	}
}

// TestScrollStory 滚动叙事：白名单 + view() 进度绑定声明 + 与入场/循环互斥。
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
				"animation: wp-story-" + word + " linear both",
				"animation-timeline: view()",
				"animation-range: entry 0% exit 100%",
				"@keyframes wp-story-" + word,
			} {
				if !strings.Contains(css, want) {
					t.Errorf("CSS 缺少 %q\n%s", want, css)
				}
			}
		})
	}

	// 互斥：滚动叙事 + 入场。
	if err := ValidateInteraction(InteractionProps{ScrollStory: "zoom", Entrance: "fade-in"}); err == nil {
		t.Errorf("滚动叙事与入场动效应互斥")
	}
	// 互斥：滚动叙事 + 循环。
	if err := ValidateInteraction(InteractionProps{ScrollStory: "zoom", LoopEffect: "pulse"}); err == nil {
		t.Errorf("滚动叙事与循环动效应互斥")
	}
	// 非法叙事词拒绝。
	if err := ValidateInteraction(InteractionProps{ScrollStory: "parallax"}); err == nil {
		t.Errorf("非法滚动叙事应被拒绝")
	}
}

// TestHoverTouchGovernance 触屏治理：悬浮规则包 @media (hover: hover)（H5 sticky hover）。
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
	// 基础过渡声明仍在触屏无害（无 hover 即无变化）。
	if !strings.Contains(css, "transition: transform 0.25s ease") {
		t.Errorf("基础过渡声明缺失\n%s", css)
	}
}

// TestSafeAreaDecls 安全区垫高出口：bottom 边 + env fallback。
func TestSafeAreaDecls(t *testing.T) {
	d := strings.Join(SafeAreaDecls("bottom"), "\n")
	if !strings.Contains(d, "padding-bottom: env(safe-area-inset-bottom, 0px)") {
		t.Errorf("安全区声明不符:\n%s", d)
	}
}

// TestStickyAutoZIndex 吸顶自动抬升层叠：未设 ZIndex 时补 10；用户显式设置以其为准。
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

// TestHoverEffectAll 悬浮效果全量（19 词）：白名单放行 + 编译输出 + 触屏治理包裹。
func TestHoverEffectAll(t *testing.T) {
	all := []string{"lift", "scale", "glow", "shadow", "underline", "shine", "sink", "grow",
		"border-glow", "text-glow", "skew", "img-zoom", "img-zoom-out", "img-gray", "img-blur",
		"img-bright", "img-sepia", "img-rotate", "img-flip"}
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
	// 非法词拒绝。
	if err := ValidateInteraction(InteractionProps{HoverEffect: "wiggle"}); err == nil {
		t.Errorf("非法悬浮效果应被拒绝")
	}
}
