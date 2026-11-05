package builder

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

const i18nComponentDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "i18n", "description": "i18n"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"items": [{"label": "首页", "url": "/"}], "mobileCollapse": true}},
    {"id": "gal1", "type": "core.gallery", "props": {"items": [{"url": "/storage/image/a.jpg", "alt": "图"}], "mode": "carousel", "carousel": {"arrows": true, "dots": true}}},
    {"id": "sld1", "type": "core.slider", "props": {"showArrows": true, "showDots": true}, "children": [{"id": "sld1a", "type": "core.text", "props": {"text": "slide"}}]},
    {"id": "vid1", "type": "core.video", "props": {"url": "https://www.youtube.com/watch?v=PLACEHOLDER_ID"}},
    {"id": "cd1", "type": "core.countdown", "props": {"targetDate": "2026-01-01", "showDays": true}},
    {"id": "frm1", "type": "core.form", "props": {"fields": [{"type": "email", "name": "email", "label": "邮箱"}]}},
    {"id": "rat1", "type": "core.rating", "props": {"value": 4.5, "max": 5}}
  ]
}`

func i18nTestComponentSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	return set
}

func compileI18nDoc(t *testing.T, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(i18nComponentDocJSON))
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

func TestCompileComponentTextUsesI18nKey(t *testing.T) {
	var asked []string
	html := compileI18nDoc(t, WithTranslator(func(key, fallback string) string {
		asked = append(asked, key)
		return "<" + key + ">"
	}))

	wantKeys := []string{
		"site.component.nav.label",
		"site.component.gallery.prev",
		"site.component.gallery.next",
		"site.component.slider.prev",
		"site.component.slider.next",
		"site.component.slider.slide_label",
		"site.component.video.title",
		"site.component.countdown.days",
		"site.component.countdown.hours",
		"site.component.countdown.minutes",
		"site.component.countdown.seconds",
		"site.component.form.submit",
		"site.component.rating.label",
	}
	joined := strings.Join(asked, "\n")
	for _, key := range wantKeys {
		if !strings.Contains(joined, key) {
			t.Fatalf("构建期未向取词函数请求 key %q（实际请求：%v）", key, asked)
		}

		escaped := strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace("<" + key + ">")
		if !strings.Contains(html, escaped) {
			t.Fatalf("产物未出现 key %q 的译文（转义后 %q）", key, escaped)
		}
	}

	for _, zh := range []string{"站点导航", "上一张", "下一张", "视频", "提交"} {
		if strings.Contains(html, zh) {
			t.Fatalf("取词函数已命中，产物仍出现中文原文 %q", zh)
		}
	}
}

func TestCompileComponentTextFallbackChain(t *testing.T) {

	html := compileI18nDoc(t)

	for _, want := range []string{
		`aria-label="站点导航"`,
		`aria-label="上一张"`,
		`aria-label="下一张"`,
		`title="视频"`,
		`>天<`, `>时<`, `>分<`, `>秒<`,
		`>提交<`,
		`aria-label="第 1 张"`,
		`aria-label="评分 4.5 / 5"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("缺词条时应回退中文原文，产物缺少 %q\nHTML=%s", want, html)
		}
	}

	for _, bad := range []string{`aria-label=""`, `title=""`, "site.component.", "%!s(MISSING)"} {
		if strings.Contains(html, bad) {
			t.Fatalf("产物出现非法输出 %q", bad)
		}
	}
}

func TestCompileComponentTextEmptyTranslatorStillFallsBack(t *testing.T) {
	html := compileI18nDoc(t, WithTranslator(func(key, fallback string) string { return "" }))

	if !strings.Contains(html, `aria-label="站点导航"`) {
		t.Fatalf("取词函数返回空串时应回退中文原文\nHTML=%s", html)
	}
	if strings.Contains(html, `aria-label=""`) || strings.Contains(html, `title=""`) {
		t.Fatalf("取词函数返回空串时产物出现空属性\nHTML=%s", html)
	}
}

func TestResolveCompileI18nLang(t *testing.T) {

	lang, fn := resolveCompileI18n(&compileConfig{})
	if lang != i18n.GetDefaultLang() {
		t.Fatalf("未指定语言应取默认语言 %q，实际 %q", i18n.GetDefaultLang(), lang)
	}
	if fn == nil {
		t.Fatal("取词函数不应为 nil")
	}

	lang, _ = resolveCompileI18n(&compileConfig{lang: " en-US "})
	if lang != "en-US" {
		t.Fatalf("显式语言应规范化并保留，实际 %q", lang)
	}

	custom := func(key, fallback string) string { return "custom" }
	_, fn = resolveCompileI18n(&compileConfig{lang: "en-US", translate: custom})
	if fn("x", "y") != "custom" {
		t.Fatal("注入的取词函数应被原样使用")
	}

	cfg := &compileConfig{}
	WithLanguage(" en-US ")(cfg)
	WithTranslator(custom)(cfg)
	if cfg.lang != "en-US" || cfg.translate == nil {
		t.Fatalf("选项未正确写入 compileConfig: lang=%q translate=%v", cfg.lang, cfg.translate != nil)
	}
}

func TestRenderContextTextNilSafe(t *testing.T) {
	var nilCtx *core.RenderContext
	if got := nilCtx.Text("k", "原中文"); got != "原中文" {
		t.Fatalf("nil 接收者应回退 fallback，实际 %q", got)
	}
	ctx := &core.RenderContext{}
	if got := ctx.Text("k", "原中文"); got != "原中文" {
		t.Fatalf("无取词函数应回退 fallback，实际 %q", got)
	}
	ctx = &core.RenderContext{Translate: func(key, fallback string) string { return "" }}
	if got := ctx.Text("k", "原中文"); got != "原中文" {
		t.Fatalf("取词函数返回空串应回退 fallback，实际 %q", got)
	}
	if got := ctx.Text("k", ""); got != "k" {
		t.Fatalf("fallback 为空时应返回 key，实际 %q", got)
	}
}

func TestCompileComponentTextUserValueWins(t *testing.T) {
	doc := `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "i18n", "description": "i18n"}},
  "root": [
    {"id": "frm1", "type": "core.form", "props": {"submitLabel": "发送询价", "fields": [{"type": "text", "name": "q", "label": "问题"}]}}
  ]
}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := Compile(p, WithComponentSet(i18nTestComponentSet(t)), WithTranslator(func(key, fallback string) string {
		return "SUBMIT"
	}))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.HTML, ">发送询价<") {
		t.Fatalf("用户填写的提交文案应原样输出\nHTML=%s", res.HTML)
	}
	if strings.Contains(res.HTML, ">SUBMIT<") {
		t.Fatalf("用户填写的提交文案不应被翻译覆盖\nHTML=%s", res.HTML)
	}
}

func TestCompileComponentTextBrokenPlaceholder(t *testing.T) {
	doc := `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "i18n", "description": "i18n"}},
  "root": [{"id": "rat1", "type": "core.rating", "props": {"value": 4.5, "max": 5}}]
}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := Compile(p, WithComponentSet(i18nTestComponentSet(t)), WithTranslator(func(key, fallback string) string {
		return "Rated"
	}))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.HTML, `aria-label="Rated"`) {
		t.Fatalf("缺占位符的词条应原样输出\nHTML=%s", res.HTML)
	}
	if strings.Contains(res.HTML, "%!") {
		t.Fatalf("产物出现破损格式占位符\nHTML=%s", res.HTML)
	}
}
