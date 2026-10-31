package builder

import (
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

func incompletePreviewPage() *Page {
	return &Page{
		Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}},
		Root: []*core.Node{{ID: "body", Type: "core.container", Props: containerProps(), Children: []*core.Node{
			{ID: "empty-text", Name: "商品正文", Type: "core.text", Props: json.RawMessage(`{"mode":"richtext","text":""}`)},
			{ID: "kept-text", Type: "core.text", Props: json.RawMessage(`{"mode":"plaintext","text":"兄弟内容完整保留"}`)},
			{ID: "empty-slider", Type: "core.slider", Props: json.RawMessage(`{}`)},
		}}},
	}
}

func TestIncompletePreviewKeepsSiblingsAndDocument(t *testing.T) {
	page := incompletePreviewPage()
	before, _ := json.Marshal(page)
	incomplete, err := ValidatePageTolerant(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 2 || !incomplete["empty-text"] || !incomplete["empty-slider"] || incomplete["body"] {
		t.Fatalf("只应记录实际未完成节点: %v", incomplete)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`sky-c-body`, `sky-c-empty-text sky-incomplete`, `data-sky-id="empty-text"`, `商品正文`, `sky-c-empty-slider sky-incomplete`, `兄弟内容完整保留`} {
		if !strings.Contains(preview.HTML, expected) {
			t.Errorf("预览缺少 %q: %s", expected, preview.HTML)
		}
	}
	if strings.Count(preview.HTML, `data-sky-incomplete="true"`) != 2 {
		t.Errorf("占位数量不正确: %s", preview.HTML)
	}
	after, _ := json.Marshal(page)
	if string(before) != string(after) {
		t.Fatal("容错校验或预览修改了输入文档")
	}
	again, err := Compile(page, WithComponentSet(set), WithCompileMode(CompileModePreview))
	if err != nil || again.HTML != preview.HTML || again.CSS != preview.CSS {
		t.Fatalf("同一草稿重复编译必须确定: %v", err)
	}

	if _, err = Compile(page, WithComponentSet(set), WithCompileMode(CompileModePublish)); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("发布必须拒绝未完成文本: %v", err)
	}
	page.Root[0].Children[0].Props = json.RawMessage(`{"text":"已补正文"}`)
	if _, err = Compile(page, WithComponentSet(set), WithCompileMode(CompileModePublish)); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("发布必须拒绝未添加内容的轮播: %v", err)
	}
	page.Root[0].Children[2].Children = []*core.Node{{ID: "slide", Type: "core.text", Props: json.RawMessage(`{"text":"轮播已补齐"}`)}}
	published, err := Compile(page, WithComponentSet(set), WithCompileMode(CompileModePublish))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(published.HTML, "sky-incomplete") {
		t.Fatal("正式产物不应出现编辑占位")
	}
}

func TestIncompletePreviewStillRejectsInvalidNodes(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Page)
	}{
		{"未完成后的兄弟重复ID", func(p *Page) { p.Root[0].Children[1].ID = "empty-text" }},
		{"未完成后的兄弟未知组件", func(p *Page) { p.Root[0].Children[1].Type = "core.unknown" }},
		{"未完成后的兄弟非法属性", func(p *Page) { p.Root[0].Children[1].Props = json.RawMessage(`{"mode":"bad","text":"abc"}`) }},
		{"空文本的非法绑定", func(p *Page) {
			p.Root[0].Children[0].Props = json.RawMessage(`{"binding":{"field":"post.secret;alert(1)"}}`)
		}},
		{"空文本的非法样式", func(p *Page) {
			p.Root[0].Children[0].Props = json.RawMessage(`{"typography":{"desktop":{"fontSize":"bad; color:red"}}}`)
		}},
		{"空文本的非法高级属性", func(p *Page) {
			p.Root[0].Children[0].Props = json.RawMessage(`{"advanced":{"widthMode":"fixed","widthValue":"bad; color:red"}}`)
		}},
		{"空文本自定义ID与后续节点冲突", func(p *Page) { p.Root[0].Children[0].Props = json.RawMessage(`{"advanced":{"customId":"kept-text"}}`) }},
		{"空轮播的非法属性", func(p *Page) { p.Root[0].Children[2].Props = json.RawMessage(`{"perView":{"desktop":99}}`) }},
		{"空轮播的非法高级属性", func(p *Page) { p.Root[0].Children[2].Props = json.RawMessage(`{"advanced":{"widthMode":"bad"}}`) }},
		{"容器非法布局", func(p *Page) { p.Root[0].Props = json.RawMessage(`{"tag":"section","layout":{"engine":"bad"}}`) }},
		{"页签标签数量不匹配", func(p *Page) {
			p.Root[0].Type = "core.tabs"
			p.Root[0].Props = json.RawMessage(`{"tabs":[{"label":"唯一标签"}]}`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := incompletePreviewPage()
			tc.edit(page)
			if _, err := ValidatePageTolerant(page); err == nil || errors.Is(err, core.ErrIncompleteNode) {
				t.Fatalf("非法配置必须被拒绝，不能按未完成降级: %v", err)
			}
		})
	}
}

func TestContainerValidateStillChecksChildren(t *testing.T) {
	page := incompletePreviewPage()
	container, err := core.Lookup("core.container")
	if err != nil {
		t.Fatal(err)
	}
	if err = container.Validate(page.Root[0], map[string]bool{}); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("组件直接校验仍应检查整个子树: %v", err)
	}
}
