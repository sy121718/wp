package mailservice

// template_compile_test.go — 模板编译器的判据（T5）。
//
// 三条核心：
//
//  1. **确定性**：同一棵树编译两次，html 与 text 必须**字节相等**（AGENTS.md 不变量 5
//     的延伸）。这条最容易在「顺手用 map 迭代拼属性」时破掉，而破掉之后没人会发现
//     —— 每次发出去的字节不同，但看起来都对。
//  2. **错误可定位**：未知块 / 非法父子 / 缺 src 都要带 NodeKey。一张正文几十个块，
//     只说「某处不合法」等于没说。
//  3. **每个容器都成 table**：`<table` 的个数 ≥ 容器节点数（邮件客户端只认表格布局）。

import (
	"bytes"
	"strings"
	"testing"
)

// sampleTree 一份覆盖 12 种块（除 body 自己）的树。
func sampleTree() TemplateTree {
	return TemplateTree{Schema: 1, Root: TemplateNode{
		Key: "root", Type: tplBody,
		Children: []TemplateNode{
			{Key: "w1", Type: tplWrapper, Attrs: map[string]string{"background-color": "#f6f7f9"}, Children: []TemplateNode{
				{Key: "s1", Type: tplSection, Attrs: map[string]string{"padding": "24px"}, Children: []TemplateNode{
					{Key: "c1", Type: tplColumn, Children: []TemplateNode{
						{Key: "t1", Type: tplText, Text: "<p>你好 {{.name}}</p>", Attrs: map[string]string{"font-size": "15px"}},
						{Key: "b1", Type: tplButton, Text: "点这里", Attrs: map[string]string{"href": "https://example.com/x"}},
						{Key: "i1", Type: tplImage, Attrs: map[string]string{"src": "https://example.com/a.png", "alt": "图"}},
						{Key: "d1", Type: tplDivider},
						{Key: "sp1", Type: tplSpacer, Attrs: map[string]string{"height": "32"}},
						{Key: "so1", Type: tplSocial, Text: "关注我们"},
					}},
					{Key: "g1", Type: tplGroup, Children: []TemplateNode{
						{Key: "c2", Type: tplColumn, Children: []TemplateNode{
							{Key: "t2", Type: tplText, Text: "左"},
						}},
						{Key: "c3", Type: tplColumn, Children: []TemplateNode{
							{Key: "t3", Type: tplText, Text: "右"},
						}},
					}},
				}},
			}},
			{Key: "r1", Type: tplRaw, Text: "<!-- 原始片段 -->"},
		},
	}}
}

// TestCompileTemplateIsByteIdentical 两次编译字节相等。
func TestCompileTemplateIsByteIdentical(t *testing.T) {
	html1, text1, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	html2, text2, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("第二次编译失败: %v", err)
	}
	if !bytes.Equal([]byte(html1), []byte(html2)) {
		t.Fatalf("HTML 两次编译不相等（确定性被破了）：\n%s\n---\n%s", html1, html2)
	}
	if !bytes.Equal([]byte(text1), []byte(text2)) {
		t.Fatalf("纯文本两次编译不相等")
	}
}

// TestCompileTemplateAttrOrderIsFixed 属性按固定顺序输出（不迭代 map）。
func TestCompileTemplateAttrOrderIsFixed(t *testing.T) {
	// column 必须放在 section 里（父子白名单），所以套两层。
	node := TemplateNode{
		Key: "c1", Type: tplColumn,
		// 故意按「非期望顺序」写进 map：如果实现迭代 map，这段顺序可能变。
		Attrs: map[string]string{
			"width": "100%", "text-align": "center", "border": "1px solid #eee",
			"padding": "12px", "background-color": "#fff",
		},
	}
	tree := TemplateTree{Schema: 1, Root: TemplateNode{Key: "root", Type: tplBody,
		Children: []TemplateNode{
			{Key: "s1", Type: tplSection, Children: []TemplateNode{node}},
		}}}
	html, _, err := CompileTemplate(tree)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 期望顺序：background-color → padding → border → text-align → width
	want := "background-color:#fff;padding:12px;border:1px solid #eee;text-align:center;width:100%"
	if !strings.Contains(html, want) {
		t.Fatalf("属性顺序不符合固定表，期望含 %q\n实际：%s", want, html)
	}
}

