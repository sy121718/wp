package builder

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

const (
	blockHeaderDoc = `{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"hb1","type":"core.heading","props":{"text":"块标题-页眉"}},` +
		`{"id":"hb2","type":"core.button","props":{"text":"块按钮-页眉","action":"internal","value":"/x"}}]}`
	blockPromoDoc = `{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"pb1","type":"core.text","props":{"text":"块正文-促销"}},` +
		`{"id":"pb2","type":"core.globalref","props":{"blockId":"inner-block"}}]}`
	blockInnerDoc = `{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"ib1","type":"core.button","props":{"text":"块按钮-内层","action":"internal","value":"/y"}}]}`
)

const blockRefPageDoc = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"p5b-blocks","description":"p5b-blocks"},"structure":{"headerBlockId":"header-block"}},"root":[` +
	`{"id":"ph1","type":"core.heading","props":{"text":"本页标题"}},` +
	`{"id":"ref1","type":"core.globalref","props":{"blockId":"promo-block"}}]}`

var blockDocs = map[string]string{
	"header-block": blockHeaderDoc,
	"promo-block":  blockPromoDoc,
	"inner-block":  blockInnerDoc,
}

type blockResolverFunc func(blockID string) ([]*core.Node, error)

func (f blockResolverFunc) ResolveBlockRoot(blockID string) ([]*core.Node, error) { return f(blockID) }

func fakeBlockResolver(docs map[string]string) (func(string) ([]*core.Node, error), map[string]int) {
	loads := map[string]int{}
	return func(blockID string) ([]*core.Node, error) {
		loads[blockID]++
		doc, ok := docs[blockID]
		if !ok {
			return nil, errBlockUnavailable
		}
		p, err := ParsePage([]byte(doc))
		if err != nil {
			return nil, err
		}
		return p.Root, nil
	}, loads
}

type blockUnavailableError struct{}

func (blockUnavailableError) Error() string { return "块不可用" }

var errBlockUnavailable = blockUnavailableError{}

func deepCandidates(t *testing.T, doc string, extra []string, docs map[string]string) []ContentCandidate {
	t.Helper()
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(docs)

	return AppendSEOCandidates(p, CollectContentCandidatesDeep(p, extra, resolve))
}

func TestCollectContentCandidatesDeepIncludesBlocks(t *testing.T) {
	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	for _, c := range CollectContentCandidates(p) {
		if strings.HasPrefix(c.Source, "块") {
			t.Fatalf("只扫本页 AST 时不应出现块内文本: %+v", c)
		}
	}

	extra := []string{p.Settings.Structure.HeaderBlockID}
	deep := deepCandidates(t, blockRefPageDoc, extra, blockDocs)
	got := map[string]bool{}
	for _, c := range deep {
		got[c.Context+"|"+c.Source] = true
	}
	for _, want := range [][2]string{
		{"core.heading.text", "本页标题"},
		{"core.heading.text", "块标题-页眉"},
		{"core.button.text", "块按钮-页眉"},
		{"core.text.text", "块正文-促销"},
		{"core.button.text", "块按钮-内层"},
	} {
		if !got[want[0]+"|"+want[1]] {
			t.Fatalf("候选集合缺少 %s=%q；实际: %+v", want[0], want[1], deep)
		}
	}

	again := deepCandidates(t, blockRefPageDoc, extra, blockDocs)
	if len(again) != len(deep) {
		t.Fatalf("两次收集数量不一致: %d vs %d", len(deep), len(again))
	}
	for i := range deep {
		if deep[i] != again[i] {
			t.Fatalf("候选顺序不确定: %+v vs %+v", deep[i], again[i])
		}
	}
}

func TestCollectContentCandidatesDeepCycleAndDedupe(t *testing.T) {
	cyclic := map[string]string{
		"a": `{"settings":{},"root":[{"id":"a1","type":"core.text","props":{"text":"环-A"}},{"id":"a2","type":"core.globalref","props":{"blockId":"b"}}]}`,
		"b": `{"settings":{},"root":[{"id":"b1","type":"core.text","props":{"text":"环-B"}},{"id":"b2","type":"core.globalref","props":{"blockId":"a"}}]}`,
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"r1","type":"core.globalref","props":{"blockId":"a"}},{"id":"r2","type":"core.globalref","props":{"blockId":"a"}}]}`
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, loads := fakeBlockResolver(cyclic)
	cands := CollectContentCandidatesDeep(p, nil, resolve)

	sources := map[string]bool{}
	for _, c := range cands {
		sources[c.Source] = true
	}
	if !sources["环-A"] || !sources["环-B"] {
		t.Fatalf("引用环两侧的块文本都应进候选: %+v", cands)
	}
	if loads["a"] != 1 || loads["b"] != 1 {
		t.Fatalf("同一块只应解析一次（环保护 + 去重），实际 %v", loads)
	}
}

