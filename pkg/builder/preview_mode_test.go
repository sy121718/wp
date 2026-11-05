package builder

import (
	"context"
	"go_wp/internal/templates"
	"go_wp/pkg/builder/source"
	"strings"
	"testing"
)

func TestPreviewReadOnlyFactDoesNotEnterPublishedDocument(t *testing.T) {
	page := incompletePreviewPage()
	page.Root[0].Children = page.Root[0].Children[1:2]
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	const marker = `<meta name="site-preview-mode" content="readonly">`
	for _, mode := range []CompileMode{CompileModePreview, CompileModePublish} {
		compiled, err := Compile(page, WithComponentSet(set), WithCompileMode(mode), WithTrackSource("auditTrackingCall();"), WithContext(source.WithPreviewState(context.Background(), source.PreviewSuccess)))
		if err != nil {
			t.Fatal(err)
		}
		html, err := RenderDocument(compiled)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(html, "auditTrackingCall();") != (mode == CompileModePublish) {
			t.Fatalf("用途%s统计采集隔离失败", mode)
		}
		if strings.Contains(html, `<meta name="site-preview-state" content="success">`) != (mode == CompileModePreview) {
			t.Fatalf("发布不能携带模拟状态: %s", mode)
		}
		if strings.Contains(html, marker) != (mode == CompileModePreview) || strings.Contains(html, `class="sky-preview-notice"`) != (mode == CompileModePreview) {
			t.Fatalf("用途%s只读标记不一致", mode)
		}
	}
}
