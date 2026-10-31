package cardstack

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func nodeOf(p *Props, childN int) *core.Node {
	n := &core.Node{ID: "n1", Type: Type}
	if p != nil {
		raw, err := json.Marshal(p)
		if err != nil {
			panic(err)
		}
		n.Props = raw
	}
	for i := 0; i < childN; i++ {
		id := "c" + strconv.Itoa(i+1)
		n.Children = append(n.Children, &core.Node{ID: id, Type: Type})
	}
	return n
}

func compiled(t *testing.T, n *core.Node, p *Props) string {
	t.Helper()
	b := &core.CSSBuckets{}
	CompileCSS(n, p, 0, b)
	return b.String()
}

func TestValidateProps(t *testing.T) {
	c := &Component{}
	cases := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", nil, false},
		{"尺寸合法", &Props{Width: "240px", Height: "320px", Spacing: "26vh"}, false},
		{"宽度注入", &Props{Width: "240px;}"}, true},
		{"间距注入", &Props{Spacing: "26vh;}"}, true},
		{"粘住位置注入", &Props{StickyTop: "50%;}"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Validate(nodeOf(tt.props, 0), map[string]bool{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%+v) err=%v wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

func TestHoverFanCSS(t *testing.T) {
	s := compiled(t, nodeOf(nil, 0), &Props{})

	for _, want := range []string{
		"rotate(-20deg) translate(-480px, -50px);",
		"rotate(20deg) translate(480px, -50px)",
		"rotate(-20deg) translate(calc(-4 * clamp(0px, calc((50vw - 16px - 17.1010px - 0.9397 * 50% - 0.3420 * var(--sky-cardstack-h) / 2) / 3.7588), 120px)), -50px)",
		"rotate(20deg) translate(calc(4 * clamp(0px,",
		"filter: hue-rotate(-200deg)",
		"min-height: calc(320px + 100px)",
		"--sky-cardstack-h: calc(320px + 20px)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("扇形悬停缺少 %q", want)
		}
	}
	if strings.Contains(s, "container-type") {
		t.Errorf("不应依赖容器查询（contain: layout 会困住 fixed 放大层）")
	}

	if !strings.Contains(s, ".sky-c-n1:hover .sky-cardstack-track .sky-cardstack-card:nth-child(1)") {
		t.Errorf("悬停选择器应挂在容器上并经轨道下到卡片")
	}
	if strings.Contains(s, ".sky-c-n1:hover .sky-c-n1") {
		t.Errorf("悬停选择器重复了容器类名（永不匹配）")
	}
}

func TestHoverGridStack(t *testing.T) {
	p := &Props{Trigger: TriggerHover, Count: 3}
	s := compiled(t, nodeOf(p, 0), p)
	for _, want := range []string{
		"display: grid",
		"justify-items: center",
		"align-items: center",
		"grid-area: 1 / 1",
		"justify-self: center",
		"align-self: center",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("grid 同格叠放缺少 %q", want)
		}
	}

	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "position: absolute") && strings.Contains(line, "sky-cardstack-card") {
			t.Errorf("卡片仍在使用绝对定位：%s", strings.TrimSpace(line))
		}
	}
}

func TestHoverLineHorizontalCSS(t *testing.T) {
	p := &Props{Shape: ShapeLine}
	s := compiled(t, nodeOf(p, 0), p)

	if strings.Contains(s, "transform: rotate(") || strings.Contains(s, ") rotate(") {
		t.Errorf("横排不该有任何旋转（「直接排开」的全部意义就在这里）")
	}
	for _, want := range []string{
		"translate(-480px, 0px);",
		"translate(calc(-4 * clamp(0px, calc((50vw - 16px - 50%) / 4), 120px)), 0px)",
		"min-height: 320px",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("横排缺少 %q", want)
		}
	}
}

func TestHoverLineVerticalCSS(t *testing.T) {
	p := &Props{Shape: ShapeLine, Direction: directionVertical}
	s := compiled(t, nodeOf(p, 0), p)

	if strings.Contains(s, "transform: rotate(") || strings.Contains(s, ") rotate(") {
		t.Errorf("竖排不该有任何旋转")
	}
	for _, want := range []string{
		"translate(0px, -480px);",
		"translate(0px, calc(-4 * clamp(0px, calc((50vh - 16px - var(--sky-cardstack-h) / 2) / 4), 120px)))",
		"min-height: calc(320px + 960px)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("竖排缺少 %q", want)
		}
	}
}

