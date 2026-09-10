package heading

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestSplitTextSegments 逐字/逐词切分：chars 按 rune、words 保留尾随空格、空值回退 nil。
func TestSplitTextSegments(t *testing.T) {
	tests := []struct {
		name string
		text string
		mode string
		want []string
	}{
		{"空文本返回 nil", "", "chars", nil},
		{"逐字中文按 rune", "标题", "chars", []string{"标", "题"}},
		{"逐字英文按字母", "ab", "chars", []string{"a", "b"}},
		{"逐字含空格保留", "a b", "chars", []string{"a", " ", "b"}},
		{"逐词保留尾随空格", "hello world", "words", []string{"hello ", "world"}},
		{"逐词无空格单段", "标题文本", "words", []string{"标题文本"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTextSegments(tt.text, tt.mode)
			if len(got) != len(tt.want) {
				t.Fatalf("splitTextSegments(%q,%q) 段数 = %d, want %d（实际 %q）", tt.text, tt.mode, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("段 %d = %q, want %q（全部 %q）", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

// TestSplitTextSegmentsLimit 分段上限 maxTextSegments：恰好上限可用，超限回退 nil（模板整段输出）。
func TestSplitTextSegmentsLimit(t *testing.T) {
	atLimit := strings.Repeat("a", maxTextSegments)
	if got := splitTextSegments(atLimit, "chars"); len(got) != maxTextSegments {
		t.Errorf("恰好 %d 段应拆分，实际 %d 段", maxTextSegments, len(got))
	}
	overLimit := strings.Repeat("a", maxTextSegments+1)
	if got := splitTextSegments(overLimit, "chars"); got != nil {
		t.Errorf("超出上限应返回 nil，实际 %d 段", len(got))
	}
}

// TestBuildViewTextAnim 视图层拆分：chars/words 生效、高亮盒与关闭态不拆分。
func TestBuildViewTextAnim(t *testing.T) {
	t.Run("逐字", func(t *testing.T) {
		v, err := BuildView(&Props{Text: "标题", TextAnim: "chars"}, nil)
		if err != nil {
			t.Fatalf("BuildView 报错: %v", err)
		}
		if v.Text != "标题" || len(v.Segments) != 2 {
			t.Fatalf("Segments 段数 = %d, want 2（实际 %q）", len(v.Segments), v.Segments)
		}
		if v.Segments[0] != "标" || v.Segments[1] != "题" {
			t.Errorf("Segments = %q, want 标 + 题", v.Segments)
		}
	})
	t.Run("逐词", func(t *testing.T) {
		v, err := BuildView(&Props{Text: "Hello World", TextAnim: "words"}, nil)
		if err != nil {
			t.Fatalf("BuildView 报错: %v", err)
		}
		if len(v.Segments) != 2 || v.Segments[0] != "Hello " {
			t.Errorf("Segments = %q, want 两段且首段为 Hello 加空格", v.Segments)
		}
	})
	t.Run("高亮盒不拆分", func(t *testing.T) {
		v, err := BuildView(&Props{Text: "标题", TextAnim: "chars", HighlightColor: "#ff0"}, nil)
		if err != nil {
			t.Fatalf("BuildView 报错: %v", err)
		}
		if !v.Highlight {
			t.Error("HighlightColor 非空时 Highlight 应为 true")
		}
		if v.Segments != nil {
			t.Errorf("高亮盒模式不应拆分，实际 %q", v.Segments)
		}
	})
	t.Run("未开启不拆分", func(t *testing.T) {
		v, err := BuildView(&Props{Text: "标题"}, nil)
		if err != nil {
			t.Fatalf("BuildView 报错: %v", err)
		}
		if v.Segments != nil {
			t.Errorf("TextAnim 为空不应拆分，实际 %q", v.Segments)
		}
	})
}

// TestCompileCSSTextAnim 文本动画 CSS：分段基础规则 + 递增延迟 + 第 21 段兜底档 + keyframes 激活。
func TestCompileCSSTextAnim(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{Text: "标题动画", TextAnim: "chars", TextAnimDelay: 40}, b)
	css := b.String()

	base := cssBlock(css, ".sky-c-n1 .sky-h-seg")
	for _, want := range []string{
		"display: inline-block",
		"white-space: pre",
		"animation: sky-fade-up 0.6s ease backwards",
	} {
		if !strings.Contains(base, want) {
			t.Errorf("分段基础规则缺少 %q，实际 %q", want, base)
		}
	}

	// 逐段递增：第 1 段 0ms、第 3 段 2*delay、第 20 段 19*delay，第 21 段起统一兜底档。
	for _, c := range []struct {
		nth  string
		decl string
	}{
		{".sky-c-n1 .sky-h-seg:nth-child(1)", "animation-delay: 0ms"},
		{".sky-c-n1 .sky-h-seg:nth-child(3)", "animation-delay: 80ms"},
		{".sky-c-n1 .sky-h-seg:nth-child(20)", "animation-delay: 760ms"},
		{".sky-c-n1 .sky-h-seg:nth-child(n+21)", "animation-delay: 800ms"},
	} {
		if !strings.Contains(css, c.nth+" {") {
			t.Errorf("缺少规则 %q", c.nth)
			continue
		}
		if !strings.Contains(cssBlock(css, c.nth), c.decl) {
			t.Errorf("%q 缺少 %q，实际 %q", c.nth, c.decl, cssBlock(css, c.nth))
		}
	}

	if !strings.Contains(css, "@keyframes sky-fade-up") {
		t.Error("NeedKeyframes 未激活 @keyframes sky-fade-up")
	}
}

// TestCompileCSSTextAnimDelay 字间延迟：未设或非法（<=0）回退默认 40ms；显式值按值递增。
func TestCompileCSSTextAnimDelay(t *testing.T) {
	for _, tt := range []struct {
		name  string
		delay int
		want  string
	}{
		{"未设回退 40ms", 0, "animation-delay: 120ms"},  // 第 4 段 = 3*40
		{"负数回退 40ms", -5, "animation-delay: 120ms"}, // 第 4 段 = 3*40
		{"显式 100ms", 100, "animation-delay: 300ms"}, // 第 4 段 = 3*100
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := &core.CSSBuckets{}
			compileCSS("n1", &Props{Text: "标题 文本", TextAnim: "words", TextAnimDelay: tt.delay}, b)
			sel := ".sky-c-n1 .sky-h-seg:nth-child(4)"
			if got := cssBlock(b.String(), sel); !strings.Contains(got, tt.want) {
				t.Errorf("delay=%d：%q 期望 %q，实际 %q", tt.delay, sel, tt.want, got)
			}
		})
	}
}

// TestCompileCSSTextAnimOff 未开启文本动画：不产出分段规则，也不激活关键帧。
func TestCompileCSSTextAnimOff(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{Text: "标题"}, b)
	css := b.String()
	if strings.Contains(css, "sky-h-seg") {
		t.Errorf("未开启文本动画不应输出分段规则，实际 %q", css)
	}
	if strings.Contains(css, "@keyframes sky-fade-up") {
		t.Error("未开启文本动画不应激活 @keyframes sky-fade-up")
	}
}
