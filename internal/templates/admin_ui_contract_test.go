package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var partialIncludeRe = regexp.MustCompile(`\{\{include "([^"]+\.html)"`)

func hasFormLayout(src string) bool {
	return strings.Contains(src, "form-inline") || strings.Contains(src, "form-stack") || strings.Contains(src, "form-row")
}

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

func TestMigratedAdminPagesDoNotUseLegacyClasses(t *testing.T) {
	for _, name := range []string{"theme.html", "theme_settings.html", "pages.html", "blocks.html", "plugins.html", "product_brands.html", "product_categories.html", "product_tags.html", "products.html", "product_attributes.html", "product_pricing.html", "product_detail_template.html", "product_translations.html", "inventory.html", "sources.html", "purchases.html"} {

		path := adminTemplatePath(t, name)
		html := adminTemplateSource(t, name)
		for _, legacy := range []string{"pages-", "attr-form"} {
			if strings.Contains(html, legacy) {
				t.Errorf("%s 仍依赖旧兼容类 %q", path, legacy)
			}
		}
		if !strings.Contains(html, "card") {
			t.Errorf("%s 未使用公共类 %q", path, "card")
		}

		layoutSrc := html
		if !hasFormLayout(layoutSrc) {
			for _, m := range partialIncludeRe.FindAllStringSubmatch(html, -1) {

				ref := m[1]
				if i := strings.LastIndex(ref, "/"); i >= 0 {
					ref = ref[i+1:]
				}
				layoutSrc += adminTemplateSource(t, ref)
			}
		}
		if !hasFormLayout(layoutSrc) {
			t.Errorf("%s 未使用公共表单布局类（form-inline / form-stack / form-row）", path)
		}
		if name == "theme_settings.html" && strings.Contains(html, "theme-font-input") {
			t.Errorf("%s 仍依赖主题页私有控件视觉类", path)
		}
	}
}