func TestHoverBookSpreadWithinViewport(t *testing.T) {
	p := &Props{Shape: ShapeBook}
	s := compiled(t, nodeOf(p, 0), p)

	if strings.Contains(s, "45vw") {
		t.Errorf("摊开的书不能再拿 45vw 封顶（窄屏会把页面撑出横向滚动）")
	}
	if !strings.Contains(s, "translateX(clamp(calc(-1 * calc((50vw - 16px - 50%) / ") {
		t.Errorf("摊开位移缺少按视口收敛的 clamp")
	}
	if !strings.Contains(s, "rotateY(0deg)") {
		t.Errorf("摊开时应转平（rotateY 0deg）")
	}
	if !strings.Contains(s, "perspective: 1800px") {
		t.Errorf("翻书需要透视，否则 rotateY 被压平成横向缩放")
	}
}

func TestScrollCSS(t *testing.T) {
	p := &Props{Trigger: TriggerScroll}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"position: sticky",
		"top: 50%",
		"translate: 0 -50%",
		"margin: 0 auto 26vh",
		"scale: 0.9400",
		"scale: 1.1000",
		"animation: sky-cs-n1-1 linear both",
		"animation-timeline: view()",
		"animation-range: entry 0% entry 60%",
		"@keyframes sky-cs-n1-1",
		"from { scale: 1.0528; opacity: .5 }",
		"to { scale: 0.9400; opacity: 1 }",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("滚动模式缺少 %q", want)
		}
	}

	for _, block := range strings.Split(s, "}") {
		if !strings.Contains(block, "sky-cardstack-card") || strings.Contains(block, "zoom-layer") {
			continue
		}
		if strings.Contains(block, "position: absolute") {
			t.Errorf("滚动模式的卡片不应绝对堆叠：%s", strings.TrimSpace(block))
		}
	}
}

func TestSlideCSS(t *testing.T) {
	p := &Props{Trigger: TriggerSlide, Count: 3}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"overflow-y: auto",
		"scroll-snap-type: y mandatory",
		"scroll-snap-align: start",
		"scroll-snap-stop: always",
		"width: 100%",
		"height: 100dvh",
		"height: 100vh",
		"min-height: 100dvh",
		"border-radius: 0",
		"box-shadow: none",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("全屏分页缺少 %q", want)
		}
	}

	if strings.Contains(s, "sky-cardstack-scrim") {
		t.Errorf("全屏分页不该输出放大层")
	}
}

func TestSlideStackPageno(t *testing.T) {
	sp := &Props{Trigger: TriggerSlide, Count: 3, SlideStack: true}
	ss := compiled(t, nodeOf(sp, 0), sp)
	for _, want := range []string{
		":has(.sky-cardstack-card.is-current)",
		":not(.is-current) .sky-cardstack-page",
	} {
		if !strings.Contains(ss, want) {
			t.Errorf("堆叠模式缺少页码显隐规则 %q（会与更小的卡叠成重影）", want)
		}
	}

	plain := &Props{Trigger: TriggerSlide, Count: 3}
	ps := compiled(t, nodeOf(plain, 0), plain)
	if strings.Contains(ps, ":has(.sky-cardstack-card.is-current)") {
		t.Errorf("平铺模式不该套页码显隐规则")
	}
}

