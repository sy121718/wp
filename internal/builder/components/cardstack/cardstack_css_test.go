package cardstack

import (
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// cssOf 编译给定 props 的实例并返回完整 CSS（含 hover / active / 容器查询出口）。
func cssOf(p *Props, childN, cardN int) string {
	b := &core.CSSBuckets{}
	CompileCSS(nodeOf(p, childN), p, cardN, b)
	return b.String()
}

// mediaBlocks 提取产物中每个完整媒体查询块（按花括号配平），用于逐块断言：
// hover 与 hovernone 的规则在同一条产物流里交替出现，取「从 marker 起到文末」会把
// 两种媒体查询混在一起，断言就会命中隔壁块的内容。
func mediaBlocks(out, marker string) []string {
	var blocks []string
	rest := out
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			return blocks
		}
		rest = rest[i:]
		depth, end := 0, -1
		for j := 0; j < len(rest) && end < 0; j++ {
			switch rest[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
		}
		if end < 0 {
			return blocks
		}
		blocks = append(blocks, rest[:end+1])
		rest = rest[end+1:]
	}
}

// fromMarker 取产物中从 marker 起的片段（marker 不存在时返回空串）。
func fromMarker(out, marker string) string {
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	return out[i:]
}

// TestCardstackCSSHoverNoneMirror 触屏没有悬停：@hover 的每一条展开规则都必须给出
// (hover: none) 等价形态，且等价形态的选择器里不能带 :hover —— 带上去在触屏上依然不匹配，
// 等于没写（手机端卡片会一直叠着，文字互相透出来）。
func TestCardstackCSSHoverNoneMirror(t *testing.T) {
	out := cssOf(&Props{}, 0, 0)
	if !strings.Contains(out, "@media (hover: hover) {") {
		t.Fatalf("悬停展开缺少 (hover: hover) 桶:\n%s", out)
	}
	hoverBlocks := mediaBlocks(out, "@media (hover: hover) {")
	noneBlocks := mediaBlocks(out, "@media (hover: none) {")
	if len(noneBlocks) == 0 {
		t.Fatalf("触屏等价形态缺失:\n%s", out)
	}
	if len(noneBlocks) != len(hoverBlocks) {
		t.Errorf("悬停规则 %d 条、触屏等价 %d 条，两者必须一一对应", len(hoverBlocks), len(noneBlocks))
	}
	if len(noneBlocks) != 9 {
		t.Errorf("触屏等价规则 %d 条，want 9（与缺省卡片数一致）", len(noneBlocks))
	}
	for _, b := range noneBlocks {
		if strings.Contains(b, ":hover") {
			t.Errorf("(hover: none) 里的选择器不得含 :hover（触屏永不匹配）:\n%s", b)
		}
		if !strings.Contains(b, "clamp(0px,") {
			t.Errorf("触屏等价形态必须用带 clamp 收敛的位移（窄屏不横向溢出）:\n%s", b)
		}
	}
	if strings.Count(strings.Join(noneBlocks, ""), ".sky-c-n1 .sky-cardstack-track .sky-cardstack-card:nth-child(") != 9 {
		t.Errorf("触屏等价规则的选择器形状不对")
	}
}

// TestCardstackCSSHoverScopeOnce :hover 挂在容器上、经轨道下到卡片；选择器里作用域前缀
// 只能出现一次 —— 写成「容器:hover 容器 轨道 …」是永不匹配的选择器，整块样式静默失效。
func TestCardstackCSSHoverScopeOnce(t *testing.T) {
	hover := fromMarker(cssOf(&Props{}, 0, 0), "@media (hover: hover) {")
	if hover == "" {
		t.Fatalf("缺少悬停桶")
	}
	if strings.Contains(hover, ".sky-c-n1:hover .sky-c-n1") {
		t.Errorf("悬停选择器重复拼了作用域前缀:\n%s", hover)
	}
	var line string
	for _, l := range strings.Split(hover, "\n") {
		if s := strings.TrimSpace(l); strings.HasPrefix(s, ".sky-c-n1:hover") {
			line = s
			break
		}
	}
	if line == "" {
		t.Fatalf("找不到容器悬停规则:\n%s", hover)
	}
	if n := strings.Count(line, ".sky-c-n1"); n != 1 {
		t.Errorf("悬停选择器里作用域前缀出现 %d 次，want 1：%s", n, line)
	}
}

