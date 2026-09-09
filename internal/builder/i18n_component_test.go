package builder

// i18n_component_test.go — 构建期组件文案多语言（多语言 P4）。
//
// 覆盖 docs/06-D §10「组件 UI 文案的构建期翻译」的三条契约：
//  1. 文案确实经 sys_i18n key 取词：注入取词函数后，产物出现该函数返回的译文；
//  2. 兜底链绝不失效：取词函数缺失 / 未命中 / 返回空串时，产物回退组件包内中文原文，
//     绝不出现空属性、裸 key 或编译报错；
//  3. 语言维度已预留：WithLanguage 决定 RenderContext.Lang，未指定时取默认语言。
//
// 本文件不依赖数据库：默认取词函数在 i18n 未初始化时按兜底链返回 fallback（原中文）。

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// i18nComponentDocJSON 覆盖全部 7 处访客面固定文案所在组件：
// nav（aria-label）/ gallery（轮播箭头）/ slider（箭头）/ video（iframe title）
// / countdown（天时分秒）/ form（提交按钮）/ rating（无障碍描述）。
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

// i18nTestComponentSet 组件模板 Set（与既有测试共用同一加载方式）。
func i18nTestComponentSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	return set
}

// compileI18nDoc 编译 i18n 用例文档，返回产物 HTML。
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

// TestCompileComponentTextUsesI18nKey 文案确实经 key 取词：注入的取词函数按 key 返回译文，
// 产物必须出现译文而不是组件包内中文原文。
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
		// 模板经 Jet 默认转义输出，故断言转义后的形态。
		escaped := strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace("<" + key + ">")
		if !strings.Contains(html, escaped) {
			t.Fatalf("产物未出现 key %q 的译文（转义后 %q）", key, escaped)
		}
	}

	// 命中取词函数时不应再出现中文原文。
	for _, zh := range []string{"站点导航", "上一张", "下一张", "视频", "提交"} {
		if strings.Contains(html, zh) {
			t.Fatalf("取词函数已命中，产物仍出现中文原文 %q", zh)
		}
	}
}

// TestCompileComponentTextFallbackChain 兜底链：i18n 未初始化（缓存为空）时，
// 缺词条必须回退组件包内中文原文，绝不输出空串、裸 key 或编译报错。
func TestCompileComponentTextFallbackChain(t *testing.T) {
	// 默认取词函数 = i18n.TranslateFunc(默认语言)；本测试进程未初始化 i18n 缓存，
	// GetText 返回 key 本身 → Translate 回退 fallback（组件包内中文原文）。
	html := compileI18nDoc(t)

	for _, want := range []string{
		`aria-label="站点导航"`,
		`aria-label="上一张"`,
		`aria-label="下一张"`,
		`title="视频"`,
		`>天<`, `>时<`, `>分<`, `>秒<`,
		`>提交<`,
		`data-slide-label="第 %s 张"`,
		`aria-label="评分 4.5 / 5"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("缺词条时应回退中文原文，产物缺少 %q\nHTML=%s", want, html)
		}
	}

	// 绝不输出空属性 / 裸 key / 破损占位符。
	for _, bad := range []string{`aria-label=""`, `title=""`, "site.component.", "%!s(MISSING)"} {
		if strings.Contains(html, bad) {
			t.Fatalf("产物出现非法输出 %q", bad)
		}
	}
}

// TestCompileComponentTextEmptyTranslatorStillFallsBack 取词函数返回空串（或为 nil）时，
// RenderContext.Text 必须继续回退中文原文——空串绝不能进入产物。
func TestCompileComponentTextEmptyTranslatorStillFallsBack(t *testing.T) {
	html := compileI18nDoc(t, WithTranslator(func(key, fallback string) string { return "" }))

	if !strings.Contains(html, `aria-label="站点导航"`) {
		t.Fatalf("取词函数返回空串时应回退中文原文\nHTML=%s", html)
	}
	if strings.Contains(html, `aria-label=""`) || strings.Contains(html, `title=""`) {
		t.Fatalf("取词函数返回空串时产物出现空属性\nHTML=%s", html)
	}
}

// TestResolveCompileI18nLang 语言维度预留：显式语言优先，未指定取默认语言，
// 取词函数未注入时按语言构造（且绝不返回 nil）。
func TestResolveCompileI18nLang(t *testing.T) {
	// 未指定语言 → 默认语言（i18n 未初始化时为内置 zh-CN）。
	lang, fn := resolveCompileI18n(&compileConfig{})
	if lang != i18n.GetDefaultLang() {
		t.Fatalf("未指定语言应取默认语言 %q，实际 %q", i18n.GetDefaultLang(), lang)
	}
	if fn == nil {
		t.Fatal("取词函数不应为 nil")
	}

	// 显式语言优先（P2 接入 /{lang}/ 时装配层经此注入）。
	lang, _ = resolveCompileI18n(&compileConfig{lang: " en-US "})
	if lang != "en-US" {
		t.Fatalf("显式语言应规范化并保留，实际 %q", lang)
	}

	// 注入的取词函数原样使用（不覆盖）。
	custom := func(key, fallback string) string { return "custom" }
	_, fn = resolveCompileI18n(&compileConfig{lang: "en-US", translate: custom})
	if fn("x", "y") != "custom" {
		t.Fatal("注入的取词函数应被原样使用")
	}

	// WithLanguage / WithTranslator 选项写入 compileConfig。
	cfg := &compileConfig{}
	WithLanguage(" en-US ")(cfg)
	WithTranslator(custom)(cfg)
	if cfg.lang != "en-US" || cfg.translate == nil {
		t.Fatalf("选项未正确写入 compileConfig: lang=%q translate=%v", cfg.lang, cfg.translate != nil)
	}
}

// TestRenderContextTextNilSafe RenderContext.Text 自身兜底：nil 接收者 / nil 取词函数
// / 空 fallback 均不 panic 且不返回空串。
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

// TestCompileComponentTextUserValueWins 用户显式填写的文案优先：只有内置缺省才参与翻译。
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

// TestCompileComponentTextBrokenPlaceholder rating 词条缺 %s 占位符时按纯文本输出，
// 绝不产生 %!s(MISSING) 之类的破损文本。
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
		return "Rated" // 词条丢失占位符
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