func TestSlideHighlight(t *testing.T) {
	hp := &Props{Trigger: TriggerSlide, Count: 2, SlideHighlight: "glow"}
	hs := compiled(t, nodeOf(hp, 0), hp)
	for _, want := range []string{
		"animation: sky-loop-glow 2s ease-in-out infinite",
		"@keyframes sky-loop-glow",
		".sky-cardstack-card.is-current",
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("当前屏高亮缺少 %q", want)
		}
	}

	bp := &Props{Trigger: TriggerSlide, Count: 2, SlideEffect: "flip", SlideHighlight: "glow"}
	bs := compiled(t, nodeOf(bp, 0), bp)
	for _, want := range []string{
		".sky-cardstack-card.is-enter",
		".sky-cardstack-card.is-current",
		"animation: sky-flip-in-x 800ms cubic-bezier(.22,.61,.36,1) both",
		"animation: sky-loop-glow 2s ease-in-out infinite",
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("切换动画 + 当前屏高亮缺少 %q", want)
		}
	}

	if strings.Contains(bs, "animation-timeline") {
		t.Errorf("slide 不该用 animation-timeline: view()：内嵌滚动容器下实测不驱动动画")
	}

	plain := &Props{Trigger: TriggerSlide, Count: 2}
	if ps := compiled(t, nodeOf(plain, 0), plain); strings.Contains(ps, "is-enter") {
		t.Errorf("都不开时不该输出动画规则")
	}
}

func TestLoopEffects(t *testing.T) {
	hp := &Props{Trigger: TriggerHover, Count: 3, HoverEffect: "glow"}
	hs := compiled(t, nodeOf(hp, 0), hp)
	for _, want := range []string{
		"animation: sky-loop-glow 2s ease-in-out infinite",
		"@keyframes sky-loop-glow",
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("悬停循环效果缺少 %q", want)
		}
	}

	dp := &Props{Trigger: TriggerDeck, Count: 3, DeckHighlight: "flash"}
	ds := compiled(t, nodeOf(dp, 0), dp)
	for _, want := range []string{
		"animation: sky-loop-flash 2s ease-in-out infinite",
		"@keyframes sky-loop-flash",
		".sky-c-n1 .sky-cardstack-card.is-active",
	} {
		if !strings.Contains(ds, want) {
			t.Errorf("主卡高亮缺少 %q", want)
		}
	}

	plain := &Props{Trigger: TriggerHover, Count: 3}
	if ps := compiled(t, nodeOf(plain, 0), plain); strings.Contains(ps, "sky-loop-") {
		t.Errorf("缺省不该带循环动画")
	}
}

func TestSlideEffect(t *testing.T) {
	plain := &Props{Trigger: TriggerSlide, Count: 2}
	ps := compiled(t, nodeOf(plain, 0), plain)
	if strings.Contains(ps, "animation-timeline: view()") {
		t.Errorf("缺省不该带切换动画（只有位置变化）")
	}

	cases := map[string][2]string{
		"fade":   {"sky-fade-in-bottom-left", "sky-fade-in-bottom-right"},
		"zoom":   {"sky-zoom-in-up", "sky-zoom-in-right"},
		"flip":   {"sky-flip-in-x", "sky-flip-in-y"},
		"bounce": {"sky-bounce-in-up", "sky-bounce-in-right"},
		"back":   {"sky-back-in-up", "sky-back-in-right"},
		"rotate": {"sky-rotate-in-up-left", "sky-rotate-in-up-right"},
		"light":  {"sky-light-speed-in-left", "sky-light-speed-in-right"},
		"roll":   {"sky-roll-in", "sky-roll-in"},
		"jack":   {"sky-jack-in-the-box", "sky-jack-in-the-box"},
	}
	for effect, kfs := range cases {
		for _, side := range []int{0, 1} {
			kf := kfs[side]
			p := &Props{Trigger: TriggerSlide, Count: 2, SlideEffect: effect}
			if side == 1 {
				p.SlideDirection = slideDirectionHorizontal
			}
			s := compiled(t, nodeOf(p, 0), p)
			for _, want := range []string{
				"animation: " + kf + " 800ms cubic-bezier(.22,.61,.36,1) both",
				".sky-cardstack-card.is-enter",
				"@keyframes " + kf,
			} {
				if !strings.Contains(s, want) {
					t.Errorf("效果 %s（side=%d）缺少 %q", effect, side, want)
				}
			}
		}
	}
}

func TestDeckTransition(t *testing.T) {
	def := &Props{Trigger: TriggerDeck, Count: 3}
	ds := compiled(t, nodeOf(def, 0), def)
	if !strings.Contains(ds, "transition: transform 450ms cubic-bezier(.22,.61,.36,1)") {
		t.Errorf("缺省应为 450ms 平滑缓出")
	}

	spring := &Props{Trigger: TriggerDeck, Count: 3, DeckTransition: "spring", DeckDuration: 600}
	ss := compiled(t, nodeOf(spring, 0), spring)
	if !strings.Contains(ss, "transition: transform 600ms cubic-bezier(.34,1.56,.64,1)") {
		t.Errorf("回弹曲线与自定义时长应生效")
	}
	if !strings.Contains(ss, "cubic-bezier(.34,1.56,.64,1)") {
		t.Errorf("回弹曲线缺失")
	}
}