// TestCardstackCSSZoomSwitch 点击放大整块随 Zoom 存废；slide 模式恒不产出
// （卡片本来就占满一屏，再放大等于原地不动）。
func TestCardstackCSSZoomSwitch(t *testing.T) {
	on := cssOf(&Props{}, 0, 0)
	for _, want := range []string{".sky-cardstack-toggle", ".sky-cardstack-scrim", ".sky-cardstack-close-btn"} {
		if !strings.Contains(on, want) {
			t.Errorf("默认开启点击放大却缺少 %q", want)
		}
	}
	off := cssOf(&Props{Zoom: "off"}, 0, 0)
	// 卡片选择器里的 :not(:has(> .sky-cardstack-toggle:checked)) 是悬停展开自带的，
	// 不算放大层；这里按「整条规则」匹配（选择器后跟 {）。
	for _, notWant := range []string{
		".sky-c-n1 .sky-cardstack-toggle {",
		".sky-c-n1 .sky-cardstack-close {",
		".sky-c-n1 .sky-cardstack-scrim {",
		".sky-c-n1 .sky-cardstack-close-btn {",
		".sky-c-n1:has(.sky-cardstack-toggle:checked) .sky-cardstack-scrim {",
	} {
		if strings.Contains(off, notWant) {
			t.Errorf("Zoom=off 时不该产出规则 %q:\n%s", notWant, off)
		}
	}
	for _, want := range []string{
		".sky-c-n1 .sky-cardstack-toggle {",
		".sky-c-n1 .sky-cardstack-card:has(> .sky-cardstack-toggle:checked) {",
		".sky-c-n1 .sky-cardstack-close-btn {",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("开启点击放大却缺少规则 %q", want)
		}
	}
	slide := cssOf(&Props{Trigger: TriggerSlide, Zoom: "on"}, 0, 0)
	if strings.Contains(slide, ".sky-cardstack-scrim {") {
		t.Errorf("slide 模式不该产出放大层:\n%s", slide)
	}
}

// TestCardstackCSSCardRulesFollowCount 逐卡循环的展开条数必须等于卡片数：
// 多了是列表填错，少了是 @each 块被提前截断（后半段规则凭空消失）。
func TestCardstackCSSCardRulesFollowCount(t *testing.T) {
	cases := []struct {
		name   string
		p      *Props
		childN int
		cardN  int
		want   int
	}{
		{"hover 子节点数", &Props{}, 3, 0, 3},
		{"scroll 解析条数", &Props{Trigger: TriggerScroll}, 0, 2, 2},
		{"drag 子节点数", &Props{Trigger: TriggerDrag}, 4, 0, 4},
		{"deck 解析条数", &Props{Trigger: TriggerDeck}, 0, 2, 2},
		{"slide 解析条数", &Props{Trigger: TriggerSlide}, 0, 3, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := cssOf(c.p, c.childN, c.cardN)
			if !strings.Contains(out, ":nth-child("+strconv.Itoa(c.want)+")") {
				t.Errorf("缺少第 %d 张卡的规则", c.want)
			}
			if strings.Contains(out, ":nth-child("+strconv.Itoa(c.want+1)+")") {
				t.Errorf("产出了第 %d 张卡的规则（超出卡片数）", c.want+1)
			}
		})
	}
}

