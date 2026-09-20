package unit

// i18n02_direction_test.go — locale direction 元数据与 <html dir> 输出（审计 I18N-02）。
//
// 缺陷：产物只有 <html lang>，没有任何书写方向线索 —— 阿拉伯语页面在浏览器里仍按
// 从左到右排版，而报告要求 RTL 发布路径有可验收的产物事实。
//
// 这里钉住两条：
//  1. 方向元数据的判据（主语言子标签 + BCP-47 脚本位），包括**不该**命中的反例
//     （az 默认拉丁文、en-u-nu-arab 是数字系统不是书写方向）；
//  2. 产物输出：RTL 写 dir="rtl"，LTR 一个字节都不写（缺省即 LTR；写了会让全部
//     存量站点的产物 hash 变化，触发一次无意义的全量重建）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// TestLocaleDirection 方向元数据判据。
func TestLocaleDirection(t *testing.T) {
	cases := []struct {
		lang string
		want string
	}{
		{"", "ltr"},
		{"zh-CN", "ltr"},
		{"en-US", "ltr"},
		{"ja", "ltr"},
		{"ar", "rtl"},
		{"ar-EG", "rtl"},
		{"AR_eg", "rtl"}, // 大小写与分隔符都不敏感
		{"he", "rtl"},
		{"fa-IR", "rtl"},
		{"ur", "rtl"},
		{"ckb", "rtl"},
		{"az", "ltr"},      // 默认拉丁文：整条标 RTL 会让 az-Latn 页面无端镜像
		{"az-Arab", "rtl"}, // 脚本位命中
		{"ms-Arab", "rtl"},
		{"en-u-nu-arab", "ltr"}, // 扩展段里的 arab 是数字系统，不是书写方向
		{"zh-Hans", "ltr"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			if got := builder.LocaleDirection(c.lang); got != c.want {
				t.Fatalf("LocaleDirection(%q) = %q，期望 %q", c.lang, got, c.want)
			}
		})
	}
}

// TestDirAttrOnlyRTL 属性值只在 RTL 非空（HTML 与 Manifest 共用这条规则）。
func TestDirAttrOnlyRTL(t *testing.T) {
	if got := builder.DirAttr("ar"); got != "rtl" {
		t.Fatalf("DirAttr(ar) = %q，期望 rtl", got)
	}
	for _, lang := range []string{"", "zh-CN", "en-US", "az"} {
		if got := builder.DirAttr(lang); got != "" {
			t.Fatalf("DirAttr(%q) = %q，LTR 必须返回空串（省略整个属性）", lang, got)
		}
	}
}

// renderDocument 用指定语言编译最小文档并渲染完整 HTML。
func renderDocument(t *testing.T, lang string) string {
	t.Helper()
	page, err := builder.ParsePage([]byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`))
	if err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	compiled, err := compile(t, page, builder.WithLanguage(lang))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		t.Fatalf("渲染文档失败: %v", err)
	}
	return doc
}

// TestRenderDocumentDirAttribute RTL 语言输出 dir="rtl"。
func TestRenderDocumentDirAttribute(t *testing.T) {
	doc := renderDocument(t, "ar-EG")
	if !strings.Contains(doc, `<html lang="ar-EG" dir="rtl">`) {
		t.Fatalf("RTL 产物应输出 dir=\"rtl\"，实际开头：%s", firstLine(doc))
	}
}

// TestRenderDocumentOmitsDirForLTR LTR 语言不输出 dir（缺省即 LTR）。
func TestRenderDocumentOmitsDirForLTR(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		doc := renderDocument(t, lang)
		if !strings.Contains(doc, `<html lang="`+lang+`">`) {
			t.Fatalf("%s 的产物应保持既有的 <html lang> 形态，实际：%s", lang, firstLine(doc))
		}
		if strings.Contains(firstLine(doc), "dir=") {
			t.Fatalf("%s 是 LTR，不该写 dir（会改变全部存量产物的 hash），实际：%s", lang, firstLine(doc))
		}
	}
}

// firstLine 取文档首行，失败信息里给出可读的那一行。
func firstLine(doc string) string {
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		return doc[:i]
	}
	return doc
}