func TestSlideScrollbarHidden(t *testing.T) {
	p := &Props{Trigger: TriggerSlide, Count: 3}
	s := compiled(t, nodeOf(p, 0), p)
	for _, want := range []string{
		"scrollbar-width: none",
		"-ms-overflow-style: none",
		"::-webkit-scrollbar",
		"display: none",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("滚动条隐藏缺少 %q", want)
		}
	}
	if strings.Contains(s, "scrollbar-width: thin") {
		t.Errorf("旧的 thin 写法应被替换掉")
	}
}

func TestSlideHorizontal(t *testing.T) {
	p := &Props{Trigger: TriggerSlide, Count: 3, SlideDirection: slideDirectionHorizontal, SlideStack: true}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"overflow-x: auto",
		"overflow-y: hidden",
		"scroll-snap-type: x mandatory",
		"left: 0",
		"position: sticky",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("横向分页缺少 %q", want)
		}
	}
	if strings.Contains(s, "scroll-snap-type: y mandatory") {
		t.Errorf("横向分页不该再用纵向吸附")
	}
}

func TestSlideStack(t *testing.T) {
	plain := &Props{Trigger: TriggerSlide, Count: 3}
	ps := compiled(t, nodeOf(plain, 0), plain)
	if strings.Contains(ps, "position: sticky") {
		t.Errorf("缺省（平铺）不该出现 sticky")
	}

	stack := &Props{Trigger: TriggerSlide, Count: 3, SlideStack: true}
	ss := compiled(t, nodeOf(stack, 0), stack)
	for _, want := range []string{
		"position: sticky",
		"top: 0",
		"z-index: 1",
		"z-index: 3",
	} {
		if !strings.Contains(ss, want) {
			t.Errorf("堆叠翻页缺少 %q", want)
		}
	}
}

func TestSlideFitViewport(t *testing.T) {
	inline := &Props{Trigger: TriggerSlide, Count: 3}
	is := compiled(t, nodeOf(inline, 0), inline)
	if strings.Contains(is, "position: fixed") {
		t.Errorf("缺省（页面内滚动区）不该脱离文档流")
	}

	full := &Props{Trigger: TriggerSlide, Count: 3, SlideFit: slideFitViewport}
	fs := compiled(t, nodeOf(full, 0), full)
	for _, want := range []string{
		"position: fixed",
		"inset: 0",
		"width: 100vw",
		"height: 100dvh",
		"height: 100vh",
	} {
		if !strings.Contains(fs, want) {
			t.Errorf("铺满视口缺少 %q", want)
		}
	}
}

func TestSlideDefaults(t *testing.T) {
	p := &Props{Trigger: TriggerSlide}
	if n := cardCount(nodeOf(p, 0), p); n != defaultSlideCount {
		t.Errorf("全屏分页占位卡缺省应为 %d 屏，got %d", defaultSlideCount, n)
	}
	if w, _ := cardSize(p, TriggerSlide, false); w != "100%" {
		t.Errorf("全屏分页卡片宽度应为 100%%，got %q", w)
	}
	if zoomEnabled(p) {
		t.Errorf("全屏分页应自动关闭点击放大")
	}
	if zoomEnabled(&Props{Trigger: TriggerHover}) != true {
		t.Errorf("其他模式仍应默认开启点击放大")
	}

	custom := &Props{Trigger: TriggerSlide, SlideHeight: "80dvh"}
	cs := compiled(t, nodeOf(custom, 0), custom)
	if !strings.Contains(cs, "height: 80dvh") || !strings.Contains(cs, "height: 80vh") {
		t.Errorf("自定义每屏高度应生效并带 vh 降级")
	}
}

