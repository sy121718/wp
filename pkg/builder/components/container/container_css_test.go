package container

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func containerCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

func TestContainerCSSEmptyPropsProduceNothing(t *testing.T) {
	if out := containerCSSFor(&Props{Tag: "div"}); out != "" {
		t.Errorf("空容器不该产出 CSS，got:\n%s", out)
	}
}

func TestContainerCSSSlidesKeyframesFollowCount(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Visual.BgSlides = []string{"a.png", "b.png", "c.png"}
	out := containerCSSFor(p)

	for _, want := range []string{
		"@keyframes sky-bg-fade-3 {",
		"  29% { opacity: 1 }",
		"  33% { opacity: 0 }",
		"animation: sky-bg-fade-3 18s ease-in-out infinite;",
		".sky-c-t .sky-bg-slide:nth-child(3) {\n  animation-delay: 12s;\n}",

		".sky-c-t {\n  position: relative;\n  overflow: hidden;\n}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "animation-delay: "); got != 3 {
		t.Errorf("逐张延迟条数应为 3（条数随张数变化），got %d:\n%s", got, out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("产物里留下了未展开的变量占位:\n%s", out)
	}
}

func TestContainerCSSSlideIntervalFallback(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", "12s"}, {"abc", "12s"}, {"0", "12s"}, {"120", "12s"},
		{"10s", "20s"}, {"10", "20s"},
	} {
		p := &Props{Tag: "div"}
		p.Visual.BgSlides = []string{"a.png", "b.png"}
		p.Visual.BgSlideInterval = tc.raw
		want := "animation: sky-bg-fade-2 " + tc.want + " ease-in-out infinite;"
		if out := containerCSSFor(p); !strings.Contains(out, want) {
			t.Errorf("间隔 %q 应产出 %q:\n%s", tc.raw, want, out)
		}
	}
}

func TestContainerCSSGradientFlowNeedsKeyframe(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Visual.BgGradient = "linear-gradient(90deg, #111, #999)"
	p.Visual.BgGradientAnimated = true
	out := containerCSSFor(p)
	if !strings.Contains(out, "background-size: 200% 200%;") ||
		!strings.Contains(out, "animation: sky-bg-flow 8s ease infinite;") {
		t.Errorf("流动声明缺失:\n%s", out)
	}
	if !strings.Contains(out, "@keyframes sky-bg-flow {") {
		t.Errorf("流动关键帧未登记（动画会挂着不动）:\n%s", out)
	}

	p.Visual.BgGradientAnimated = false
	if out := containerCSSFor(p); strings.Contains(out, "sky-bg-flow") {
		t.Errorf("未开流动时不该产出流动动画或关键帧:\n%s", out)
	}
}

func TestContainerCSSBackgroundSourceExclusive(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Visual.Pattern = "dots"
	p.Visual.BgImage = "https://cdn.example.com/a.png"
	p.Visual.BgGradient = "linear-gradient(90deg, #111, #999)"
	out := containerCSSFor(p)
	if n := strings.Count(out, "background-image:"); n != 1 {
		t.Errorf("background-image 只能有一条，got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "background-image: linear-gradient(90deg, #111, #999);") {
		t.Errorf("渐变优先级最高:\n%s", out)
	}
	if strings.Contains(out, "url(") || strings.Contains(out, "radial-gradient") {
		t.Errorf("渐变生效时不该再产出背景图或图案:\n%s", out)
	}

	p = &Props{Tag: "div"}
	p.Visual.BgImage = "https://cdn.example.com/a.png"
	p.Visual.BgPosition = "custom"
	p.Visual.BgSize = "custom"
	out = containerCSSFor(p)
	for _, notWant := range []string{
		"background-position", "background-size", "background-attachment", "background-repeat",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("自定义值缺失时不该产出 %s:\n%s", notWant, out)
		}
	}
}

func TestContainerCSSBorderRadiusShadowFallbacks(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Visual.BorderStyle = "dashed"
	if out := containerCSSFor(p); !strings.Contains(out, "border: 1px dashed currentColor;") {
		t.Errorf("边框缺项未兜底:\n%s", out)
	}

	p = &Props{Tag: "div"}
	p.Visual.Radius = "8px"
	p.Visual.RadiusTL = "2px"
	out := containerCSSFor(p)
	if !strings.Contains(out, "border-radius: 2px 8px 8px 8px;") {
		t.Errorf("四角优先 + 统一值兜底失效:\n%s", out)
	}
	if n := strings.Count(out, "border-radius:"); n != 1 {
		t.Errorf("四角与统一值只能出一条 border-radius，got %d:\n%s", n, out)
	}

	p = &Props{Tag: "div"}
	p.Visual.Shadow = "custom"
	out = containerCSSFor(p)
	if !strings.Contains(out, "box-shadow: 0 4px 12px 0 rgba(0,0,0,.12);") {
		t.Errorf("自定义阴影四参兜底失效:\n%s", out)
	}
	if n := strings.Count(out, "box-shadow:"); n != 1 {
		t.Errorf("自定义与预设级别只能出一条 box-shadow，got %d:\n%s", n, out)
	}

	p = &Props{Tag: "div"}
	p.Visual.Shadow = "huge"
	if out := containerCSSFor(p); strings.Contains(out, "box-shadow") {
		t.Errorf("未知阴影级别既不兜底也不该产出声明:\n%s", out)
	}
}

func TestContainerCSSScopePrefixOnce(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Position.Type = "drawer"
	p.Position.DrawerSide = "left"
	p.StyleEx.Overlay = "rgba(0,0,0,.4)"
	p.StyleEx.ShapeDivider = "wave"
	out := containerCSSFor(p)
	if strings.Contains(out, ".sky-c-t .sky-c-t") || strings.Contains(out, ".sky-c-t.sky-c-t") {
		t.Errorf("作用域前缀被拼了两次:\n%s", out)
	}
	for _, want := range []string{
		".sky-c-t:target {",
		".sky-c-t::before {",
		".sky-c-t > * {",
		".sky-c-t .sky-shape svg {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q:\n%s", want, out)
		}
	}
}

