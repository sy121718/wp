package workbenchhttp

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if strings.Contains(editorBridgeJSSource, "cls.slice(5)") {
		t.Fatal("editorBridgeJSSource 里仍有写死的 cls.slice(5)：'sky-c-' 是 6 个字符，会还原出带前导 '-' 的 id")
	}
	if !strings.Contains(editorBridgeJSSource, "cls.slice(WB_SKY_PREFIX.length)") {
		t.Fatal("editorBridgeJSSource 未按 WB_SKY_PREFIX.length 截断节点类名")
	}
	if !strings.Contains(editorBridgeJSSource, "var WB_SKY_PREFIX = 'sky-c-'") {
		t.Fatal("editorBridgeJSSource 未声明 WB_SKY_PREFIX（前缀真源）")
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
	for _, m := range regexp.MustCompile(`\{\{(bridge\.[A-Za-z0-9_.]+)\}\}`).FindAllStringSubmatch(editorBridgeJSSource, -1) {
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
	occurrences := len(regexp.MustCompile(`\{\{bridge\.[A-Za-z0-9_.]+\}\}`).FindAllString(editorBridgeJSSource, -1))
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

// TestEditorBridgeIsValidJavaScript 桥接脚本必须能被 node 解析。
//
// 这是挪出 Go 字符串常量的**主要收益**：常量的语法错误只有画布真的坏掉才会暴露，
// 而桥接坏掉的表现是「点了没反应」——父窗口看起来一切正常，报错在 iframe 控制台。
// 同一段代码在 .js 文件里就能被 node --check 静态拦住。
//
// 没有 node 时跳过（与 check-workbench.sh / defaults_contract_test.go 同一套探针）。
func TestEditorBridgeIsValidJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node：跳过桥接脚本语法检查")
	}
	// 占位符先替换成合法字面量，否则 {{bridge.x}} 本身就是语法错误。
	src := regexp.MustCompile(`\{\{bridge\.[A-Za-z0-9_.]+\}\}`).ReplaceAllString(editorBridgeJSSource, "PLACEHOLDER")
	dir := t.TempDir()
	file := filepath.Join(dir, "editor_bridge.js")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("写临时脚本失败: %v", err)
	}
	out, err := exec.Command(node, "--check", file).CombinedOutput()
	if err != nil {
		t.Fatalf("桥接脚本语法错误（node --check）：%v\n%s", err, out)
	}
}

// TestEditorBridgeSourceFileExists 源码文件必须存在且非空（go:embed 的目标）。
//
// 单独钉一次的理由：go:embed 的目标文件缺失时是**编译错误**（足够安全），但空文件
// 能编译通过 —— 那样注入的就是一个空 <script>，画布静默失去全部交互。
func TestEditorBridgeSourceFileExists(t *testing.T) {
	info, err := os.Stat(filepath.Join("scripts", "editor_bridge.js"))
	if err != nil {
		t.Fatalf("桥接脚本源文件缺失: %v", err)
	}
	if info.Size() < 1000 {
		t.Fatalf("桥接脚本只有 %d 字节，不像完整实现（空文件能编译通过，画布会静默失去交互）", info.Size())
	}
	if !strings.Contains(editorBridgeJSSource, "WB_SKY_PREFIX") {
		t.Error("嵌入的源码里没有 WB_SKY_PREFIX：go:embed 可能指错了文件")
	}
}