func TestDeckCSS(t *testing.T) {
	p := &Props{Trigger: TriggerDeck}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"cursor: grab",
		"touch-action: pan-y",
		"grid-area: 1 / 1",
		"--sky-deck-off: -4",
		"--sky-deck-abs: 4",
		"translateX(calc(var(--sky-deck-off, 0) * 54%))",
		"rotate(calc(var(--sky-deck-off, 0) * 4deg))",
		"scale(calc(1 - var(--sky-deck-abs, 0) * 0.0600))",
		"z-index: calc(50 - var(--sky-deck-abs, 0))",
		".sky-c-n1 .sky-cardstack-card.is-active",
		"transition: transform 450ms cubic-bezier(.22,.61,.36,1)",

		"opacity: max(0, calc(1 - var(--sky-deck-abs, 0) * 0.28))",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("堆叠轮播缺少 %q", want)
		}
	}
	if strings.Contains(s, "position: sticky") || strings.Contains(s, "rotate(-20deg)") {
		t.Errorf("堆叠轮播混入了其他模式的几何")
	}
}

func TestDeckView(t *testing.T) {
	p := &Props{Trigger: TriggerDeck, Count: 5}
	v, err := BuildView(nodeOf(p, 0), p, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !v.Deck {
		t.Errorf("轮播模式应标记 Deck")
	}
	if v.DeckIndex != 2 {
		t.Errorf("5 张卡的初始主卡应为第 3 张（序号 2），got %d", v.DeckIndex)
	}
	hover := &Props{Trigger: TriggerHover}
	v2, _ := BuildView(nodeOf(hover, 0), hover, &core.RenderContext{})
	if v2.Deck {
		t.Errorf("悬停模式不该标记 Deck")
	}

	looped := &Props{Trigger: TriggerDeck, Count: 5, DeckLoop: true}
	v3, err := BuildView(nodeOf(looped, 0), looped, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView(loop): %v", err)
	}
	if !v3.DeckLoop {
		t.Errorf("开启循环切换后应标记 DeckLoop")
	}
}

func TestDragCylinderSideRoomAccountsPerspective(t *testing.T) {
	p := &Props{Trigger: TriggerDrag, DragMode: dragModeCylinder}
	s := compiled(t, nodeOf(p, 0), p)

	if !strings.Contains(s, "perspective: 1600px") {
		t.Errorf("圆柱环绕缺少透视")
	}

	var sideLine string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--sky-cardstack-side") {
			sideLine = strings.TrimSpace(line)
		}
	}
	if !strings.Contains(sideLine, "calc((100vw - calc(") || !strings.Contains(sideLine, " * 1.") {
		t.Errorf("单侧可用空间没有把透视放大计入卡片占位：%s", sideLine)
	}
	if !strings.Contains(s, "/ 2 - 20px)") {
		t.Errorf("每侧留白应为 20px（含桌面滚动条 7.5px 的误差余量）")
	}

	flat := compiled(t, nodeOf(&Props{Trigger: TriggerDrag}, 0), &Props{Trigger: TriggerDrag})
	if strings.Contains(flat, "calc((100vw - calc(") {
		t.Errorf("平面环没有透视放大，不该乘系数")
	}
}

func TestDragCSS(t *testing.T) {
	p := &Props{Trigger: TriggerDrag}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"cursor: grab",
		"touch-action: pan-y",
		"--sky-cardstack-rot: 0deg",

		"grid-area: 1 / 1",
		"transform: rotate(calc(0deg + var(--sky-cardstack-rot, 0deg))) translateY(calc(-1 * min(350px, var(--sky-cardstack-side, 40vw))))",
		"rotate(calc(-320deg - var(--sky-cardstack-rot, 0deg)))",
		"transition: transform .35s ease",
		".sky-c-n1.is-dragging .sky-cardstack-card",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("拖拽模式缺少 %q", want)
		}
	}

	if strings.Contains(s, "position: sticky") || strings.Contains(s, "rotate(-20deg)") {
		t.Errorf("拖拽模式混入了其他模式的几何")
	}
}

