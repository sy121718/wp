package marquee

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestMarqueeCSSPerInstanceKeyframes 每个实例一份帧名，且名字与动画引用一致。
//
// 这不是装饰：帧名带实例 id 才让同页多个跑马灯各有各的速度与方向。
// 名字里的 {{id}} 若没展开，产物里会多出一个谁都不引用的关键帧 —— 动画直接不动。
func TestMarqueeCSSPerInstanceKeyframes(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("abc", &Props{Speed: 6}, &b)
	out := b.String()
	if !strings.Contains(out, "@keyframes sky-marquee-abc {") {
		t.Errorf("帧名应带实例 id:\n%s", out)
	}
	if !strings.Contains(out, "animation: sky-marquee-abc 6s linear infinite;") {
		t.Errorf("动画引用与帧名不一致:\n%s", out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("产物里残留占位:\n%s", out)
	}
}

// TestMarqueeCSSDirection 方向决定帧的起止值（左移 0 → -50%，右移反之）。
func TestMarqueeCSSDirection(t *testing.T) {
	var left core.CSSBuckets
	compileCSS("t", &Props{}, &left)
	if !strings.Contains(left.String(), "from { transform: translateX(0) }") {
		t.Errorf("默认向左：起点应为 0:\n%s", left.String())
	}

	var right core.CSSBuckets
	compileCSS("t", &Props{Direction: DirRight}, &right)
	out := right.String()
	if !strings.Contains(out, "from { transform: translateX(-50%) }") || !strings.Contains(out, "to { transform: translateX(0) }") {
		t.Errorf("向右：起止值应对调:\n%s", out)
	}
}

// TestMarqueeCSSOptionalRules 悬停暂停 / 背景 / 内边距都是可选规则，缺失即不产出。
func TestMarqueeCSSOptionalRules(t *testing.T) {
	var plain core.CSSBuckets
	compileCSS("t", &Props{}, &plain)
	out := plain.String()
	if strings.Contains(out, "animation-play-state") {
		t.Errorf("未开悬停暂停时不该产出该规则:\n%s", out)
	}
	if strings.Count(out, ".sky-c-t {") != 1 {
		t.Errorf("未设背景时不该多出一条容器规则:\n%s", out)
	}
	if strings.Contains(out, "padding-top") {
		t.Errorf("未设内边距时不该产出该规则:\n%s", out)
	}
	// 间距兜底 24px（宽度决定的两倍，接缝才看不出）。
	if !strings.Contains(out, "gap: 24px;") || !strings.Contains(out, "padding-right: 24px;") {
		t.Errorf("间距未兜底 24px:\n%s", out)
	}

	var full core.CSSBuckets
	compileCSS("t", &Props{PauseOnHover: true, Background: "#eee", Padding: "8px", Gap: "32px"}, &full)
	fullOut := full.String()
	for _, want := range []string{
		"animation-play-state: paused;",
		"background: #eee;",
		"padding-top: 8px;",
		"padding-bottom: 8px;",
		"gap: 32px;",
	} {
		if !strings.Contains(fullOut, want) {
			t.Errorf("产物缺少 %q\n%s", want, fullOut)
		}
	}
}
