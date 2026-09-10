package cardstack

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// nodeOf 构造测试节点：props 为 nil 表示空 props；childN 为子节点数量
// （用已注册的 cardstack 自身当子节点类型，避免依赖其它组件包）。
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

// compiled 编译节点并返回 CSS。
func compiled(t *testing.T, n *core.Node, p *Props) string {
	t.Helper()
	b := &core.CSSBuckets{}
	CompileCSS(n, p, 0, b) // 0 = 按静态卡片数（子节点数 / 占位卡数量）生成
	return b.String()
}

// TestValidateProps 校验：尺寸类字段必须是 CSS 安全值，其余由 ct tag 兜底。
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

// TestHoverFanCSS 悬停 + 扇形：rotate 在 translate 之前（位移走旋转坐标系 → 弧线），
// 收敛式扣掉上抬量的旋转投影，且不再依赖容器查询。
func TestHoverFanCSS(t *testing.T) {
	s := compiled(t, nodeOf(nil, 0), &Props{}) // 默认 9 张数字卡

	for _, want := range []string{
		"rotate(-20deg) translate(-480px, -50px);", // 固定值兜底：第 1 张
		"rotate(20deg) translate(480px, -50px)",    // 第 9 张（扇形对称）
		"rotate(-20deg) translate(calc(-4 * clamp(0px, calc((50vw - 16px - 17.1010px - 0.9397 * 50% - 0.3420 * var(--sky-cardstack-h) / 2) / 3.7588), 120px)), -50px)",
		"rotate(20deg) translate(calc(4 * clamp(0px,",
		"filter: hue-rotate(-200deg)",           // 数字卡位置派生色相
		"min-height: calc(320px + 100px)",       // 纵向预留上抬空间
		"--sky-cardstack-h: calc(320px + 20px)", // 旋转外扩要用的卡高
	} {
		if !strings.Contains(s, want) {
			t.Errorf("扇形悬停缺少 %q", want)
		}
	}
	if strings.Contains(s, "container-type") {
		t.Errorf("不应依赖容器查询（contain: layout 会困住 fixed 放大层）")
	}
	// 选择器形态：:hover 挂容器、经轨道到卡片；容器类名只能出现一次。
	if !strings.Contains(s, ".sky-c-n1:hover .sky-cardstack-track .sky-cardstack-card:nth-child(1)") {
		t.Errorf("悬停选择器应挂在容器上并经轨道下到卡片")
	}
	if strings.Contains(s, ".sky-c-n1:hover .sky-c-n1") {
		t.Errorf("悬停选择器重复了容器类名（永不匹配）")
	}
}

// TestHoverLineHorizontalCSS 横排：卡片不带任何角度，纯水平平移成一行。
func TestHoverLineHorizontalCSS(t *testing.T) {
	p := &Props{Shape: ShapeLine}
	s := compiled(t, nodeOf(p, 0), p)

	if strings.Contains(s, "transform: rotate(") || strings.Contains(s, ") rotate(") {
		t.Errorf("横排不该有任何旋转（「直接排开」的全部意义就在这里）")
	}
	for _, want := range []string{
		"translate(-480px, 0px);", // 第 1 张固定值
		"translate(calc(-4 * clamp(0px, calc((50vw - 16px - 50%) / 4), 120px)), 0px)",
		"min-height: 320px", // 横排不需要纵向预留
	} {
		if !strings.Contains(s, want) {
			t.Errorf("横排缺少 %q", want)
		}
	}
}

// TestHoverLineVerticalCSS 竖排：同样无角度，纯垂直平移成一列；收敛按视口高度，
// 轨道按「卡高 + 2×最大步距×位移」预留纵向铺开空间。
func TestHoverLineVerticalCSS(t *testing.T) {
	p := &Props{Shape: ShapeLine, Direction: directionVertical}
	s := compiled(t, nodeOf(p, 0), p)

	if strings.Contains(s, "transform: rotate(") || strings.Contains(s, ") rotate(") {
		t.Errorf("竖排不该有任何旋转")
	}
	for _, want := range []string{
		"translate(0px, -480px);",
		"translate(0px, calc(-4 * clamp(0px, calc((50vh - 16px - var(--sky-cardstack-h) / 2) / 4), 120px)))",
		"min-height: calc(320px + 960px)", // 2 × 4 × 120
	} {
		if !strings.Contains(s, want) {
			t.Errorf("竖排缺少 %q", want)
		}
	}
}

