package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	reCSSCommentBlock = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reKeyframeDefName = regexp.MustCompile(`@keyframes\s+([^\s]+)`)
)

var templateKeyframeNames = map[string]string{
	"{{card.kf}}":                "sky-cs-<id>-<n>（cardstack.go keyframesName）",
	"sky-marquee-{{id}}":         "sky-marquee-<id>（marquee 每实例一份）",
	"sky-bg-fade-{{slideCount}}": "sky-bg-fade-<n>（container 每张数一份）",
}

var runtimeKeyframePrefixes = []string{"sky-cs-", "sky-marquee-", "sky-bg-fade-"}

var vocabKeyframeSource = []string{
	"../builder/core/keyframes/builtin.css",
	"../builder/core/keyframes/animate.css",
}

func TestComponentKeyframesDoNotShadowVocabulary(t *testing.T) {
	vocab := readKeyframeNames(t, vocabKeyframeSource...)

	if len(vocab) != 61 {
		t.Fatalf("动效词汇表解析出 %d 条（预期 61）：解析口径可能已失效", len(vocab))
	}
	vocabSet := make(map[string]bool, len(vocab))
	for _, n := range vocab {
		vocabSet[n] = true
	}

	files, err := filepath.Glob("../builder/components/*/*.css")
	if err != nil {
		t.Fatalf("枚举组件 CSS 失败: %v", err)
	}
	if len(files) < 30 {
		t.Fatalf("只找到 %d 个组件 CSS（预期 ≥30）：路径口径可能已变", len(files))
	}

	var literal, templated []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}

		body := reCSSCommentBlock.ReplaceAllString(string(raw), "")
		for _, m := range reKeyframeDefName.FindAllStringSubmatch(body, -1) {
			name := strings.TrimSuffix(m[1], "{")
			if strings.Contains(name, "{{") {
				templated = append(templated, name)
				continue
			}
			literal = append(literal, name)
		}
	}
	if len(literal)+len(templated) < 15 {
		t.Fatalf("组件私有 @keyframes 只解析出 %d 处（预期 ≥15）：解析口径可能已失效",
			len(literal)+len(templated))
	}

	var shadowed []string
	for _, n := range literal {
		if vocabSet[n] {
			shadowed = append(shadowed, n)
		}
	}
	sort.Strings(shadowed)
	if len(shadowed) > 0 {
		t.Errorf("组件私有帧与共享词汇表同名（产物里两份定义会互相覆盖）: %v", shadowed)
	}

	registered := 0
	for _, n := range templated {
		ns, ok := templateKeyframeNames[n]
		if !ok {
			t.Errorf("组件 CSS 出现未登记的模板变量帧名 %q：请在 templateKeyframeNames 登记它的运行时命名空间", n)
			continue
		}
		if ns == "" {
			t.Errorf("模板变量帧名 %q 登记了空命名空间说明", n)
			continue
		}
		registered++
	}

	for _, p := range runtimeKeyframePrefixes {
		for _, n := range vocab {
			if strings.HasPrefix(n, p) {
				t.Errorf("词汇表条目 %q 落在组件运行时命名空间 %q 里：运行期会与组件私有帧同名", n, p)
			}
		}
	}

	t.Logf("组件私有关键帧 %d 处（字面量 %d + 模板 %d），其中已登记命名空间 %d 项；共享词汇表 %d 条",
		len(literal)+len(templated), len(literal), len(templated), registered, len(vocab))
	sort.Strings(literal)
	sort.Strings(templated)
	t.Logf("字面量私有帧: %v", literal)
	t.Logf("模板私有帧: %v", templated)
}

func readKeyframeNames(t *testing.T, paths ...string) []string {
	t.Helper()
	var out []string
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取关键帧源 %s 失败: %v", p, err)
		}
		body := reCSSCommentBlock.ReplaceAllString(string(raw), "")
		for _, m := range reKeyframeDefName.FindAllStringSubmatch(body, -1) {
			out = append(out, strings.TrimSuffix(m[1], "{"))
		}
	}
	sort.Strings(out)
	return out
}