// TestCardstackCSSEmptyOptionalLeavesNothing 可选项为空时整条声明省略：
// 不留 "animation: ;" 这类无效声明，也不登记用不到的关键帧。
func TestCardstackCSSEmptyOptionalLeavesNothing(t *testing.T) {
	plain := cssOf(&Props{}, 0, 0)
	if strings.Contains(plain, "animation:") {
		t.Errorf("未选悬停效果时不该产出 animation 声明:\n%s", plain)
	}
	if strings.Contains(plain, "sky-loop-glow") || strings.Contains(plain, "sky-loop-flash") {
		t.Errorf("未选悬停效果时不该登记循环关键帧")
	}
	for _, bad := range []string{"animation: ;", "transform: ;", "color: ;", "width: ;", "height: ;", "border:  solid"} {
		if strings.Contains(plain, bad) {
			t.Errorf("产物里出现无效声明 %q（变量为空时应整条省略）", bad)
		}
	}

	glow := cssOf(&Props{HoverEffect: "glow"}, 0, 0)
	if !strings.Contains(glow, "animation: sky-loop-glow 2s ease-in-out infinite;") {
		t.Errorf("选了发光却没产出循环动画:\n%s", glow)
	}
	if !strings.Contains(glow, "@keyframes sky-loop-glow") {
		t.Errorf("循环动画引用的关键帧没登记（动画永远不动）")
	}
}

// TestCardstackCSSTriggerBranchesIsolated 一种触发方式不得产出另一种的专属规则：
// 未命中分支的输出进临时桶被丢弃，漏了就会「同一个组件同时长出两套形态」。
func TestCardstackCSSTriggerBranchesIsolated(t *testing.T) {
	scroll := cssOf(&Props{Trigger: TriggerScroll}, 0, 0)
	slide := cssOf(&Props{Trigger: TriggerSlide}, 0, 0)
	deck := cssOf(&Props{Trigger: TriggerDeck}, 0, 0)

	for _, tc := range []struct{ name, out, notWant string }{
		{"scroll 不含触屏悬停等价形态", scroll, "@media (hover: none)"},
		{"scroll 不含全屏分页页码", scroll, ".sky-cardstack-page"},
		{"slide 不含触屏悬停等价形态", slide, "@media (hover: none)"},
		{"slide 不含堆叠轮播主卡规则", slide, ".sky-cardstack-card.is-active"},
		{"deck 不含全屏分页页码", deck, ".sky-cardstack-page"},
	} {
		if strings.Contains(tc.out, tc.notWant) {
			t.Errorf("%s：不该出现 %q", tc.name, tc.notWant)
		}
	}
	// 拖拽与堆叠轮播的容器可聚焦（键盘 Tab 可达），焦点环是这两个模式的专属。
	if !strings.Contains(deck, ":focus-visible") {
		t.Errorf("deck 模式缺少容器焦点环（键盘够不到）")
	}
	if strings.Contains(scroll, ":focus-visible") {
		t.Errorf("scroll 模式不该产出容器焦点环")
	}
	// 页码的计数复位与自增只属于 slide。
	if !strings.Contains(slide, "counter-reset: sky-page;") || !strings.Contains(slide, "counter-increment: sky-page;") {
		t.Errorf("slide 缺少 CSS counter 页码:\n%s", slide)
	}
}

// TestCardstackCSSCollectionBlocks 集合相关规则整块随 collectionSource 存废。
func TestCardstackCSSCollectionBlocks(t *testing.T) {
	plain := cssOf(&Props{}, 0, 0)
	for _, notWant := range []string{".sky-cardstack-empty", ".is-empty-hidden", ".sky-cardstack-title"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("静态卡片模式不该产出集合规则 %q", notWant)
		}
	}
	coll := cssOf(&Props{CollectionSource: "content:article"}, 0, 2)
	for _, want := range []string{
		".sky-c-n1 .sky-cardstack-img",
		".sky-c-n1 .sky-cardstack-title",
		".sky-c-n1 .sky-cardstack-text",
		".sky-c-n1 .sky-cardstack-meta",
		".sky-c-n1 .sky-cardstack-link",
		".sky-c-n1 .sky-cardstack-empty",
		".sky-c-n1.is-empty-hidden",
	} {
		if !strings.Contains(coll, want+" {") {
			t.Errorf("集合模式缺少 %q 规则:\n%s", want, coll)
		}
	}
}