// TestScrollCSS 滚动堆叠：sticky 层叠是基础形态，scroll-driven 跟手收敛叠在同一条
// 规则上 —— 老浏览器丢弃未知属性后动画停在终态，即静态缩放，自动降级。
func TestScrollCSS(t *testing.T) {
	p := &Props{Trigger: TriggerScroll}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"position: sticky",
		"top: 50%",
		"translate: 0 -50%",
		"margin: 0 auto 26vh",
		"scale: 0.9400", // 第 1 张 = base
		"scale: 1.1000", // 第 9 张 = 94% + 8*2%
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
	// absolute 只该出现在两条「隐藏单选」规则里（卡片本身是 sticky 排布）。
	if n := strings.Count(s, "position: absolute"); n != 2 {
		t.Errorf("滚动模式不该把卡片绝对堆叠（position: absolute 出现 %d 次，期望 2）", n)
	}
}

// TestSlideCSS 全屏分页：原生滚动吸附，一屏一张。
func TestSlideCSS(t *testing.T) {
	p := &Props{Trigger: TriggerSlide, Count: 3}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"overflow-y: auto",
		"scroll-snap-type: y mandatory",
		"scroll-snap-align: start",
		"scroll-snap-stop: always", // 一次手势只翻一屏
		"width: 100%",
		"height: 100dvh",     // 移动端地址栏收放时不跳
		"height: 100vh",      // 老浏览器降级
		"min-height: 100dvh", // 内容超一屏时卡片自己长高，不裁切
		"border-radius: 0",   // 全屏卡片的圆角与投影会露出拼接感
		"box-shadow: none",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("全屏分页缺少 %q", want)
		}
	}
	// 全屏下"放大到视口中央"等于原地不动，不该输出放大层。
	if strings.Contains(s, "sky-cardstack-scrim") {
		t.Errorf("全屏分页不该输出放大层")
	}
}

// TestSlideHighlight 当前屏高亮：挂在脚本切换的 .is-current 类上（普通 animation）。
//
// 为什么不用 animation-timeline: view()：slide 的轨道是**内嵌滚动容器**，实测 view()
// 在该场景下不驱动动画 —— 时间线对象创建成功、进度随滚动变化，但元素计算值恒定不变。
// 这条断言同时守住「别再退回 view() 写法」。
func TestSlideHighlight(t *testing.T) {
	hp := &Props{Trigger: TriggerSlide, Count: 2, SlideHighlight: "glow"}
	hs := compiled(t, nodeOf(hp, 0), hp)
	for _, want := range []string{
		"animation: sky-loop-glow 2s ease-in-out infinite",
		"@keyframes sky-loop-glow",
		".sky-cardstack-card.is-current", // 由脚本按可见比例切换
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("当前屏高亮缺少 %q", want)
		}
	}

	// 两者同时开：各挂各的类，互不覆盖。
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

	// slide 不得再出现 view() 时间线（内嵌滚动容器下不驱动动画）。
	if strings.Contains(bs, "animation-timeline") {
		t.Errorf("slide 不该用 animation-timeline: view()：内嵌滚动容器下实测不驱动动画")
	}

	// 两个都不开时不输出动画规则。
	plain := &Props{Trigger: TriggerSlide, Count: 2}
	if ps := compiled(t, nodeOf(plain, 0), plain); strings.Contains(ps, "is-enter") {
		t.Errorf("都不开时不该输出动画规则")
	}
}

// TestLoopEffects 循环效果：悬停展开与 deck 主卡只走 filter/opacity 两条词汇 ——
// 展开位移与卡片缩放已经占了 transform，swing/wobble/pulse 之类会把位移顶掉。
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

	// 缺省不带任何循环动画。
	plain := &Props{Trigger: TriggerHover, Count: 3}
	if ps := compiled(t, nodeOf(plain, 0), plain); strings.Contains(ps, "sky-loop-") {
		t.Errorf("缺省不该带循环动画")
	}
}