// TestCompileTemplateTextHasNoMarkup 纯文本不含标签。
func TestCompileTemplateTextHasNoMarkup(t *testing.T) {
	_, text, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(text, "<") || strings.Contains(text, ">") {
		t.Fatalf("纯文本里出现了尖括号：%q", text)
	}
	// 文本块与按钮文字都要在（文档序）。
	for _, want := range []string{"你好 {{.name}}", "点这里", "关注我们", "左", "右"} {
		if !strings.Contains(text, want) {
			t.Fatalf("纯文本缺少 %q，实际：%q", want, text)
		}
	}
	// 段落之间空行。
	if !strings.Contains(text, "\n\n") {
		t.Fatalf("段落之间应有空行，实际：%q", text)
	}
}

// TestCompileTemplateKeepsPlaceholders 变量占位符原样保留（发送时才展开）。
func TestCompileTemplateKeepsPlaceholders(t *testing.T) {
	html, text, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(html, "{{.name}}") || !strings.Contains(text, "{{.name}}") {
		t.Fatal("编译期不该展开变量占位符（发送时由 RenderTemplate 处理，missingkey=error）")
	}
}

// TestCompileTemplateEveryContainerIsTable 每个容器都成 table。
func TestCompileTemplateEveryContainerIsTable(t *testing.T) {
	html, _, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	containers := 0
	var count func(n TemplateNode)
	count = func(n TemplateNode) {
		switch n.Type {
		case tplWrapper, tplSection, tplColumn, tplGroup, tplText, tplButton, tplImage, tplDivider, tplSpacer, tplSocial:
			containers++
		}
		for _, c := range n.Children {
			count(c)
		}
	}
	count(sampleTree().Root)
	if got := strings.Count(html, "<table role=\"presentation\""); got < containers {
		t.Fatalf("<table> 个数 %d 少于容器节点数 %d（邮件客户端只认表格布局）", got, containers)
	}
	// 响应式覆盖必须在 <style> 里（inline 无法做媒体查询）。
	if !strings.Contains(html, "@media (max-width:600px)") {
		t.Fatal("缺少响应式覆盖 @media (max-width:600px)")
	}
}

// TestCompileTemplateRejectsUnknownBlock 未知块类型带 NodeKey。
func TestCompileTemplateRejectsUnknownBlock(t *testing.T) {
	tree := TemplateTree{Schema: 1, Root: TemplateNode{Key: "root", Type: tplBody,
		Children: []TemplateNode{{Key: "weird1", Type: "teleport"}}}}
	_, _, err := CompileTemplate(tree)
	if err == nil {
		t.Fatal("未知块类型应被拒绝")
	}
	var ce *TemplateCompileError
	if !asCompileError(err, &ce) {
		t.Fatalf("错误应是 *TemplateCompileError，实际 %T", err)
	}
	if ce.NodeKey != "weird1" {
		t.Fatalf("错误应带 NodeKey=weird1，实际 %q", ce.NodeKey)
	}
}

// TestCompileTemplateRejectsBadNesting column 里塞 section。
func TestCompileTemplateRejectsBadNesting(t *testing.T) {
	tree := TemplateTree{Schema: 1, Root: TemplateNode{Key: "root", Type: tplBody,
		Children: []TemplateNode{
			{Key: "s1", Type: tplSection, Children: []TemplateNode{
				{Key: "c1", Type: tplColumn, Children: []TemplateNode{
					{Key: "s2", Type: tplSection, Children: nil},
				}},
			}},
		}}}
	_, _, err := CompileTemplate(tree)
	if err == nil {
		t.Fatal("column 里放 section 应被拒绝")
	}
	var ce *TemplateCompileError
	if !asCompileError(err, &ce) {
		t.Fatalf("错误应是 *TemplateCompileError，实际 %T", err)
	}
	if ce.NodeKey != "s2" {
		t.Fatalf("错误应定位到非法子块 s2，实际 %q", ce.NodeKey)
	}
}

