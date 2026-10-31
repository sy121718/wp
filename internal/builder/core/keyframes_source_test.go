package core

import (
	"strings"
	"testing"
)

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

func TestKeyframesSourceParsed(t *testing.T) {
	if len(keyframesCatalog) < 60 {
		t.Fatalf("关键帧数量异常: %d（迁移前为 63）", len(keyframesCatalog))
	}
	seen := map[string]bool{}
	for _, k := range keyframesCatalog {
		if strings.TrimSpace(k.CSS) == "" {
			t.Errorf("关键帧 %s 的 CSS 为空", k.Name)
		}

		if !strings.HasPrefix(k.CSS, "@keyframes "+k.Name) {
			t.Errorf("关键帧 %s 的规则体不对应（开头为 %q）", k.Name, firstLine(k.CSS))
		}

		if !strings.HasSuffix(strings.TrimSpace(k.CSS), "}") {
			t.Errorf("关键帧 %s 的规则体没有闭合: %q", k.Name, firstLine(k.CSS))
		}
		if seen[k.Name] {
			t.Errorf("关键帧重名: %s", k.Name)
		}
		seen[k.Name] = true
	}

	for _, name := range []string{"sky-fade-in-up", "sky-loop-flash", "sky-story-zoom"} {
		if !seen[name] {
			t.Errorf("缺少动效词汇: %s", name)
		}
	}
}

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