// TestSlideEffect 切换动画：复用通用动效词汇，关键帧随组件一起注入产物；缺省无动画。
func TestSlideEffect(t *testing.T) {
	plain := &Props{Trigger: TriggerSlide, Count: 2}
	ps := compiled(t, nodeOf(plain, 0), plain)
	if strings.Contains(ps, "animation-timeline: view()") {
		t.Errorf("缺省不该带切换动画（只有位置变化）")
	}

	// 每个效果都映射到 core 那套词汇里的真实关键帧名，且关键帧被注入。
	// 方向自适应：纵向走 *-up / -left 变体，横向走 *-right / -y 变体。
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
				"@keyframes " + kf, // 词汇从 core 统一注入，组件不自己造关键帧
			} {
				if !strings.Contains(s, want) {
					t.Errorf("效果 %s（side=%d）缺少 %q", effect, side, want)
				}
			}
		}
	}
}

// TestDeckTransition deck 切换曲线与时长可调（卡片在视口内，走过渡而非入场动画）。
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

// TestSlideScrollbarHidden 滚动条默认隐藏（三种写法覆盖 Firefox / 旧 Edge / WebKit）。
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

// TestSlideHorizontal 横向滚动：整套换到 X 轴（含吸附与 sticky 轴）。
func TestSlideHorizontal(t *testing.T) {
	p := &Props{Trigger: TriggerSlide, Count: 3, SlideDirection: slideDirectionHorizontal, SlideStack: true}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"overflow-x: auto",
		"overflow-y: hidden",
		"scroll-snap-type: x mandatory",
		"left: 0", // sticky 换到 X 轴
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

// TestSlideStack 堆叠翻页：粘在同位 + 递增 z-index，后一张盖住前一张（零 JS）。
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
		"z-index: 3", // 第 3 张盖在最上面
	} {
		if !strings.Contains(ss, want) {
			t.Errorf("堆叠翻页缺少 %q", want)
		}
	}
}

// TestSlideFitViewport 铺满视口：容器脱离文档流，父容器内边距不再影响它。
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
		"height: 100vh", // 降级
	} {
		if !strings.Contains(fs, want) {
			t.Errorf("铺满视口缺少 %q", want)
		}
	}
}

// TestSlideDefaults 全屏分页缺省值：3 屏、宽度占满、自动关闭放大。
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
	// 自定义每屏高度生效，且 dvh 会带一条 vh 降级。
	custom := &Props{Trigger: TriggerSlide, SlideHeight: "80dvh"}
	cs := compiled(t, nodeOf(custom, 0), custom)
	if !strings.Contains(cs, "height: 80dvh") || !strings.Contains(cs, "height: 80vh") {
		t.Errorf("自定义每屏高度应生效并带 vh 降级")
	}
}

// TestDeckCSS 堆叠轮播：几何由每张卡的两个变量驱动，脚本只改写它们。
func TestDeckCSS(t *testing.T) {
	p := &Props{Trigger: TriggerDeck}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"cursor: grab",
		"touch-action: pan-y",
		"--sky-deck-off: -4", // 第 1 张：静态降级值 = i - mid（9 张卡）
		"--sky-deck-abs: 4",
		"translateX(calc(var(--sky-deck-off, 0) * 54%))",
		"rotate(calc(var(--sky-deck-off, 0) * 4deg))",
		"scale(calc(1 - var(--sky-deck-abs, 0) * 0.0600))",
		"z-index: calc(50 - var(--sky-deck-abs, 0))",
		".sky-c-n1 .sky-cardstack-card.is-active",
		"transition: transform 450ms cubic-bezier(.22,.61,.36,1)",
		// 越远越淡：卡片多时不至于在两侧无限堆远（max() 不被支持时退化为全不透明）。
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

// TestDeckView 轮播模式在视图上打标，初始主卡取中间那张（两侧对称叠开）。
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

	// 循环切换：模板据此输出 data-cardstack-loop，脚本用「最短方向」算偏移。
	looped := &Props{Trigger: TriggerDeck, Count: 5, DeckLoop: true}
	v3, err := BuildView(nodeOf(looped, 0), looped, &core.RenderContext{})
	if err != nil {
		t.Fatalf("BuildView(loop): %v", err)
	}
	if !v3.DeckLoop {
		t.Errorf("开启循环切换后应标记 DeckLoop")
	}
}

