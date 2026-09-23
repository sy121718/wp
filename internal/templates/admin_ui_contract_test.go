package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// partialIncludeRe 页面里 include 的 partials 片段（判据输入要跟着 markup 走，见下）。
//
// 只锚到片段路径、**不要求紧跟 `}}`** —— 带参数的 include（`{{include "partials/x.html" a.Form}}`）
// 是属性页抽屉的常态写法，要求闭合会把它们全部漏掉（漏掉的表现是判据又退回「对搬走的代码判空」）。
// 路径形态有三种：根下页面写 partials/x.html、子目录页面写 ../partials/x.html、
// 同目录片段写裸名 x.html —— 正则只锚「include 了哪个 .html」，具体位置交给
// adminTemplatePath 按 basename 解析（锚死 partials/ 前缀会漏掉后两种）。
var partialIncludeRe = regexp.MustCompile(`\{\{include "([^"]+\.html)"`)

// hasFormLayout 公共表单布局类三支任一：form-inline（工具栏式一行）、form-stack（纵向）、
// form-row（等宽自动列数 + 窄屏堆叠，抽屉里的表单用它）。只认前两支会让改造后改用
// form-row 的页面被判成「没接公共类」。
func hasFormLayout(src string) bool {
	return strings.Contains(src, "form-inline") || strings.Contains(src, "form-stack") || strings.Contains(src, "form-row")
}

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

// formLayoutInPartials 已并入判据：表单 markup 搬进 partials/ 的页面（属性页的三个抽屉），
// 其布局类判据的输入 = 页面 + 它自己 `{{include "partials/..."}}` 的那些片段 ——
// 由 partialIncludeRe 从 html 现场解析，不手写映射（手写的那份迟早与 include 目标漂移）。

// 已迁移页面不得重新依赖 pages-* 兼容层；这些页面的结构由公共 UI Kit 提供。
func TestMigratedAdminPagesDoNotUseLegacyClasses(t *testing.T) {
	for _, name := range []string{"theme.html", "theme_settings.html", "pages.html", "blocks.html", "plugins.html", "product_brands.html", "product_categories.html", "product_tags.html", "products.html", "product_attributes.html", "product_pricing.html", "product_detail_template.html", "product_bundle.html", "product_translations.html", "inventory.html", "inventory_sources.html", "inventory_purchases.html"} {
		// 按 basename 递归找（模板已按后端模块分进子目录），别硬编码 admin/ 下的一层路径。
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
		// 公共表单布局类有三支：form-inline（工具栏式一行）、form-stack（纵向）、
		// form-row（等宽自动列数 + 窄屏堆叠，抽屉里的表单用它）。判据的**落点要跟着 markup 走**：
		// 属性页的三个抽屉表单已搬进 partials/（同一份 markup 被页面 include 与失败重渲染共用），
		// 页面文件里再没有表单 —— 只在页面文件上判这条会变成「对搬走的代码判空」：
		// 不是没违规，是判据看不见。故页面自身没有布局类时，把 include 的片段一起当判据输入。
		layoutSrc := html
		if !hasFormLayout(layoutSrc) {
			for _, m := range partialIncludeRe.FindAllStringSubmatch(html, -1) {
				// include 的路径随「页面所在目录」变化：根下页面写 partials/x.html，
				// 子目录页面写 ../partials/x.html，同目录片段写裸名 x.html。
				// 一律按 basename 递归解析（adminTemplatePath），别自己拼 admin/<path>。
				ref := m[1]
				if i := strings.LastIndex(ref, "/"); i >= 0 {
					ref = ref[i+1:]
				}
				partSrc, perr := os.ReadFile(adminTemplatePath(t, ref))
				if perr != nil {
					t.Fatalf("%s 里 include 的片段 %s 读不到：%v", path, m[1], perr)
				}
				layoutSrc += string(partSrc)
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
