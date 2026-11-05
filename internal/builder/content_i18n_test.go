package builder

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

type contentFakeStore struct {
	rows  map[string]string
	loads int
}

func (s *contentFakeStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	s.loads++
	out := map[string]string{}
	for _, h := range hashes {
		for k, v := range s.rows {
			if strings.HasPrefix(k, lang+"|"+h+"|") {
				out[i18n.ContentIndexKey(h, strings.TrimPrefix(k, lang+"|"+h+"|"))] = v
			}
		}
	}
	return out, nil
}

func contentStoreWith(rows ...[3]string) *contentFakeStore {
	s := &contentFakeStore{rows: map[string]string{}}
	for _, r := range rows {
		lang, ctxName, target := r[0], r[1], r[2]
		s.rows[lang+"|"+i18n.ContentHash(contentSources[ctxName])+"|"+ctxName] = target
	}
	return s
}

var contentSources = map[string]string{
	"core.button.text":      "了解更多",
	"core.heading.text":     "了解更多",
	"core.heading.subtitle": "副标题",
	"core.image.alt":        "公司前台",
	"core.image.caption":    "前台",
	"core.form.submitLabel": "发送询价",
	"core.form.label":       "你的问题",
}

const contentDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "p5b", "description": "p5b"}},
  "root": [
    {"id": "btn1", "type": "core.button", "props": {"text": "了解更多", "action": "internal", "value": "/shop"}},
    {"id": "hd1", "type": "core.heading", "props": {"text": "了解更多", "subtitle": "副标题"}},
    {"id": "img1", "type": "core.image", "props": {"src": "/storage/a.jpg", "alt": "公司前台", "caption": "前台"}},
    {"id": "frm1", "type": "core.form", "props": {"submitLabel": "发送询价", "fields": [{"type": "text", "name": "q", "label": "你的问题"}]}},
    {"id": "num1", "type": "core.button", "props": {"text": "2024", "action": "internal", "value": "/a"}},
    {"id": "sym1", "type": "core.button", "props": {"text": "→", "action": "internal", "value": "/b"}}
  ]
}`

const contentSkipDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "", "description": ""}},
  "root": [
    {"id": "n1", "type": "core.button", "props": {"text": "2024", "action": "internal", "value": "/a"}},
    {"id": "n2", "type": "core.button", "props": {"text": "→", "action": "internal", "value": "/b"}}
  ]
}`

const contentDefaultSubmitDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "", "description": ""}},
  "root": [
    {"id": "frm2", "type": "core.form", "props": {"fields": [{"type": "text", "name": "q", "label": "问题"}]}}
  ]
}`

func compileContentDoc(t *testing.T, lang string, store i18n.ContentStore, extra ...CompileOption) (string, *i18n.ContentTranslator) {
	t.Helper()
	p, err := ParsePage([]byte(contentDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{WithComponentSet(i18nTestComponentSet(t)), WithLanguage(lang)}
	var translator *i18n.ContentTranslator
	if store != nil {
		translator = i18n.NewContentTranslatorWith(context.Background(), store, lang, ContentHashes(CollectContentCandidates(p)))
		opts = append(opts, WithContentTranslator(translator))
	}
	opts = append(opts, extra...)
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML, translator
}

func TestContentTranslationLangDiffers(t *testing.T) {
	store := contentStoreWith(
		[3]string{"en-US", "core.button.text", "Learn more"},
		[3]string{"en-US", "core.heading.text", "About us"},
		[3]string{"en-US", "core.image.alt", "Reception"},
	)
	zh, _ := compileContentDoc(t, "zh-CN", store)
	en1, _ := compileContentDoc(t, "en-US", store)
	en2, _ := compileContentDoc(t, "en-US", store)

	if zh == en1 {
		t.Fatal("zh-CN 与 en-US 产物不应相同")
	}
	if en1 != en2 {
		t.Fatal("同一语言两次构建必须字节相同（确定性不变量）")
	}
	if !strings.Contains(en1, "Learn more") {
		t.Fatalf("en-US 产物缺少按钮译文\nHTML=%s", en1)
	}
	if !strings.Contains(en1, "Reception") {
		t.Fatalf("en-US 产物缺少 alt 译文\nHTML=%s", en1)
	}
	if strings.Contains(en1, "了解更多") {
		t.Fatalf("en-US 产物不应出现按钮原文\nHTML=%s", en1)
	}
	if !strings.Contains(zh, "了解更多") {
		t.Fatalf("zh-CN 产物应保持原文\nHTML=%s", zh)
	}
}

func TestContentTranslationFallbackOriginal(t *testing.T) {
	empty := &contentFakeStore{rows: map[string]string{}}
	withTranslator, _ := compileContentDoc(t, "en-US", empty)
	withoutTranslator, _ := compileContentDoc(t, "en-US", nil)

	if withTranslator != withoutTranslator {
		t.Fatalf("无译文时必须回退原文（产物应与接入前一致）\nwith=%s\nwithout=%s", withTranslator, withoutTranslator)
	}
	if !strings.Contains(withTranslator, "了解更多") || !strings.Contains(withTranslator, "公司前台") {
		t.Fatalf("无译文时产物应保留原文\nHTML=%s", withTranslator)
	}
}

func TestContentTranslationSkipNumericSymbol(t *testing.T) {
	p, err := ParsePage([]byte(contentDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	for _, c := range CollectContentCandidates(p) {
		if c.Source == "2024" || c.Source == "→" {
			t.Fatalf("纯数字/纯符号不应成为候选: %+v", c)
		}
	}

	page, err := ParsePage([]byte(contentSkipDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if got := CollectContentCandidates(page); len(got) != 0 {
		t.Fatalf("跳过取值不应产生候选: %+v", got)
	}
	translator := i18n.NewContentTranslatorWith(context.Background(), &contentFakeStore{rows: map[string]string{}}, "en-US", nil)
	res, err := Compile(page, WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"), WithContentTranslator(translator))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if translator.Misses() != 0 {
		t.Fatalf("纯数字/纯符号不计缺失，实际 misses=%d", translator.Misses())
	}
	if !strings.Contains(res.HTML, "2024") || !strings.Contains(res.HTML, "→") {
		t.Fatalf("跳过取值应原样输出\nHTML=%s", res.HTML)
	}
}

func TestContentTranslationContextSeparated(t *testing.T) {
	store := contentStoreWith(
		[3]string{"en-US", "core.button.text", "Learn more"},
		[3]string{"en-US", "core.heading.text", "About us"},
	)
	html, _ := compileContentDoc(t, "en-US", store)

	if !strings.Contains(html, "Learn more") {
		t.Fatalf("按钮语境译文缺失\nHTML=%s", html)
	}
	if !strings.Contains(html, "About us") {
		t.Fatalf("标题语境译文缺失\nHTML=%s", html)
	}

	onlyButton := contentStoreWith([3]string{"en-US", "core.button.text", "Learn more"})
	html2, _ := compileContentDoc(t, "en-US", onlyButton)
	if !strings.Contains(html2, "了解更多") {
		t.Fatalf("未命中语境的字段应回退原文\nHTML=%s", html2)
	}
}

func TestContentTranslationOncePerPage(t *testing.T) {
	store := contentStoreWith([3]string{"en-US", "core.button.text", "Learn more"})
	_, _ = compileContentDoc(t, "en-US", store)
	if store.loads != 1 {
		t.Fatalf("内容译文必须一次批量查库，实际 %d 次", store.loads)
	}
}

func TestContentTranslationUserValueWins(t *testing.T) {
	store := contentStoreWith(
		[3]string{"en-US", "core.form.submitLabel", "Send inquiry"},
		[3]string{"en-US", "core.form.label", "Your question"},
	)
	html, _ := compileContentDoc(t, "en-US", store, WithTranslator(func(key, fallback string) string {
		if key == "site.component.form.submit" {
			return "SUBMIT-KEY"
		}
		return fallback
	}))

	if !strings.Contains(html, "Send inquiry") {
		t.Fatalf("用户填写的提交文案应走内容翻译\nHTML=%s", html)
	}
	if strings.Contains(html, "SUBMIT-KEY") {
		t.Fatalf("用户已填写时，sys_i18n 缺省文案不应覆盖\nHTML=%s", html)
	}
	if !strings.Contains(html, "Your question") {
		t.Fatalf("嵌套字段（fields[].label）应参与翻译\nHTML=%s", html)
	}

	p, err := ParsePage([]byte(contentDefaultSubmitDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	translator := i18n.NewContentTranslatorWith(context.Background(), store, "en-US", ContentHashes(CollectContentCandidates(p)))
	res, err := Compile(p,
		WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"), WithContentTranslator(translator),
		WithTranslator(func(key, fallback string) string {
			if key == "site.component.form.submit" {
				return "SUBMIT-KEY"
			}
			return fallback
		}))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.HTML, "SUBMIT-KEY") {
		t.Fatalf("未填写时缺省文案应走 sys_i18n\nHTML=%s", res.HTML)
	}
}

func TestTranslatableFieldsDeclared(t *testing.T) {
	want := map[string][]string{
		"core.button":    {"text"},
		"core.heading":   {"text", "subtitle"},
		"core.image":     {"alt", "title", "caption"},
		"core.card":      {"title", "text", "label"},
		"core.table":     {"caption", "headers", "rows"},
		"core.form":      {"submitLabel", "label", "placeholder", "options"},
		"core.faq":       {"question", "answer"},
		"core.list":      {"text"},
		"core.tabs":      {"label"},
		"core.accordion": {"title"},
		"core.infobox":   {"title", "text"},
		"core.counter":   {"prefix", "suffix", "label"},
	}
	for typeName, fields := range want {
		got := core.TranslatableFields(typeName)
		for _, f := range fields {
			if !got[f] {
				t.Fatalf("组件 %s 的白名单缺少字段 %q（实际 %v）", typeName, f, got)
			}
		}
	}

	for _, bad := range []string{"value", "link", "color", "src", "width", "variant", "action"} {
		for _, typeName := range core.Types() {
			if core.TranslatableFields(typeName)[bad] {
				t.Fatalf("组件 %s 不应把字段 %q 纳入翻译白名单", typeName, bad)
			}
		}
	}

	if err := core.ValidateTranslatable(&struct{ Text string }{}, []string{"txet"}); err == nil {
		t.Fatal("拼错的白名单字段名应校验失败")
	}
	if err := core.ValidateTranslatable(&struct {
		Text string `json:"text"`
	}{}, []string{"text"}); err != nil {
		t.Fatalf("合法白名单不应报错: %v", err)
	}
	if err := core.ValidateTranslatable(nil, []string{"a.b"}); err == nil {
		t.Fatal("含点号的字段名应校验失败")
	}
}