// TestDragCSS 拖拽旋转：环形几何在构建期算好，增强脚本只改写一个 CSS 变量。
func TestDragCSS(t *testing.T) {
	p := &Props{Trigger: TriggerDrag}
	s := compiled(t, nodeOf(p, 0), p)

	for _, want := range []string{
		"cursor: grab",
		"touch-action: pan-y", // 纵向留给页面滚动，横向才归旋转
		"--sky-cardstack-rot: 0deg",
		// 9 张卡、240px 宽 → 半径 240/(2·sin20°) ≈ 350.86 → 350px
		"transform: translate(-50%, -50%) rotate(calc(0deg + var(--sky-cardstack-rot, 0deg))) translateY(-350px)",
		"rotate(calc(-320deg - var(--sky-cardstack-rot, 0deg)))", // 第 9 张 = 360×8/9
		"transition: transform .35s ease",
		".sky-c-n1.is-dragging .sky-cardstack-card",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("拖拽模式缺少 %q", want)
		}
	}
	// 不该混入其他模式的几何。
	if strings.Contains(s, "position: sticky") || strings.Contains(s, "rotate(-20deg)") {
		t.Errorf("拖拽模式混入了其他模式的几何")
	}
}

// TestDragRadius 环形半径：默认按卡片宽度与数量自动（相邻不重叠），用户值优先。
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
	// 卡数多时半径自动放大（保证不重叠），不会小于下限。
	if got := dragRadius(&Props{}, 16, "240px", "320px"); got < 600 {
		t.Errorf("16 张卡的半径应显著变大，got %v", got)
	}
}

// TestDragView 拖拽模式在视图上打标，模板据此输出 data-* 与键盘可达属性。
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

// TestZoomCSS 点击放大：隐藏但可聚焦的单选 + 放大态 + 遮罩，全部零 JS。
func TestZoomCSS(t *testing.T) {
	s := compiled(t, nodeOf(nil, 0), &Props{})

	for _, want := range []string{
		".sky-cardstack-toggle",
		"pointer-events: none", // 点击穿透到 label
		":has(> .sky-cardstack-toggle:checked)",
		"position: fixed",
		"margin: auto", // inset:0 + margin:auto 居中，不抢 transform
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

// TestCardLayoutParams 卡内布局可配：卡片因此能当容器用（排列方向/间距/对齐）。
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

	// 枚举白名单：非法值退回缺省，绝不透传进 CSS（编译期自防御）。
	bad := compiled(t, nodeOf(nil, 2), &Props{CardLayout: "grid; color: red", CardAlign: "evil"})
	if strings.Contains(bad, "grid; color: red") || strings.Contains(bad, "align-items: evil") {
		t.Errorf("非法枚举值被透传进 CSS")
	}
	if !strings.Contains(bad, "flex-direction: column") || !strings.Contains(bad, "align-items: center") {
		t.Errorf("非法枚举值应退回缺省")
	}
}

// TestContentCards 有子节点即内容卡：不套色相、裁剪溢出、高度按 min-height。
func TestContentCards(t *testing.T) {
	s := compiled(t, nodeOf(nil, 3), &Props{}) // 3 个子节点 → 3 张内容卡

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

// fakeCollection 固定两条文章数据，用于验证集合模式展开与字段映射。
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

// TestCollectionCards 内容集合：卡片数量与卡内字段都由内容决定。
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

	// 取几条
	p.CollectionLimit = 1
	view2, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView(limit=1): %v", err)
	}
	if len(view2.Cards) != 1 {
		t.Errorf("limit=1 应只取 1 条，got %d", len(view2.Cards))
	}
}

// TestCollectionFieldError 字段名写错要在构建期报错并列出可用字段，而不是静默渲染空白。
func TestCollectionFieldError(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "tittle"} // 拼错
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	_, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err == nil {
		t.Fatalf("字段名拼错应报错")
	}
	if !strings.Contains(err.Error(), "可用字段") || !strings.Contains(err.Error(), "title") {
		t.Errorf("报错应列出可用字段：%v", err)
	}
}

