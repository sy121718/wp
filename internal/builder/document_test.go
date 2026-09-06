package builder

// document_test.go — RenderDocument 文档骨架字节级 golden（防产物哈希漂移）。
//
// RenderDocument 是页面产物完整 HTML 文档的组装入口（预览与静态发布共用），
// 其输出字节直接决定 Artifact 哈希（pipeline.NewArtifact 对 html 字节哈希）。
// 骨架任何改动（换行/缩进/标签顺序/转义策略）都会让本测试红灯，
// 提醒「产物字节已漂移、历史产物哈希将失效」。
//
// golden 生成：go test ./internal/builder -run TestRenderDocumentGolden -update
// （用改造前的手工拼接输出固化，改造为 Jet 模板后对比逐字节一致）。

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateDocumentGolden 更新 RenderDocument golden（仅维护时用）。
var updateDocumentGolden = flag.Bool("update-document-golden", false, "更新 RenderDocument golden 文件")

// TestRenderDocumentGolden 断言 RenderDocument 输出与 golden 逐字节一致。
//
// 用例覆盖全部槽位 + 转义边界：
//   - Title/MetaDescription 含 &<>'" → 默认 HTML 转义（等价 html.EscapeString）；
//   - BodyClass 含 join 空格 → unsafe 原样（保持现状未转义，父代理单独处理）；
//   - HTML/CSS/ThemeVarsCSS/enhanceScript → unsafe 原样（编译产物，不二次转义）。
func TestRenderDocumentGolden(t *testing.T) {
	c := &CompiledPage{
		Title:           "测试页 & <Title> \"双引号\" '单引号'",
		MetaDescription: "页面描述 & <meta> \"引号\" '单引号'",
		BodyClasses:     []string{"wp-page", "wp-boxed", "custom-theme"},
		HTML:            `<section class="wp-c-hero wp-section"><h1 class="wp-heading">Hello &amp; World</h1></section>`,
		CSS:             `.wp-c-hero{display:grid;max-width:1200px}`,
		ThemeVarsCSS:    `:root{--wp-c-primary:#3366ff;--wp-c-bg:#ffffff}`,
	}
	got := RenderDocument(c)

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
