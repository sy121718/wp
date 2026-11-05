package builder

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go_wp/internal/templates"
	"go_wp/internal/builder/extensions/views"
	"go_wp/pkg/builder/core"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/upload"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gowp-builder-media-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", dir)
	cfg.Set("upload.base_url", "https://media.example.test")
	if err := upload.Init(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	// 业务组件的视图构建由扩展登记，测试与生产同一路径。
	views.RegisterAll()
	code := m.Run()
	_ = upload.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestMediaFieldsUseConfiguredAbsoluteURLs(t *testing.T) {
	raw := `{"settings":{"layout":{"mode":"full"}},"root":[
 {"id":"image","type":"core.image","props":{"src":"/storage/483.jpg"}},
 {"id":"card","type":"core.card","props":{"title":"卡片","imageSrc":"/storage/card.jpg"}},
 {"id":"info","type":"core.infobox","props":{"title":"信息","mediaImage":"/storage/info.jpg"}},
 {"id":"gallery","type":"core.gallery","props":{"items":[{"url":"/storage/gallery.jpg"}]}},
 {"id":"video","type":"core.video","props":{"url":"/storage/video.mp4","poster":"/storage/poster.jpg","captionsSrc":"/storage/captions.vtt"}}
 ]}`
	page, err := ParsePage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	before := make([]string, len(page.Root))
	for i, node := range page.Root {
		before[i] = string(node.Props)
	}
	compiled, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"483.jpg", "card.jpg", "info.jpg", "gallery.jpg", "video.mp4", "poster.jpg", "captions.vtt"} {
		if !strings.Contains(compiled.HTML, "https://media.example.test/storage/"+key) {
			t.Errorf("媒体 %s 未完整输出", key)
		}
	}
	for i, node := range page.Root {
		if string(node.Props) != before[i] {
			t.Errorf("编译改变节点 %s 的原始 AST", node.ID)
		}
	}
}

func TestMediaImageDraftCanBeIncomplete(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	page := &Page{Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}}, Root: []*core.Node{
		{ID: "image", Type: "core.image", Props: json.RawMessage(`{"src":"  "}`)},
		{ID: "kept", Type: "core.text", Props: json.RawMessage(`{"text":"兄弟内容保留"}`)},
	}}
	preview, err := Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set))
	if err != nil || !strings.Contains(preview.HTML, `data-sky-incomplete="true"`) || !strings.Contains(preview.HTML, "兄弟内容保留") {
		t.Fatalf("清除图片后的草稿需局部占位: %v", err)
	}
	if _, err = Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set)); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("未选图不能正式发布: %v", err)
	}
	for _, raw := range []string{`{"src":"javascript:alert(1)"}`, `{"src":"/unowned/image.jpg"}`, `{"src":"","hover":{"scale":"bad; color:red"}}`} {
		page.Root[0].Props = json.RawMessage(raw)
		if _, err = Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set)); err == nil || errors.Is(err, core.ErrIncompleteNode) {
			t.Fatalf("非法媒体配置不能按未完成降级: %s %v", raw, err)
		}
	}
	page.Root[0].Props = json.RawMessage(`{"src":"/storage/483.jpg"}`)
	published, err := Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set))
	if err != nil || strings.Contains(published.HTML, "sky-incomplete") || !strings.Contains(published.HTML, "https://media.example.test/storage/483.jpg") {
		t.Fatalf("选图后需恢复完整发布: %v", err)
	}
}

type richMediaResolver map[string]string

func (r richMediaResolver) ResolveString(field string) (string, error) { return r[field], nil }

