package builder

import (
	"strings"
	"testing"

	languagesPkg "go_wp/internal/builder/components/languages"
	"go_wp/internal/builder/core"
)

const languagesDocument = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "语言", "description": "语言"}},
  "root": [{"id": "lang1", "type": "core.languages", "props": {"orientation": "horizontal", "gap": "16px"}}]
}`

func compileLanguagesDoc(t *testing.T, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(languagesDocument))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts = append([]CompileOption{WithComponentSet(i18nTestComponentSet(t))}, opts...)
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML
}

func bilingualLinks() []core.LocaleLink {
	return []core.LocaleLink{
		{Lang: "zh-CN", Href: "/zh-CN/about", Current: true},
		{Lang: "en-US", Href: "/en-US/about"},
	}
}

func TestLanguagesSwitcherRendersLinks(t *testing.T) {
	html := compileLanguagesDoc(t, WithLocaleLinks(bilingualLinks()))

	for _, want := range []string{

		`<a class="sky-lang-link" href="/en-US/about" hreflang="en-US" lang="en-US">English</a>`,

		`<span class="sky-lang-current" lang="zh-CN" aria-current="true">简体中文</span>`,
		`<nav class="sky-c-lang1 sky-lang"`,
		`aria-label="语言"`,
		`<ul class="sky-lang-list">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\nHTML=%s", want, html)
		}
	}

	if strings.Contains(html, `href="/zh-CN/about"`) {
		t.Fatalf("当前语言不应渲染为链接\nHTML=%s", html)
	}

	if strings.Contains(html, "<script>location") || strings.Contains(html, "onclick") {
		t.Fatalf("切换器不得引入 JS 跳转\nHTML=%s", html)
	}
}

func TestLanguagesSwitcherHiddenWhenSingleLocale(t *testing.T) {
	cases := []struct {
		name  string
		links []core.LocaleLink
	}{
		{"未注入", nil},
		{"单语言", []core.LocaleLink{{Lang: "zh-CN", Href: "/zh-CN/about", Current: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := compileLanguagesDoc(t, WithLocaleLinks(c.links))
			if strings.Contains(html, "sky-lang") {
				t.Fatalf("单语言站点不应渲染切换器\nHTML=%s", html)
			}
		})
	}
}

func TestLanguagesSwitcherSkipsLinkWithoutHref(t *testing.T) {
	html := compileLanguagesDoc(t, WithLocaleLinks([]core.LocaleLink{
		{Lang: "zh-CN", Href: "/zh-CN/about", Current: true},
		{Lang: "en-US"},
	}))
	if strings.Contains(html, "sky-lang") {
		t.Fatalf("仅剩一条有效语言时应整块不渲染\nHTML=%s", html)
	}
	if strings.Contains(html, "href=\"\"") {
		t.Fatalf("产物出现空 href\nHTML=%s", html)
	}
}

func TestLanguagesSwitcherLabelUsesI18nKey(t *testing.T) {
	var asked []string
	html := compileLanguagesDoc(t, WithLocaleLinks(bilingualLinks()), WithTranslator(func(key, fallback string) string {
		asked = append(asked, key)
		return "Language"
	}))
	if !strings.Contains(strings.Join(asked, "\n"), languagesPkg.TextKeyLabel) {
		t.Fatalf("构建期未请求 %q（实际：%v）", languagesPkg.TextKeyLabel, asked)
	}
	if !strings.Contains(html, `aria-label="Language"`) {
		t.Fatalf("产物未使用注入译文\nHTML=%s", html)
	}
}

func TestLanguagesSwitcherShowCode(t *testing.T) {
	doc := `{
  "settings": {"layout": {"mode": "full"}},
  "root": [{"id": "lang1", "type": "core.languages", "props": {"showCode": true}}]
}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := Compile(p, WithComponentSet(i18nTestComponentSet(t)), WithLocaleLinks(bilingualLinks()))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.HTML, ">English (en-US)</a>") {
		t.Fatalf("showCode 应追加语言码\nHTML=%s", res.HTML)
	}
}

func TestLanguagesEndonym(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"zh-CN", "简体中文"},
		{"en-US", "English"},
		{"ja", "日本語"},
		{"fr-CA", "Français"},
		{"xx-YY", "xx-YY"},
		{"", ""},
	} {
		if got := languagesPkg.Endonym(c.in); got != c.want {
			t.Fatalf("Endonym(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestRenderDocumentLangAttribute(t *testing.T) {
	p, err := ParsePage([]byte(languagesDocument))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	compile := func(lang string) string {
		res, cerr := Compile(p, WithComponentSet(i18nTestComponentSet(t)), WithLanguage(lang))
		if cerr != nil {
			t.Fatalf("Compile: %v", cerr)
		}
		doc, derr := RenderDocument(res)
		if derr != nil {
			t.Fatalf("RenderDocument: %v", derr)
		}
		return doc
	}
	if doc := compile("en-US"); !strings.Contains(doc, `<html lang="en-US">`) {
		t.Fatalf("en-US 产物 html lang 应为 en-US")
	}
	if doc := compile(""); !strings.Contains(doc, `<html lang="zh-CN">`) {
		t.Fatalf("未指定语言应回退默认语言")
	}
}