// TestCompileTemplateRejectsImageWithoutSrc 缺 src 带 NodeKey。
func TestCompileTemplateRejectsImageWithoutSrc(t *testing.T) {
	tree := TemplateTree{Schema: 1, Root: TemplateNode{Key: "root", Type: tplBody,
		Children: []TemplateNode{
			{Key: "s1", Type: tplSection, Children: []TemplateNode{
				{Key: "c1", Type: tplColumn, Children: []TemplateNode{
					{Key: "img1", Type: tplImage},
				}},
			}},
		}}}
	_, _, err := CompileTemplate(tree)
	if err == nil {
		t.Fatal("图片缺 src 应被拒绝")
	}
	var ce *TemplateCompileError
	if !asCompileError(err, &ce) || ce.NodeKey != "img1" {
		t.Fatalf("错误应定位到 img1，实际 %v", err)
	}
}

// TestCompileTemplateRejectsBadSchema 版本号不对。
func TestCompileTemplateRejectsBadSchema(t *testing.T) {
	if _, _, err := CompileTemplate(TemplateTree{Schema: 9, Root: TemplateNode{Key: "r", Type: tplBody}}); err == nil {
		t.Fatal("未知 schema 应被拒绝")
	}
}

// TestCompileTemplateRejectsNonBodyRoot 根必须是 body。
func TestCompileTemplateRejectsNonBodyRoot(t *testing.T) {
	if _, _, err := CompileTemplate(TemplateTree{Schema: 1, Root: TemplateNode{Key: "r", Type: tplText}}); err == nil {
		t.Fatal("根不是 body 应被拒绝")
	}
}

// TestValidateTreeDoesNotCompile 校验不产出字节（前端保存前调用它）。
func TestValidateTreeDoesNotCompile(t *testing.T) {
	if err := ValidateTree(sampleTree()); err != nil {
		t.Fatalf("合法树不该报错: %v", err)
	}
	bad := TemplateTree{Schema: 1, Root: TemplateNode{Key: "r", Type: tplBody,
		Children: []TemplateNode{{Key: "x", Type: "nope"}}}}
	if err := ValidateTree(bad); err == nil {
		t.Fatal("未知块应被 ValidateTree 拒绝")
	}
}

// TestCompileTemplateEscapesAttributes 属性值里的引号与尖括号要转义（否则会跳出属性）。
func TestCompileTemplateEscapesAttributes(t *testing.T) {
	tree := TemplateTree{Schema: 1, Root: TemplateNode{Key: "root", Type: tplBody,
		Children: []TemplateNode{
			{Key: "s1", Type: tplSection, Children: []TemplateNode{
				{Key: "c1", Type: tplColumn, Children: []TemplateNode{
					{Key: "i1", Type: tplImage, Attrs: map[string]string{
						"src": `https://example.com/a.png"onerror="alert(1)`,
						"alt": "a<b>&c",
					}},
				}},
			}},
		}}}
	html, _, err := CompileTemplate(tree)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(html, `onerror="alert(1)"`) {
		t.Fatal("属性值里的引号没转义 —— 可以跳出属性注入事件处理器")
	}
	if !strings.Contains(html, "a&lt;b&gt;&amp;c") {
		t.Fatalf("alt 里的尖括号 / & 没转义：%s", html)
	}
}

// asCompileError errors.As 的薄封装（避免每处都重复写）。
func asCompileError(err error, target **TemplateCompileError) bool {
	ce, ok := err.(*TemplateCompileError)
	if ok {
		*target = ce
	}
	return ok
}