func TestRichMediaFieldsCompileToAbsoluteURLs(t *testing.T) {
	raw := `{"settings":{"layout":{"mode":"full"}},"root":[
 {"id":"text","type":"core.text","props":{"text":"<p>正文<img src='/storage/text.jpg'></p>"}},
 {"id":"card","type":"core.card","props":{"title":"卡片","text":"<img src='/storage/card-rich.jpg'>"}},
 {"id":"quote","type":"core.quote","props":{"text":"<img src='/storage/quote-rich.jpg'>"}},
 {"id":"faq","type":"core.faq","props":{"items":[{"question":"问题","answer":"<img src='/storage/faq-rich.jpg'>"}]}},
 {"id":"info","type":"core.infobox","props":{"title":"信息","text":"<img src='/storage/info-rich.jpg'>"}},
 {"id":"cms","type":"core.text","props":{"binding":{"field":"article.content"}}},
 {"id":"product","type":"core.product","props":{"descriptionField":"product.description"}},
 {"id":"external","type":"core.text","props":{"text":"<img src='https://external.test/storage/original.jpg'>"}},
 {"id":"plain","type":"core.text","props":{"mode":"plaintext","text":"<img src='/unowned/example.jpg'>"}}
 ]}`
	page, err := ParsePage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	resolver := richMediaResolver{"article.content": `<img src="/storage/cms-rich.jpg">`, "product.description": `<img src="/storage/product-rich.jpg">`}
	compiled, err := Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set), WithContentResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"text.jpg", "card-rich.jpg", "quote-rich.jpg", "faq-rich.jpg", "info-rich.jpg", "cms-rich.jpg", "product-rich.jpg"} {
		if !strings.Contains(compiled.HTML, `src="https://media.example.test/storage/`+key+`"`) {
			t.Errorf("富文本媒体未从完整发布链输出绝对URL: %s", key)
		}
	}
	if !strings.Contains(compiled.HTML, `src="https://external.test/storage/original.jpg"`) || !strings.Contains(compiled.HTML, "&lt;img src=&#39;/unowned/example.jpg&#39;&gt;") {
		t.Fatal("外链域名与纯文本HTML示例不能被媒体归一改变")
	}
	after, err := json.Marshal(page)
	if err != nil || string(before) != string(after) {
		t.Fatalf("富文本媒体归一不得改变输入AST: %v", err)
	}
	again, err := Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set), WithContentResolver(resolver))
	if err != nil || again.HTML != compiled.HTML {
		t.Fatalf("同一富文本媒体输入应生成相同产物: %v", err)
	}
}

func TestRichMediaDraftFailureIsLocalAndPublishStrict(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []*core.Node{
		{ID: "bad", Type: "core.text", Props: json.RawMessage(`{"text":"<img src='/unowned/image.jpg'>"}`)},
		{ID: "bad", Type: "core.faq", Props: json.RawMessage(`{"items":[{"question":"问题","answer":"<img src='/unowned/image.jpg'>"}]}`)},
		{ID: "bad", Type: "core.text", Props: json.RawMessage(`{"binding":{"field":"article.content"}}`)},
		{ID: "bad", Type: "core.product", Props: json.RawMessage(`{"descriptionField":"product.description"}`)},
	} {
		t.Run(node.Type+string(node.Props), func(t *testing.T) {
			page := &Page{Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}}, Root: []*core.Node{node,
				{ID: "kept", Type: "core.text", Props: json.RawMessage(`{"text":"兄弟内容保留"}`)},
			}}
			resolver := richMediaResolver{"article.content": `<img src="/unowned/cms.jpg">`, "product.description": `<img src="/unowned/product.jpg">`}
			preview, err := Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set), WithContentResolver(resolver))
			if err != nil || !strings.Contains(preview.HTML, `data-sky-incomplete="true"`) || !strings.Contains(preview.HTML, "兄弟内容保留") {
				t.Fatalf("未完成富文本图片不能破坏兄弟节点: %v", err)
			}
			if _, err = Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set), WithContentResolver(resolver)); !errors.Is(err, core.ErrIncompleteNode) || !strings.Contains(err.Error(), "bad") {
				t.Fatalf("正式发布需给出可定位作者错误: %v", err)
			}
		})
	}
	page := &Page{Root: []*core.Node{{ID: "unsafe", Type: "core.text", Props: json.RawMessage(`{"text":"<img src='/unowned/image.jpg'>","advanced":{"widthMode":"fixed","widthValue":"bad; color:red"}}`)}}}
	if _, err := Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set)); err == nil || errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("富文本未完成不能绕过Advanced安全校验: %v", err)
	}
}