// fakeSchemaCollection 在集合解析之外还提供元数据契约（content 模块的形态）。
type fakeSchemaCollection struct{ fakeCollection }

func (fakeSchemaCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{
		Source: "content:article",
		Label:  "文章列表",
		Fields: []string{"title", "excerpt", "featuredImage"},
	}}, nil
}

// TestCollectionSchemaWhitelist 有元数据契约时按白名单严格校验（不变量 4）。
func TestCollectionSchemaWhitelist(t *testing.T) {
	ctx := &core.RenderContext{Collection: fakeSchemaCollection{}}

	// 白名单内 → 通过
	ok := &Props{CollectionSource: "content:article", CardTitleField: "title", CardImageField: "featuredImage"}
	if _, err := BuildView(nodeOf(nil, 0), ok, ctx); err != nil {
		t.Fatalf("白名单内字段应通过：%v", err)
	}

	// 白名单外（body 不在 fake 白名单里）→ 报错并列出可用字段
	bad := &Props{CollectionSource: "content:article", CardTextField: "body"}
	_, err := BuildView(nodeOf(nil, 0), bad, ctx)
	if err == nil {
		t.Fatalf("白名单外字段应报错")
	}
	if !strings.Contains(err.Error(), "白名单") || !strings.Contains(err.Error(), "title") {
		t.Errorf("报错应说明白名单并列出可用字段：%v", err)
	}

	// 未知集合源 → 报错
	_, err = BuildView(nodeOf(nil, 0), &Props{CollectionSource: "content:unknown"}, ctx)
	if err == nil || !strings.Contains(err.Error(), "未知集合源") {
		t.Errorf("未知集合源应报错：%v", err)
	}

	// 裁剪：模板只能渲染白名单字段（未声明字段被丢弃）
	view, err := BuildView(nodeOf(nil, 0), ok, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Cards) != 2 || view.Cards[0].Title != "第一篇文章" {
		t.Errorf("裁剪后仍应正常出卡：%+v", view.Cards)
	}
}

// TestCollectionMissingResolver 未注入集合解析器时给出明确提示。
func TestCollectionMissingResolver(t *testing.T) {
	p := &Props{CollectionSource: "content:article"}
	if _, err := BuildView(nodeOf(nil, 0), p, &core.RenderContext{}); err == nil {
		t.Fatalf("缺少集合解析器应报错")
	}
}

// TestCollectionCSS 集合模式：既走内容卡样式，逐卡规则数量也与内容条数一致。
func TestCollectionCSS(t *testing.T) {
	p := &Props{CollectionSource: "content:article", CardTitleField: "title"}
	n := nodeOf(nil, 0)
	b := &core.CSSBuckets{}
	CompileCSS(n, p, 2, b) // 解析出 2 条内容
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

// emptyCollection 空集合（后台还没有内容时的真实形态）。
type emptyCollection struct{}

func (emptyCollection) ResolveCollection(_ context.Context, _ string, _ map[string]string) ([]map[string]any, error) {
	return nil, nil
}

func (emptyCollection) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{Source: "content:article", Label: "文章列表", Fields: []string{"title"}}}, nil
}

// TestCollectionEmptyState 集合无内容：渲染占位或整体隐藏，而不是留一片空白。
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

	// 也可以选择整体隐藏（列表页里"没有内容就什么都不显示"）。
	hidden := &Props{CollectionSource: "content:article", CollectionEmpty: "hide"}
	v2, _ := BuildView(nodeOf(nil, 0), hidden, &core.RenderContext{Collection: emptyCollection{}})
	if !v2.HideEmpty {
		t.Errorf("collectionEmpty=hide 应标记 HideEmpty")
	}
}

// TestBuiltinTextOverridable 内置文案可配（多语言站点不必改代码）。
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

	// 不配时用内置缺省。
	def, _ := BuildView(nodeOf(nil, 0), &Props{CollectionSource: "content:article"}, ctx)
	if def.LinkText != defaultCardLinkText {
		t.Errorf("缺省链接文案应为 %q，got %q", defaultCardLinkText, def.LinkText)
	}
}

// TestBuildView 渲染视图：卡片数量随内容来源与占位数量变化。
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