// TestCardstackCSSSlideHeightFallback dvh 的降级链必须「旧值在前、新值在后」：
// 反过来 dvh 会被 vh 永久盖掉，等于白写（移动端地址栏收放时仍会跳）。
func TestCardstackCSSSlideHeightFallback(t *testing.T) {
	out := cssOf(&Props{Trigger: TriggerSlide, SlideHeight: "100dvh"}, 0, 0)
	i := strings.Index(out, "height: 100vh;")
	j := strings.Index(out, "height: 100dvh;")
	if i < 0 || j < 0 {
		t.Fatalf("dvh 降级链缺失（vh 兜底或 dvh 本身）:\n%s", out)
	}
	if i > j {
		t.Errorf("降级链顺序反了：vh 必须写在 dvh 之前")
	}
	if !strings.Contains(out, "min-height: 100vh;") || !strings.Contains(out, "min-height: 100dvh;") {
		t.Errorf("卡片的每屏最小高度也要走同一条降级链")
	}
	plain := cssOf(&Props{Trigger: TriggerSlide, SlideHeight: "100vh"}, 0, 0)
	if strings.Contains(plain, "dvh") {
		t.Errorf("非 dvh 高度不该产出 dvh 分支")
	}
}

// TestCardstackCSSDeckTouchActionFollowsAxis 切换方向的轴归脚本，另一个轴让给页面滚动，
// 否则触摸手势被浏览器拿去滚页面，滑动切换在触屏上完全失效。
func TestCardstackCSSDeckTouchActionFollowsAxis(t *testing.T) {
	if h := cssOf(&Props{Trigger: TriggerDeck}, 0, 0); !strings.Contains(h, "touch-action: pan-y;") {
		t.Errorf("横向切换应把纵向留给页面（pan-y）:\n%s", h)
	}
	if v := cssOf(&Props{Trigger: TriggerDeck, DeckDirection: "vertical"}, 0, 0); !strings.Contains(v, "touch-action: pan-x;") {
		t.Errorf("纵向切换应把横向留给页面（pan-x）:\n%s", v)
	}
}

// TestCardstackCSSCardSource 内容卡与数字卡是两套外观：数字卡巨字号居中 + 位置派生色相，
// 内容卡要读得下正文（内边距 / 圆角兜底），两者不得互相串味。
func TestCardstackCSSCardSource(t *testing.T) {
	num := cssOf(&Props{}, 0, 0)
	if !strings.Contains(num, "font-size: 8em;") || !strings.Contains(num, "filter: hue-rotate(") {
		t.Errorf("数字占位卡缺少巨字号 / 位置派生色相:\n%s", num)
	}
	if strings.Contains(num, "padding: 24px;") {
		t.Errorf("数字卡不该有内容卡的内边距")
	}
	content := cssOf(&Props{}, 2, 0)
	if !strings.Contains(content, "padding: 24px;") || !strings.Contains(content, "border-radius: 16px;") {
		t.Errorf("内容卡缺少内边距 / 圆角兜底:\n%s", content)
	}
	if strings.Contains(content, "font-size: 8em;") || strings.Contains(content, "filter: hue-rotate(") {
		t.Errorf("内容卡不该产出数字卡的巨字号 / 色相:\n%s", content)
	}
}

// TestCardstackCSSScrollKeyframesPerCard 滚动堆叠每张卡一份独立关键帧，名字含 node id
// （共用一份会让所有卡停在同一个缩放上，跟手收敛就没了；两个实例也会互相串帧）。
func TestCardstackCSSScrollKeyframesPerCard(t *testing.T) {
	out := cssOf(&Props{Trigger: TriggerScroll}, 0, 3)
	if got := strings.Count(out, "@keyframes sky-cs-n1-"); got != 3 {
		t.Errorf("关键帧条数 %d，want 3（每卡一份）", got)
	}
	for i := 1; i <= 3; i++ {
		name := "sky-cs-n1-" + strconv.Itoa(i)
		if !strings.Contains(out, "animation: "+name+" linear both;") {
			t.Errorf("第 %d 张卡没有引用自己的关键帧 %s", i, name)
		}
	}
}
