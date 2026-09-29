package workbenchhttp

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestEditorBridgeStripsNodeClassPrefixByLength 钉住「节点类名 → data-sky-id」的还原口径。
//
// 回归背景（实测）：桥接脚本里写的是 cls.slice(5)，而 core.NodeClass 的前缀
// 'sky-c-' 是 6 个字符。于是每个节点的 data-sky-id 都多出一个前导 "-"：
//
//	编译产物 class = "sky-c-about-h"  →  桥接还原出 data-sky-id = "-about-h"
//	AST 里的节点 id 是 "about-h"
//
// 父窗口 findNode("-about-h") 恒返回 null，画布发回的**每一条**消息都落空 ——
// 双击能进编辑态、失焦后回写被静默丢弃（文字弹回），点选、右键、拖放重排、
// 就地插入同样全部无反应。判据只能是「长度」而不是「看起来对」。
func TestEditorBridgeStripsNodeClassPrefixByLength(t *testing.T) {
	prefix := strings.TrimSuffix(core.NodeClass("x"), "x")
	if prefix != "sky-c-" {
		t.Fatalf("NodeClass 前缀已变为 %q：桥接脚本的还原逻辑必须同步", prefix)
	}
	if got := len(prefix); got != 6 {
		t.Fatalf("前缀长度 %d（预期 6）：写死数字的还原逻辑会重新踩坑", got)
	}
	if strings.Contains(editorBridgeScript, "cls.slice(5)") {
		t.Fatal("editorBridgeScript 里仍有写死的 cls.slice(5)：'sky-c-' 是 6 个字符，会还原出带前导 '-' 的 id")
	}
	if !strings.Contains(editorBridgeScript, "cls.slice(WB_SKY_PREFIX.length)") {
		t.Fatal("editorBridgeScript 未按 WB_SKY_PREFIX.length 截断节点类名")
	}
	if !strings.Contains(editorBridgeScript, "var WB_SKY_PREFIX = 'sky-c-'") {
		t.Fatal("editorBridgeScript 未声明 WB_SKY_PREFIX（前缀真源）")
	}
}

// TestEditorBridgePlaceholdersAreClosed 桥接脚本的取词占位符与登记表**双向**闭合。
//
// 为什么两个方向都要钉：漏登记一个占位符 → 注入后那个按钮原样显示「{{bridge.x}}」
// （不会让任何断言变红、也不 500）；反过来多登记一个 → 表里躺着一条永远不会被替换的
// 词条，改名时也没人知道它还有用。判据是**集合相等**，不是计数 —— 计数相等但内容
// 不同（一个漏一个多）时计数照样对得上。
func TestEditorBridgePlaceholdersAreClosed(t *testing.T) {
	inScript := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{\{(bridge\.[A-Za-z0-9_.]+)\}\}`).FindAllStringSubmatch(editorBridgeScript, -1) {
		inScript[m[1]] = true
	}
	registered := map[string]bool{}
	for _, it := range editorBridgeTexts {
		if it.Key == "" || it.Fallback == "" {
			t.Fatalf("桥接文案登记项缺 key 或中文兜底：%+v", it)
		}
		if !strings.HasPrefix(it.Key, "workbench.bridge.") {
			t.Fatalf("桥接文案 key 越出 workbench.bridge.* 命名空间：%q", it.Key)
		}
		registered[it.Name] = true
	}
	for name := range registered {
		if !inScript[name] {
			t.Errorf("登记了 %q 但脚本里没有这个占位符（多余条目）", name)
		}
	}
	for name := range inScript {
		if !registered[name] {
			t.Errorf("脚本里的占位符 %q 没登记：注入后按钮上会原样显示花括号", name)
		}
	}
	// 判据自检：占位符集非空（防集合两边同时为空时静默通过）。
	if len(inScript) == 0 {
		t.Fatal("脚本里一个 bridge 占位符都没有：判据已退化为空转")
	}
	// 注入两条路径都要真替换：中文兜底路径无残留；译文路径的替换次数 == 脚本里
	// 占位符的**出现**次数（不是登记条数 —— 同一个占位符可以出现在多处，
	// 例如「复制」同时出现在右键菜单与快捷条）。
	if zh := editorBridgeScriptFor(func(_, fallback string) string { return fallback }); strings.Contains(zh, "{{bridge.") {
		t.Error("中文兜底路径下仍有未替换的占位符")
	}
	occurrences := len(regexp.MustCompile(`\{\{bridge\.[A-Za-z0-9_.]+\}\}`).FindAllString(editorBridgeScript, -1))
	en := editorBridgeScriptFor(func(_, _ string) string { return "EN-TEXT" })
	if got := strings.Count(en, "EN-TEXT"); got != occurrences {
		t.Errorf("译文替换次数 %d，脚本里占位符出现 %d 次：有占位符没被替换", got, occurrences)
	}
}

// TestJsSingleQuotedEscapesQuotes 译文里的引号 / 反斜杠 / 换行不能打断注入的脚本。
func TestJsSingleQuotedEscapesQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Insert component", "Insert component"},
		{`Don't`, `Don\'t`},
		{`a\b`, `a\\b`},
		{"line1\nline2", `line1\nline2`},
		{"</script>", `<\/script>`},
	}
	for _, tc := range cases {
		if got := jsSingleQuoted(tc.in); got != tc.want {
			t.Errorf("jsSingleQuoted(%q) = %q，预期 %q", tc.in, got, tc.want)
		}
	}
}
