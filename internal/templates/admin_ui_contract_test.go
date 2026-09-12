package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 后台页面必须同时挂公共语义类和旧页面类；迁移完成前保留旧类只用于布局桥接，
// 不能再出现只依赖 pages-* 才有外观的新增页面。
func TestAdminPagesExposePublicUIClasses(t *testing.T) {
	entries, err := os.ReadDir("admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") || entry.Name() == "layout.html" {
			continue
		}
		path := filepath.Join("admin", entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(src)
		checks := []struct{ legacy, public string }{
			{"pages-card", "card"},
			{"pages-form", "form-row"},
			{"pages-table", "data-table"},
			{"pages-table-wrap", "table-wrap"},
		}
		for _, check := range checks {
			if strings.Contains(html, check.legacy) && !strings.Contains(html, check.public) {
				t.Errorf("%s 使用 %s 但未接入公共类 %s", path, check.legacy, check.public)
			}
		}
	}
}

// 已迁移页面不得重新依赖 pages-* 兼容层；这些页面的结构由公共 UI Kit 提供。
func TestMigratedAdminPagesDoNotUseLegacyClasses(t *testing.T) {
	for _, name := range []string{"theme.html", "theme_settings.html", "pages.html", "blocks.html", "plugins.html", "product_brands.html", "product_categories.html", "product_tags.html", "products.html", "product_attributes.html", "product_pricing.html", "product_detail_template.html", "product_bundle.html", "product_translations.html"} {
		path := filepath.Join("admin", name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(src)
		for _, legacy := range []string{"pages-", "attr-form"} {
			if strings.Contains(html, legacy) {
				t.Errorf("%s 仍依赖旧兼容类 %q", path, legacy)
			}
		}
		if !strings.Contains(html, "card") {
			t.Errorf("%s 未使用公共类 %q", path, "card")
		}
		if !strings.Contains(html, "form-inline") && !strings.Contains(html, "form-stack") {
			t.Errorf("%s 未使用公共表单布局类", path)
		}
		if name == "theme_settings.html" && strings.Contains(html, "theme-font-input") {
			t.Errorf("%s 仍依赖主题页私有控件视觉类", path)
		}
	}
}
