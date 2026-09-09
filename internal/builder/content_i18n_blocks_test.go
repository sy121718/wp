package builder

// content_i18n_blocks_test.go — 块内文本的内容翻译（多语言 P5b 缺口补齐，docs/06-D §7.7/§15.11）。
//
// 覆盖四条契约：
//  1. 候选收集覆盖块：本页 AST + 页眉/页脚绑定块（extraBlockIDs）+ core.globalref 内联块
//     （递归，含引用环保护），且与「只扫本页 AST」的 CollectContentCandidates 有可观测差异；
//  2. 内联块文本可翻译：取词器用「含块」的候选集合构造后，globalref 展开的块内文本随语言变化；
//  3. 回退：块内文本无译文时产物与「不接入内容翻译」逐字节一致；
//  4. 缺失统计包含块内：只登记本页译文时，块内未命中计入 Misses。
//
// 不依赖数据库：块解析用内存假解析器，译文用内存假存储（与 content_i18n_test.go 同套基建）。

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// 块文档（块内文本一律用与页面不同的字面量，便于断言来源）。
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

// blockRefPageDoc 页面文档：本页 heading + settings.structure 页眉绑定 + 引用块（promo）的 globalref。
const blockRefPageDoc = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"p5b-blocks","description":"p5b-blocks"},"structure":{"headerBlockId":"header-block"}},"root":[` +
	`{"id":"ph1","type":"core.heading","props":{"text":"本页标题"}},` +
	`{"id":"ref1","type":"core.globalref","props":{"blockId":"promo-block"}}]}`

// blockDocs 假块库：块 ID → 块文档 JSON。
var blockDocs = map[string]string{
	"header-block": blockHeaderDoc,
	"promo-block":  blockPromoDoc,
	"inner-block":  blockInnerDoc,
}

// blockResolverFunc 把函数适配为 core.BlockResolver（builder 测试内联使用）。
type blockResolverFunc func(blockID string) ([]*core.Node, error)

// ResolveBlockRoot 实现 core.BlockResolver。
func (f blockResolverFunc) ResolveBlockRoot(blockID string) ([]*core.Node, error) { return f(blockID) }

// fakeBlockResolver 内存版块解析器（记录每个块被解析的次数）。
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

// deepCandidates 用假解析器收集「页面 + extra 块 + globalref 块」的候选。
func deepCandidates(t *testing.T, doc string, extra []string, docs map[string]string) []ContentCandidate {
	t.Helper()
	p, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(docs)
	return CollectContentCandidatesDeep(p, extra, resolve)
}

// TestCollectContentCandidatesDeepIncludesBlocks 候选收集覆盖块内文本（缺口修复的机器证据）。
func TestCollectContentCandidatesDeepIncludesBlocks(t *testing.T) {
	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	// 只扫本页 AST：块内文本不在候选里（改造前的行为，缺口本身）。
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
		{"core.heading.text", "块标题-页眉"}, // settings.structure 页眉块
		{"core.button.text", "块按钮-页眉"},  // 页眉块
		{"core.text.text", "块正文-促销"},    // core.globalref 内联块
		{"core.button.text", "块按钮-内层"},  // 块内再引用块（递归）
	} {
		if !got[want[0]+"|"+want[1]] {
			t.Fatalf("候选集合缺少 %s=%q；实际: %+v", want[0], want[1], deep)
		}
	}
	// 确定性：同一输入两次收集结果一致（顺序按 (context, source) 字典序）。
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

// TestCollectContentCandidatesDeepCycleAndDedupe 引用环不死循环、同块只解析一次。
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

// TestContentTranslationBlockTextTranslated 内联块文本随语言切换（缺口修复）。
func TestContentTranslationBlockTextTranslated(t *testing.T) {
	store := &contentFakeStore{rows: map[string]string{}}
	cands := deepCandidates(t, blockRefPageDoc, []string{"header-block"}, blockDocs)
	translations := map[string]string{
		"core.heading.text|本页标题":   "Page heading",
		"core.heading.text|块标题-页眉": "Block heading",
		"core.button.text|块按钮-页眉":  "Block button",
		"core.text.text|块正文-促销":    "Block body",
		"core.button.text|块按钮-内层":  "Inner button",
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

// TestContentTranslationBlockFallbackOriginal 块内文本无译文时逐字节回退原文。
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

// TestContentTranslationBlockMissesCounted 缺失统计包含块内未命中。
func TestContentTranslationBlockMissesCounted(t *testing.T) {
	p, err := ParsePage([]byte(blockRefPageDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	resolve, _ := fakeBlockResolver(blockDocs)
	cands := CollectContentCandidatesDeep(p, []string{"header-block"}, resolve)

	// 只登记本页标题的译文：globalref 内联块的两处文本必然未命中。
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
