package mailservice

import (
	"bytes"
	"strings"
	"testing"
)

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

func TestCompileTemplateAttrOrderIsFixed(t *testing.T) {

	node := TemplateNode{
		Key: "c1", Type: tplColumn,

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

	want := "background-color:#fff;padding:12px;border:1px solid #eee;text-align:center;width:100%"
	if !strings.Contains(html, want) {
		t.Fatalf("属性顺序不符合固定表，期望含 %q\n实际：%s", want, html)
	}
}

func TestCompileTemplateTextHasNoMarkup(t *testing.T) {
	_, text, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(text, "<") || strings.Contains(text, ">") {
		t.Fatalf("纯文本里出现了尖括号：%q", text)
	}

	for _, want := range []string{"你好 {{.name}}", "点这里", "关注我们", "左", "右"} {
		if !strings.Contains(text, want) {
			t.Fatalf("纯文本缺少 %q，实际：%q", want, text)
		}
	}

	if !strings.Contains(text, "\n\n") {
		t.Fatalf("段落之间应有空行，实际：%q", text)
	}
}

func TestCompileTemplateKeepsPlaceholders(t *testing.T) {
	html, text, err := CompileTemplate(sampleTree())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(html, "{{.name}}") || !strings.Contains(text, "{{.name}}") {
		t.Fatal("编译期不该展开变量占位符（发送时由 RenderTemplate 处理，missingkey=error）")
	}
}

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

	if !strings.Contains(html, "@media (max-width:600px)") {
		t.Fatal("缺少响应式覆盖 @media (max-width:600px)")
	}
}

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

func TestCompileTemplateRejectsBadSchema(t *testing.T) {
	if _, _, err := CompileTemplate(TemplateTree{Schema: 9, Root: TemplateNode{Key: "r", Type: tplBody}}); err == nil {
		t.Fatal("未知 schema 应被拒绝")
	}
}

func TestCompileTemplateRejectsNonBodyRoot(t *testing.T) {
	if _, _, err := CompileTemplate(TemplateTree{Schema: 1, Root: TemplateNode{Key: "r", Type: tplText}}); err == nil {
		t.Fatal("根不是 body 应被拒绝")
	}
}

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

func asCompileError(err error, target **TemplateCompileError) bool {
	ce, ok := err.(*TemplateCompileError)
	if ok {
		*target = ce
	}
	return ok
}
