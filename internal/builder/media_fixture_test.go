package builder

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/upload"
)

// 编译测试使用显式媒体部署配置，避免把未配置的相对URL当作可发布资源。
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
	code := m.Run()
	_ = upload.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// 历史文档在组件数据入口归一，产物所有媒体完整且原始 AST 不受影响。
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

// 清除/尚未选图只影响本节点；危险地址和非法样式不进入容错分支。
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