func TestContainerCSSShapeDividerPosition(t *testing.T) {
	p := &Props{Tag: "div"}
	p.StyleEx.ShapeDivider = "wave"
	out := containerCSSFor(p)
	if !strings.Contains(out, ".sky-c-t .sky-shape-bottom {\n  bottom: 0;\n}") {
		t.Errorf("默认位置应为 bottom:\n%s", out)
	}
	if !strings.Contains(out, "color: currentColor;") {
		t.Errorf("无背景色时形状色应回退 currentColor:\n%s", out)
	}

	p.StyleEx.ShapeDividerPosition = "top"
	p.Visual.BgColor = "#123456"
	out = containerCSSFor(p)
	if !strings.Contains(out, ".sky-c-t .sky-shape-top {\n  top: 0;\n}") {
		t.Errorf("位置应同时进选择器与属性名:\n%s", out)
	}
	if !strings.Contains(out, "color: #123456;") {
		t.Errorf("形状色应跟随容器背景色:\n%s", out)
	}
	if strings.Contains(out, "sky-shape-bottom") {
		t.Errorf("位置为 top 时不该再有 bottom 形态:\n%s", out)
	}
}

func TestContainerCSSInteractionSitsBetweenMainAndPosition(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Layout.Engine = EngineFlex
	p.Layout.Flex = &FlexProps{}
	p.Interaction.HoverEffect = "lift"
	p.Position.Type = "relative"
	p.StyleEx.Order = 2
	out := containerCSSFor(p)

	main := strings.Index(out, ".sky-c-t {\n  display: flex;\n}")
	inter := strings.Index(out, "transition: transform 0.25s ease, box-shadow 0.25s ease;")
	pos := strings.Index(out, ".sky-c-t {\n  position: relative;\n}")
	order := strings.Index(out, "order: 2;")
	if main < 0 || inter < 0 || pos < 0 || order < 0 {
		t.Fatalf("段落缺失（main=%d inter=%d pos=%d order=%d）:\n%s", main, inter, pos, order, out)
	}
	if !(main < inter && inter < pos && pos < order) {
		t.Errorf("段落顺序错位（main=%d inter=%d pos=%d order=%d）:\n%s", main, inter, pos, order, out)
	}
}

func TestContainerCSSGridBreakpoints(t *testing.T) {
	p := &Props{Tag: "div"}
	p.Layout.Engine = EngineGrid
	p.Layout.Grid = &GridProps{Columns: ResponsiveInt{Desktop: 3, Mobile: 1}, ColumnGap: "20px"}
	out := containerCSSFor(p)
	if !strings.Contains(out, "grid-template-columns: repeat(3, 1fr);") {
		t.Errorf("桌面列数缺失:\n%s", out)
	}
	if !strings.Contains(out, "@media (max-width: 767px) {\n.sky-c-t {\n  grid-template-columns: repeat(1, 1fr);\n}\n}") {
		t.Errorf("手机列数未进媒体查询:\n%s", out)
	}
	if strings.Contains(out, "max-width: 1024px") {
		t.Errorf("平板端未设列数，不该产出媒体查询:\n%s", out)
	}
}

func TestContainerCSSSwitchesStaySilent(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Tag: "div"}, &b)
	out := b.String() + b.ContainerQueryCSS() + b.TopLevelCSS()
	for _, notWant := range []string{
		"container-type", "--sky-card-layout", "content-visibility", "safe-area-inset",
		"@media", "::before", ":target", "order:", "sky-shape", "sky-bg-fade",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("默认关闭的开关不该产出 %q:\n%s", notWant, out)
		}
	}
}

func TestContainerCSSContainerQueryContext(t *testing.T) {
	p := &Props{Tag: "div"}
	p.StyleEx.ContainerQuery = true
	out := containerCSSFor(p)
	if !strings.Contains(out, "container-type: inline-size;\n  container-name: sky-theme;") {
		t.Errorf("容器查询上下文缺 type 或 name:\n%s", out)
	}
}
