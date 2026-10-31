package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

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
		{"AR_eg", "rtl"},
		{"he", "rtl"},
		{"fa-IR", "rtl"},
		{"ur", "rtl"},
		{"ckb", "rtl"},
		{"az", "ltr"},
		{"az-Arab", "rtl"},
		{"ms-Arab", "rtl"},
		{"en-u-nu-arab", "ltr"},
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

func TestRenderDocumentDirAttribute(t *testing.T) {
	doc := renderDocument(t, "ar-EG")
	if !strings.Contains(doc, `<html lang="ar-EG" dir="rtl">`) {
		t.Fatalf("RTL 产物应输出 dir=\"rtl\"，实际开头：%s", firstLine(doc))
	}
}

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

func firstLine(doc string) string {
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		return doc[:i]
	}
	return doc
}
