package builder

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateDocumentGolden = flag.Bool("update-document-golden", false, "更新 RenderDocument golden 文件")

func TestRenderDocumentGolden(t *testing.T) {
	c := &CompiledPage{
		Title:           "测试页 & <Title> \"双引号\" '单引号'",
		MetaDescription: "页面描述 & <meta> \"引号\" '单引号'",
		BodyClasses:     []string{"sky-page", "sky-boxed", "custom-theme"},
		HTML:            `<section class="sky-c-hero sky-section"><h1 class="sky-heading">Hello &amp; World</h1></section>`,
		CSS:             `.sky-c-hero{display:grid;max-width:1200px}`,
		ThemeVarsCSS:    `:root{--sky-c-primary:#3366ff;--sky-c-bg:#ffffff}`,

		EnhanceSource: enhanceSrcForTest(t),
	}
	got, err := RenderDocument(c)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "document.html")
	if *updateDocumentGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("写 golden 失败: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("读 golden 失败（首次请用 -update-document-golden 生成）: %v", err)
	}
	if got != string(want) {
		t.Errorf("RenderDocument 输出与 golden 不一致（产物字节漂移）:\n--- got  ---\n%q\n--- want ---\n%q", got, string(want))
	}
}