func TestStackModesUseGrid(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *Props
	}{
		{"deck", &Props{Trigger: TriggerDeck, Count: 3}},
		{"drag", &Props{Trigger: TriggerDrag, Count: 3}},
	} {
		s := compiled(t, nodeOf(tc.p, 0), tc.p)
		for _, want := range []string{
			"display: grid",
			"grid-area: 1 / 1",
			"justify-items: center",
			"min-height: ",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s 模式缺少 %q", tc.name, want)
			}
		}
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, "position: absolute") && strings.Contains(line, "sky-cardstack-card") {
				t.Errorf("%s 模式的卡片仍在绝对定位：%s", tc.name, strings.TrimSpace(line))
			}
		}
	}
}

func TestDragRadius(t *testing.T) {
	if got := dragRadius(&Props{}, 9, "240px", "320px"); math.Abs(got-350.86) > 0.5 {
		t.Errorf("自动半径 = 卡宽/(2·sin(π/n)) ≈ 350.86，got %v", got)
	}
	if got := dragRadius(&Props{DragRadius: 500}, 9, "240px", "320px"); got != 500 {
		t.Errorf("用户指定半径应优先，got %v", got)
	}
	if got := dragRadius(&Props{}, 1, "240px", "320px"); got != 160 {
		t.Errorf("单张卡半径退化为卡高一半，got %v", got)
	}

	if got := dragRadius(&Props{}, 16, "240px", "320px"); got < 600 {
		t.Errorf("16 张卡的半径应显著变大，got %v", got)
	}
}

func TestDragView(t *testing.T) {
	v, err := BuildView(nodeOf(&Props{Trigger: TriggerDrag}, 0), &Props{Trigger: TriggerDrag}, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !v.Drag {
		t.Errorf("拖拽模式应标记 Drag")
	}
	p := &Props{Trigger: TriggerHover}
	v2, _ := BuildView(nodeOf(p, 0), p, &core.RenderContext{})
	if v2.Drag {
		t.Errorf("悬停模式不该标记 Drag")
	}
}

func TestZoomCSS(t *testing.T) {
	s := compiled(t, nodeOf(nil, 0), &Props{})

	for _, want := range []string{
		".sky-cardstack-toggle",
		"pointer-events: none",
		":has(> .sky-cardstack-toggle:checked)",
		"position: fixed",
		"margin: auto",
		"translate: none",
		"scale: 1",
		"z-index: 1001",
		".sky-cardstack-scrim",
		".sky-cardstack-close-btn",
		"display: block",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("放大层缺少 %q", want)
		}
	}

	off := compiled(t, nodeOf(nil, 0), &Props{Zoom: "off"})
	if strings.Contains(off, "sky-cardstack-scrim") {
		t.Errorf("Zoom=off 不应输出遮罩")
	}
}

func TestCardLayoutParams(t *testing.T) {
	def := compiled(t, nodeOf(nil, 2), &Props{})
	for _, want := range []string{
		"flex-direction: column",
		"justify-content: center",
		"align-items: center",
		"gap: 10px",
	} {
		if !strings.Contains(def, want) {
			t.Errorf("缺省卡内布局缺少 %q", want)
		}
	}

	row := compiled(t, nodeOf(nil, 2), &Props{
		CardLayout: "row", CardGap: "18px", CardJustify: "space-between", CardAlign: "flex-start",
	})
	for _, want := range []string{
		"flex-direction: row",
		"gap: 18px",
		"justify-content: space-between",
		"align-items: flex-start",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("自定义卡内布局缺少 %q", want)
		}
	}

	bad := compiled(t, nodeOf(nil, 2), &Props{CardLayout: "grid; color: red", CardAlign: "evil"})
	if strings.Contains(bad, "grid; color: red") || strings.Contains(bad, "align-items: evil") {
		t.Errorf("非法枚举值被透传进 CSS")
	}
	if !strings.Contains(bad, "flex-direction: column") || !strings.Contains(bad, "align-items: center") {
		t.Errorf("非法枚举值应退回缺省")
	}
}

func TestContentCards(t *testing.T) {
	s := compiled(t, nodeOf(nil, 3), &Props{})

	if strings.Contains(s, "hue-rotate") {
		t.Errorf("内容卡不该套色相滤镜")
	}
	if !strings.Contains(s, "overflow: hidden") {
		t.Errorf("内容卡应裁剪溢出内容")
	}
	if !strings.Contains(s, "min-height: 240px") {
		t.Errorf("内容卡高度缺省 240px（min-height）")
	}
	if !strings.Contains(s, ":nth-child(3)") || strings.Contains(s, ":nth-child(4)") {
		t.Errorf("卡片数应等于子节点数（3 张）")
	}
}

