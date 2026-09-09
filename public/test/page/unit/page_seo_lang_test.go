package unit

// page_seo_lang_test.go — 产物 head 的 hreflang 与站点 sitemap 语言分组
// （多语言 P3，docs/06-D §5 / §15.5 第 4 条）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// artifactIndexHTML 读取落盘产物的 index.html。
func artifactIndexHTML(t *testing.T, hash string) string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	data, err := os.ReadFile(filepath.Join(root, "artifacts", hash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return string(data)
}

// TestPageArtifactHreflangPerLanguage 同一页两种语言的产物 head 各自输出
// 互指链接（含 x-default 指向默认语言）。
func TestPageArtifactHreflangPerLanguage(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "en-US"},
		},
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}

	page := createPage(t, svc, projectID, "/about", headingDocument)
	zh, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	en, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}

	want := []string{
		"hreflang=\"zh-CN\" href=\"/about\"",
		"hreflang=\"en-US\" href=\"/en/about\"",
		"hreflang=\"x-default\" href=\"/about\"",
	}
	for _, c := range []struct{ name, hash string }{
		{"zh-CN", zh.StagedHash},
		{"en-US", en.StagedHash},
	} {
		html := artifactIndexHTML(t, c.hash)
		for _, w := range want {
			if !strings.Contains(html, w) {
				t.Fatalf("%s 产物缺少互指 %s", c.name, w)
			}
		}
	}
}

// TestPageSitemapGroupedByLanguage 发布后 sitemap.xml 按语言分组输出 xhtml:link。
func TestPageSitemapGroupedByLanguage(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "en-US"},
		},
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}

	page := createPage(t, svc, projectID, "/about", headingDocument)
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(activeDir(t), "sitemap.xml"))
	if err != nil {
		t.Fatalf("读取 sitemap 失败: %v", err)
	}
	sm := string(data)
	for _, w := range []string{
		"/about", "/en/about",
		"xmlns:xhtml=\"http://www.w3.org/1999/xhtml\"",
		"hreflang=\"en-US\"", "hreflang=\"zh-CN\"", "hreflang=\"x-default\"",
	} {
		if !strings.Contains(sm, w) {
			t.Fatalf("sitemap 缺少 %s:\n%s", w, sm)
		}
	}
	if strings.Count(sm, "<url>") != 2 {
		t.Fatalf("sitemap 应有 2 条 url，实际 %d:\n%s", strings.Count(sm, "<url>"), sm)
	}
	if strings.Count(sm, "<xhtml:link") != 6 {
		t.Fatalf("每条 url 应有 3 条互指（2 语言 + x-default），实际 %d:\n%s", strings.Count(sm, "<xhtml:link"), sm)
	}
}
