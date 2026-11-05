package workbenchhttp

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

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

	if len(inScript) == 0 {
		t.Fatal("脚本里一个 bridge 占位符都没有：判据已退化为空转")
	}

	if zh := editorBridgeScriptFor(func(_, fallback string) string { return fallback }); strings.Contains(zh, "{{bridge.") {
		t.Error("中文兜底路径下仍有未替换的占位符")
	}
	occurrences := len(regexp.MustCompile(`\{\{bridge\.[A-Za-z0-9_.]+\}\}`).FindAllString(editorBridgeJSSource, -1))
	en := editorBridgeScriptFor(func(_, _ string) string { return "EN-TEXT" })
	if got := strings.Count(en, "EN-TEXT"); got != occurrences {
		t.Errorf("译文替换次数 %d，脚本里占位符出现 %d 次：有占位符没被替换", got, occurrences)
	}
}

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

func TestEditorBridgeIsValidJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node：跳过桥接脚本语法检查")
	}

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