type fakeCollection struct{}

func (fakeCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	if source != "content:article" {
		return nil, fmt.Errorf("未知集合源 %q", source)
	}
	return []map[string]any{
		{"id": 1, "slug": "first", "title": "第一篇文章", "excerpt": "第一篇摘要", "featuredImage": "https://x/1.jpg"},
		{"id": 2, "slug": "second", "title": "第二篇文章", "excerpt": "第二篇摘要", "featuredImage": "https://x/2.jpg"},
	}, nil
}

func TestCollectionCards(t *testing.T) {
	p := &Props{
		CollectionSource: "content:article",
		CardTitleField:   "title",
		CardTextField:    "excerpt",
		CardImageField:   "featuredImage",
		CardLinkField:    "slug",
		CardLinkPrefix:   "/article/",
	}
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	view, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.Collection {
		t.Fatalf("应标记为集合模式")
	}
	if len(view.Cards) != 2 {
		t.Fatalf("应展开 2 张卡，got %d", len(view.Cards))
	}
	c := view.Cards[0]
	if c.Title != "第一篇文章" || c.Text != "第一篇摘要" || c.Image != "https://x/1.jpg" || c.Href != "/article/first" {
		t.Errorf("字段映射结果不对：%+v", c)
	}
	if !c.HasImage || !c.HasTitle || !c.HasText || !c.HasHref {
		t.Errorf("Has* 标记应全部为真：%+v", c)
	}
	if strings.Contains(c.AriaLabel, "1") && c.AriaLabel != "放大：第一篇文章" {
		t.Errorf("无障碍描述应带标题：%q", c.AriaLabel)
	}

	p.CollectionLimit = 1
	view2, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView(limit=1): %v", err)
	}
	if len(view2.Cards) != 1 {
		t.Errorf("limit=1 应只取 1 条，got %d", len(view2.Cards))
	}
}

func TestCollectionFieldError(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "tittle"}
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	_, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err == nil {
		t.Fatalf("字段名拼错应报错")
	}
	if !strings.Contains(err.Error(), "可用字段") || !strings.Contains(err.Error(), "title") {
		t.Errorf("报错应列出可用字段：%v", err)
	}
}

type fakeSchemaCollection struct{ fakeCollection }

func (fakeSchemaCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{
		Source: "content:article",
		Label:  "文章列表",
		Fields: []string{"title", "excerpt", "featuredImage"},
	}}, nil
}

func TestCollectionSchemaWhitelist(t *testing.T) {
	ctx := &core.RenderContext{Collection: fakeSchemaCollection{}}

	ok := &Props{CollectionSource: "content:article", CardTitleField: "title", CardImageField: "featuredImage"}
	if _, err := BuildView(nodeOf(nil, 0), ok, ctx); err != nil {
		t.Fatalf("白名单内字段应通过：%v", err)
	}

	bad := &Props{CollectionSource: "content:article", CardTextField: "body"}
	_, err := BuildView(nodeOf(nil, 0), bad, ctx)
	if err == nil {
		t.Fatalf("白名单外字段应报错")
	}
	if !strings.Contains(err.Error(), "白名单") || !strings.Contains(err.Error(), "title") {
		t.Errorf("报错应说明白名单并列出可用字段：%v", err)
	}

	_, err = BuildView(nodeOf(nil, 0), &Props{CollectionSource: "content:unknown"}, ctx)
	if err == nil || !strings.Contains(err.Error(), "未知集合源") {
		t.Errorf("未知集合源应报错：%v", err)
	}

	view, err := BuildView(nodeOf(nil, 0), ok, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Cards) != 2 || view.Cards[0].Title != "第一篇文章" {
		t.Errorf("裁剪后仍应正常出卡：%+v", view.Cards)
	}
}

func TestCollectionMissingResolver(t *testing.T) {
	p := &Props{CollectionSource: "content:article"}
	if _, err := BuildView(nodeOf(nil, 0), p, &core.RenderContext{}); err == nil {
		t.Fatalf("缺少集合解析器应报错")
	}
}