func TestContentTranslationBlockTextTranslated(t *testing.T) {
	store := &contentFakeStore{rows: map[string]string{}}
	cands := deepCandidates(t, blockRefPageDoc, []string{"header-block"}, blockDocs)
	translations := map[string]string{
		"core.heading.text|本页标题":   "Page heading",
		"core.heading.text|块标题-页眉": "Block heading",
		"core.button.text|块按钮-页眉":  "Block button",
		"core.text.text|块正文-促销":    "Block body",
		"core.button.text|块按钮-内层":  "Inner button",

		"page.seo.title|p5b-blocks":       "p5b-blocks en",
		"page.seo.description|p5b-blocks": "p5b-blocks en",
	}
	for _, c := range cands {
		if target, ok := translations[c.Context+"|"+c.Source]; ok {
			store.rows["en-US|"+i18n.ContentHash(c.Source)+"|"+c.Context] = target
		}
	}

	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(blockDocs)
	translator := i18n.NewContentTranslatorWith(context.Background(), store, "en-US", ContentHashes(cands))

	res, err := Compile(p,
		WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"),
		WithBlockResolver(blockResolverFunc(resolve)), WithContentTranslator(translator))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{"Page heading", "Block body", "Inner button"} {
		if !strings.Contains(res.HTML, want) {
			t.Fatalf("en-US 产物缺少 %q；HTML=%s", want, res.HTML)
		}
	}
	for _, bad := range []string{"本页标题", "块正文-促销", "块按钮-内层"} {
		if strings.Contains(res.HTML, bad) {
			t.Fatalf("en-US 产物不应出现原文 %q；HTML=%s", bad, res.HTML)
		}
	}
	if got := translator.Misses(); got != 0 {
		t.Fatalf("全部候选均有译文时不应有缺失，实际 misses=%d", got)
	}
	if store.loads != 1 {
		t.Fatalf("取词器必须一次批量查库，实际 %d 次", store.loads)
	}
}

func TestContentTranslationBlockFallbackOriginal(t *testing.T) {
	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(blockDocs)
	cands := CollectContentCandidatesDeep(p, []string{"header-block"}, resolve)
	empty := &contentFakeStore{rows: map[string]string{}}
	translator := i18n.NewContentTranslatorWith(context.Background(), empty, "en-US", ContentHashes(cands))

	withTranslator, err := Compile(p,
		WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"),
		WithBlockResolver(blockResolverFunc(resolve)), WithContentTranslator(translator))
	if err != nil {
		t.Fatalf("Compile(with): %v", err)
	}
	withoutTranslator, err := Compile(p,
		WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"),
		WithBlockResolver(blockResolverFunc(resolve)))
	if err != nil {
		t.Fatalf("Compile(without): %v", err)
	}
	if withTranslator.HTML != withoutTranslator.HTML {
		t.Fatalf("无译文时块内文本必须回退原文（产物应与接入前一致）；with=%s；without=%s", withTranslator.HTML, withoutTranslator.HTML)
	}
	if !strings.Contains(withTranslator.HTML, "块正文-促销") {
		t.Fatalf("回退产物应保留块内原文；HTML=%s", withTranslator.HTML)
	}
}

func TestContentTranslationBlockMissesCounted(t *testing.T) {
	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(blockDocs)
	cands := CollectContentCandidatesDeep(p, []string{"header-block"}, resolve)

	store := &contentFakeStore{rows: map[string]string{}}
	for _, c := range cands {
		if c.Context == "core.heading.text" && c.Source == "本页标题" {
			store.rows["en-US|"+i18n.ContentHash(c.Source)+"|"+c.Context] = "Page heading"
		}
	}
	translator := i18n.NewContentTranslatorWith(context.Background(), store, "en-US", ContentHashes(cands))
	res, err := Compile(p,
		WithComponentSet(i18nTestComponentSet(t)), WithLanguage("en-US"),
		WithBlockResolver(blockResolverFunc(resolve)), WithContentTranslator(translator))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.HTML, "Page heading") {
		t.Fatalf("本页译文应生效；HTML=%s", res.HTML)
	}
	if got := translator.Misses(); got < 2 {
		t.Fatalf("块内未命中必须计入 Misses，实际 misses=%d；HTML=%s", got, res.HTML)
	}
}
