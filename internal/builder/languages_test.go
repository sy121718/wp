package builder

// languages_test.go — 前台语言切换器（core.languages，多语言 P3）。
//
// 覆盖 docs/06-D 的访问面硬约束与 §9 缺语言策略：
//  1. 零 JS：切换器只输出纯链接（<a hreflang>），产物不含跳转脚本；
//  2. 当前语言有标记且不可点（<span aria-current>），其他语言为 <a hreflang lang>；
//  3. 构建期注入的链接清单原样进入产物（产物里必须真实出现链接）；
//  4. §9 缺语言策略 S2「隐藏」：条目不足两条（单语言 / 未开语言前缀）整块不渲染，
//     单语言站点产物字节与 P3 之前一致；
//  5. <html lang> 跟随构建语言（en-US 产物不再自称 zh-CN）。

import (
	"strings"
	"testing"

	languagesPkg "go_wp/internal/builder/components/languages"
	"go_wp/internal/builder/core"
)

// languagesDocument 含一个语言切换器节点的页面文档（原子组件，无 children）。
const languagesDocument = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "语言", "description": "语言"}},
  "root": [{"id": "lang1", "type": "core.languages", "props": {"orientation": "horizontal", "gap": "16px"}}]
}`

// compileLanguagesDoc 编译语言切换器用例文档。
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

// bilingualLinks 双语站点在 /about 页上的切换器条目（与装配层同源的数据形态）。
func bilingualLinks() []core.LocaleLink {
	return []core.LocaleLink{
		{Lang: "zh-CN", Href: "/zh-CN/about", Current: true},
		{Lang: "en-US", Href: "/en-US/about"},
	}
}

// TestLanguagesSwitcherRendersLinks 双语站点：产物出现各语言链接 + 当前语言标记。
func TestLanguagesSwitcherRendersLinks(t *testing.T) {
	html := compileLanguagesDoc(t, WithLocaleLinks(bilingualLinks()))

	for _, want := range []string{
		// 非当前语言：纯链接，带 hreflang 与 lang（无障碍正确发音）。
		`<a class="sky-lang-link" href="/en-US/about" hreflang="en-US" lang="en-US">English</a>`,
		// 当前语言：不可点的 span + aria-current 标记 + 语言自称。
		`<span class="sky-lang-current" lang="zh-CN" aria-current="true">简体中文</span>`,
		`<nav class="sky-c-lang1 sky-lang"`,
		`aria-label="语言"`,
		`<ul class="sky-lang-list">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\nHTML=%s", want, html)
		}
	}
	// 当前语言不得输出指向自身的 <a>（docs/06-D：当前语言不可点）。
	if strings.Contains(html, `href="/zh-CN/about"`) {
		t.Fatalf("当前语言不应渲染为链接\nHTML=%s", html)
	}
	// 零 JS：切换器片段里不得出现脚本或内联事件。
	if strings.Contains(html, "<script>location") || strings.Contains(html, "onclick") {
		t.Fatalf("切换器不得引入 JS 跳转\nHTML=%s", html)
	}
}

// TestLanguagesSwitcherHiddenWhenSingleLocale §9 策略 S2：条目不足两条整块不渲染。
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

// TestLanguagesSwitcherSkipsLinkWithoutHref 目标语言无地址（本页缺该语言路径）时隐藏该条，
// 绝不输出空 href（S2「隐藏」，不做指向默认语言的回退页）。
func TestLanguagesSwitcherSkipsLinkWithoutHref(t *testing.T) {
	html := compileLanguagesDoc(t, WithLocaleLinks([]core.LocaleLink{
		{Lang: "zh-CN", Href: "/zh-CN/about", Current: true},
		{Lang: "en-US"}, // 缺 Href：本页无该语言可寻址路径
	}))
	if strings.Contains(html, "sky-lang") {
		t.Fatalf("仅剩一条有效语言时应整块不渲染\nHTML=%s", html)
	}
	if strings.Contains(html, "href=\"\"") {
		t.Fatalf("产物出现空 href\nHTML=%s", html)
	}
}

// TestLanguagesSwitcherLabelUsesI18nKey 容器无障碍标签走 site.component.languages.label 词条。
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

// TestLanguagesSwitcherShowCode showCode 在语言自称后追加语言码。
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

// TestLanguagesEndonym 语言自称表：精确命中 → 主语言子标签 → 回退语言码。
func TestLanguagesEndonym(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"zh-CN", "简体中文"},
		{"en-US", "English"},
		{"ja", "日本語"},
		{"fr-CA", "Français"}, // 主语言子标签回退
		{"xx-YY", "xx-YY"},    // 未收录：回退语言码本身
		{"", ""},
	} {
		if got := languagesPkg.Endonym(c.in); got != c.want {
			t.Fatalf("Endonym(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestRenderDocumentLangAttribute <html lang> 跟随构建语言；未指定时回退默认语言。
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