func TestCollectionCSS(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "title"}
	n := nodeOf(nil, 0)
	b := &core.CSSBuckets{}
	CompileCSS(n, p, 2, b)
	s := b.String()

	if !strings.Contains(s, ":nth-child(2)") || strings.Contains(s, ":nth-child(3)") {
		t.Errorf("逐卡规则数量应与内容条数一致（2 条）")
	}
	if !strings.Contains(s, "min-height: 240px") {
		t.Errorf("集合卡应走内容卡样式（min-height 缺省 240px）")
	}
	for _, want := range []string{".sky-cardstack-img", ".sky-cardstack-title", ".sky-cardstack-text", ".sky-cardstack-link"} {
		if !strings.Contains(s, want) {
			t.Errorf("集合卡元素样式缺少 %q", want)
		}
	}
}

type emptyCollection struct{}

func (emptyCollection) ResolveCollection(_ context.Context, _ string, _ map[string]string) ([]map[string]any, error) {
	return nil, nil
}

func (emptyCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{Source: "content:article", Label: "文章列表", Fields: []string{"title"}}}, nil
}

func TestCollectionEmptyState(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "title"}
	v, err := BuildView(nodeOf(nil, 0), p, &core.RenderContext{Collection: emptyCollection{}})
	if err != nil {
		t.Fatalf("空集合不该报错: %v", err)
	}
	if !v.Empty {
		t.Errorf("空集合应标记 Empty")
	}
	if v.EmptyText != defaultCollectionEmptyText {
		t.Errorf("占位文案缺省应为 %q，got %q", defaultCollectionEmptyText, v.EmptyText)
	}
	if v.HideEmpty {
		t.Errorf("缺省应显示占位而不是隐藏整个组件")
	}
	if len(v.Cards) != 0 {
		t.Errorf("空集合不该产出卡片，got %d", len(v.Cards))
	}

	hidden := &Props{CollectionSource: "content:article", CollectionEmpty: "hide"}
	v2, _ := BuildView(nodeOf(nil, 0), hidden, &core.RenderContext{Collection: emptyCollection{}})
	if !v2.HideEmpty {
		t.Errorf("collectionEmpty=hide 应标记 HideEmpty")
	}
}

func TestBuiltinTextOverridable(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "title", CardLinkField: "slug",
		CardLinkText: "阅读全文", CollectionEmptyText: "还没有内容"}
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	v, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.LinkText != "阅读全文" {
		t.Errorf("链接文案应可覆盖，got %q", v.LinkText)
	}
	if v.EmptyText != "还没有内容" {
		t.Errorf("占位文案应可覆盖，got %q", v.EmptyText)
	}
	if len(v.Cards) == 0 || v.Cards[0].LinkText != "阅读全文" {
		t.Errorf("卡片应带上自定义链接文案：%+v", v.Cards)
	}

	def, _ := BuildView(nodeOf(nil, 0), &Props{CollectionSource: "content:article"}, ctx)
	if def.LinkText != defaultCardLinkText {
		t.Errorf("缺省链接文案应为 %q，got %q", defaultCardLinkText, def.LinkText)
	}
}

func TestBuildView(t *testing.T) {
	v, err := BuildView(nodeOf(nil, 0), &Props{}, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(v.Cards) != 9 || v.HasContent {
		t.Fatalf("无子节点应 9 张占位卡且 HasContent=false，got %d/%v", len(v.Cards), v.HasContent)
	}
	if v.Cards[0].Label != "1" || v.Cards[8].Label != "9" {
		t.Errorf("占位卡序号应为 1..N，got %q/%q", v.Cards[0].Label, v.Cards[8].Label)
	}
	if !strings.Contains(v.Cards[0].AriaLabel, "1") {
		t.Errorf("缺少无障碍描述：%q", v.Cards[0].AriaLabel)
	}
	v2, err := BuildView(nodeOf(nil, 2), &Props{}, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView(2 children): %v", err)
	}
	if len(v2.Cards) != 2 || !v2.HasContent {
		t.Fatalf("2 个子节点应 2 张内容卡且 HasContent=true，got %d/%v", len(v2.Cards), v2.HasContent)
	}
}
