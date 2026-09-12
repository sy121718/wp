package core

import (
	"strings"
	"testing"
)

// TestKeyframeSourcesBalanced 关键帧源里每个块的花括号必须平衡。
//
// animate.css 的 33 个块曾经每个都多一个 `}`（从 Go 常量迁出时带过来的）。
// 它不止是丑：Chromium 实测会吞掉**紧随其后的那一整条规则** —— 而产物里关键帧区
// 之后紧跟的正是桌面规则，等于「用了某个动效的页面会静默少一条组件样式」。
// 静态校验比事后在浏览器里发现便宜得多。
func TestKeyframeSourcesBalanced(t *testing.T) {
	sources := []struct{ name, body string }{
		{"keyframes/builtin.css", builtinKeyframesSource},
		{"keyframes/animate.css", animateKeyframesSource},
	}
	for _, src := range sources {
		ks, err := ParseKeyframeCSS(src.body)
		if err != nil {
			t.Fatalf("%s: %v", src.name, err)
		}
		for _, k := range ks {
			open, closed := strings.Count(k.CSS, "{"), strings.Count(k.CSS, "}")
			if open != closed {
				t.Errorf("%s 的 %s 花括号不平衡：{ %d 个、} %d 个 —— 多出的 } 会吞掉紧随其后的一条规则",
					src.name, k.Name, open, closed)
			}
		}
	}
}

// TestKeyframesSourceParsed 关键帧 CSS 源的解析正确性。
//
// 背景：63 条关键帧的 CSS 从 Go 字符串字面量迁到 keyframes/*.css，中间多了一层解析。
// 解析器写错（截短、错位、漏块）会让产物里的动画变形或消失，而这类缺陷在页面上
// 往往只表现为「动画有点怪」，很难追。所以这里把结构与对应关系都钉住。
func TestKeyframesSourceParsed(t *testing.T) {
	if len(keyframesCatalog) < 60 {
		t.Fatalf("关键帧数量异常: %d（迁移前为 63）", len(keyframesCatalog))
	}
	seen := map[string]bool{}
	for _, k := range keyframesCatalog {
		if strings.TrimSpace(k.CSS) == "" {
			t.Errorf("关键帧 %s 的 CSS 为空", k.Name)
		}
		// 名字与规则体必须对应：切片按 @keyframes 边界划分，错位会在这里暴露。
		if !strings.HasPrefix(k.CSS, "@keyframes "+k.Name) {
			t.Errorf("关键帧 %s 的规则体不对应（开头为 %q）", k.Name, firstLine(k.CSS))
		}
		// 规则体必须以闭合花括号收尾（允许历史源里的多余花括号，但不允许缺）。
		if !strings.HasSuffix(strings.TrimSpace(k.CSS), "}") {
			t.Errorf("关键帧 %s 的规则体没有闭合: %q", k.Name, firstLine(k.CSS))
		}
		if seen[k.Name] {
			t.Errorf("关键帧重名: %s", k.Name)
		}
		seen[k.Name] = true
	}
	// 抽查几个动效词汇确实在（入场 / 循环 / 拆解组 / 滚动叙事各自代表）。
	for _, name := range []string{"sky-fade-up", "sky-loop-flash", "sky-story-zoom"} {
		if !seen[name] {
			t.Errorf("缺少动效词汇: %s", name)
		}
	}
}

// TestParseKeyframeCSSKeepsBraces 解析必须字节保真，包括历史源里多余的闭合花括号。
//
// 迁移时实测：内置组有若干条以 `}}` 结尾（浏览器容忍，但属于既有写法）。
// 花括号计数法的解析器会在那里截短；本测试钉住「原样保留」这个要求。
func TestParseKeyframeCSSKeepsBraces(t *testing.T) {
	src := "@keyframes a {\n  from { opacity: 0 }\n}}\n\n@keyframes b {\n  to { opacity: 1 }\n}\n"
	ks, err := ParseKeyframeCSS(src)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(ks) != 2 {
		t.Fatalf("应解析出 2 条，得到 %d", len(ks))
	}
	if ks[0].Name != "a" || ks[1].Name != "b" {
		t.Fatalf("名字错位: %s / %s", ks[0].Name, ks[1].Name)
	}
	if !strings.HasSuffix(ks[0].CSS, "}}") {
		t.Errorf("多余的闭合花括号被截掉了（会破坏字节保真）: %q", ks[0].CSS)
	}
	if strings.HasSuffix(ks[1].CSS, "}}") {
		t.Errorf("第二条本就没有多余花括号，却被加上了: %q", ks[1].CSS)
	}
}
