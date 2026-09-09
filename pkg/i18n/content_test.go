package i18n

// content_test.go — 内容翻译基础设施（多语言 P5a，docs/06-D-site-i18n.md §7）单元验证。
//
// 覆盖（不查库，全部用注入的 stub 存储）：
//  1. 跳过规则（§7.6）：纯数字 / 纯符号 / 空白跳过；含数字的句子照常翻译；
//  2. 内容寻址（§7.4）：hash 只对原文（TrimSpace 后）做 sha256，改原文即变；
//  3. 语境命名（§7.5）：{类型}.{字段名}，缺一为空串；
//  4. 取词与兜底（§7.7 / 决策 F8）：命中用译文、未命中回退原文、不跨语境回退、
//     nil 取词器 / nil 存储 / 存储报错均不 panic 且回退原文、绝不返回空串；
//  5. 批量语义：一次构造只调用存储一次（不逐条查库），索引在构造后冻结。

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubContentStore 记录调用次数与入参，返回预设结果或错误。
type stubContentStore struct {
	targets map[string]string
	err     error

	calls  int
	lang   string
	hashes []string
}

func (s *stubContentStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	s.calls++
	s.lang = lang
	s.hashes = hashes
	if s.err != nil {
		return nil, s.err
	}
	return s.targets, nil
}

// TestContentSkipRule 跳过规则：整体纯数字/纯符号/空白不进表，含数字的句子照常翻译。
func TestContentSkipRule(t *testing.T) {
	skip := []string{"2024", "42", "3.14", "-5", "1,024", "→", "--", "·", "|", "…", "", "   ", "\t\n", "＋－", "12:30", "50%"}
	for _, v := range skip {
		if ShouldTranslateContent(v) {
			t.Errorf("ShouldTranslateContent(%q) = true，应为 false（跳过）", v)
		}
	}

	translate := []string{"共 42 件", "2010 年成立", "Learn more", "了解更多", "A1", "v2.0 beta", "3 件", "第 1 章"}
	for _, v := range translate {
		if !ShouldTranslateContent(v) {
			t.Errorf("ShouldTranslateContent(%q) = false，应为 true（照常翻译）", v)
		}
	}
}

// TestContentHashAndContext 内容寻址与语境命名。
func TestContentHashAndContext(t *testing.T) {
	hash := ContentHash("了解更多")
	if len(hash) != 64 {
		t.Fatalf("hash 应为 64 字符，实际 %d：%q", len(hash), hash)
	}
	if hash != strings.ToLower(hash) {
		t.Fatalf("hash 应为小写十六进制，实际 %q", hash)
	}
	if hash != ContentHash("了解更多") {
		t.Fatalf("同一原文两次 hash 应一致")
	}
	if hash != ContentHash("  了解更多  ") {
		t.Fatalf("原文统一 TrimSpace，前后空白不应改变 hash")
	}
	if hash == ContentHash("了解 更多") {
		t.Fatalf("内部空白不同应视为不同文本")
	}
	if hash == ContentHash("了解详情") {
		t.Fatalf("改原文后 hash 必须变化（旧译文自动失效）")
	}

	if got := ContentContext("core.button", "text"); got != "core.button.text" {
		t.Fatalf("ContentContext = %q，期望 core.button.text", got)
	}
	if got := ContentContext("product", "name"); got != "product.name" {
		t.Fatalf("ContentContext = %q，期望 product.name", got)
	}
	if got := ContentContext("", "text"); got != "" {
		t.Fatalf("类型为空应返回空串，实际 %q", got)
	}
	if got := ContentContext("core.button", "  "); got != "" {
		t.Fatalf("字段为空应返回空串，实际 %q", got)
	}

	h := ContentHash("了解更多")
	if ContentIndexKey(h, "core.button.text") == ContentIndexKey(h, "product.cta") {
		t.Fatalf("同一 hash 的不同语境索引键必须不同")
	}
	if !strings.Contains(ContentIndexKey(h, "core.button.text"), "\x00") {
		t.Fatalf("索引键应含 \x00 分隔符")
	}
}

// TestContentTranslatorBatchLoad 一次构造只查一次存储（批量取数，不逐条查库）。
func TestContentTranslatorBatchLoad(t *testing.T) {
	hitHash := ContentHash("了解更多")
	missHash := ContentHash("联系我们")
	store := &stubContentStore{targets: map[string]string{
		ContentIndexKey(hitHash, "core.button.text"): "Learn more",
		ContentIndexKey(hitHash, "product.cta"):      "Shop now",
	}}

	tr := NewContentTranslatorWith(context.Background(), store, "en-US", []string{hitHash, missHash})

	if store.calls != 1 {
		t.Fatalf("应只调用存储一次，实际 %d 次", store.calls)
	}
	if store.lang != "en-US" {
		t.Fatalf("语言应透传 en-US，实际 %q", store.lang)
	}
	if len(store.hashes) != 2 {
		t.Fatalf("hash 集合应一次传入 2 个，实际 %v", store.hashes)
	}
	if tr.Size() != 2 {
		t.Fatalf("索引应命中 2 条，实际 %d", tr.Size())
	}
	if tr.Lang() != "en-US" {
		t.Fatalf("Lang = %q", tr.Lang())
	}
}

// TestContentTranslatorTranslateAndFallback 取词与兜底链。
func TestContentTranslatorTranslateAndFallback(t *testing.T) {
	hitHash := ContentHash("了解更多")
	store := &stubContentStore{targets: map[string]string{
		ContentIndexKey(hitHash, "core.button.text"): "Learn more",
	}}
	tr := NewContentTranslatorWith(context.Background(), store, "en-US", []string{hitHash})

	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "Learn more" {
		t.Fatalf("命中应返回译文，实际 %q", got)
	}
	if got := tr.Misses(); got != 0 {
		t.Fatalf("命中不应计入缺失，实际 %d", got)
	}

	// 同文本不同语境：目标 context 无行 → 回退原文（不跨语境回退）。
	if got := tr.TranslateContent("了解更多", "product.cta"); got != "了解更多" {
		t.Fatalf("跨语境不应回退到其他 context 的译文，实际 %q", got)
	}
	if got := tr.Misses(); got != 1 {
		t.Fatalf("未命中应计入缺失，实际 %d", got)
	}

	// 空语境 → 回退原文，且不计入缺失（未参与取词）。
	if got := tr.TranslateContent("了解更多", ""); got != "了解更多" {
		t.Fatalf("空语境应回退原文，实际 %q", got)
	}
	if got := tr.Misses(); got != 1 {
		t.Fatalf("空语境不应计入缺失，实际 %d", got)
	}

	// 跳过规则命中的取值 → 原样返回，不计入缺失。
	if got := tr.TranslateContent("2024", "core.button.text"); got != "2024" {
		t.Fatalf("纯数字应原样返回，实际 %q", got)
	}
	if got := tr.TranslateContent("→", "core.button.text"); got != "→" {
		t.Fatalf("纯符号应原样返回，实际 %q", got)
	}
	if got := tr.Misses(); got != 1 {
		t.Fatalf("跳过取值不应计入缺失，实际 %d", got)
	}

	// 原文变了（hash 变）→ 未命中 → 回退新原文。
	if got := tr.TranslateContent("了解详情", "core.button.text"); got != "了解详情" {
		t.Fatalf("改原文后应回退新原文，实际 %q", got)
	}
}

// TestContentTranslatorNeverPanicsOrErrors 兜底铁律：nil 取词器 / nil 存储 / 存储报错均回退原文。
func TestContentTranslatorNeverPanicsOrErrors(t *testing.T) {
	var nilTranslator *ContentTranslator
	if got := nilTranslator.TranslateContent("了解更多", "core.button.text"); got != "了解更多" {
		t.Fatalf("nil 取词器应回退原文，实际 %q", got)
	}
	if nilTranslator.Lang() != "" || nilTranslator.Size() != 0 || nilTranslator.Misses() != 0 {
		t.Fatalf("nil 取词器诊断方法应返回零值")
	}

	// 存储为 nil。
	tr := NewContentTranslatorWith(context.Background(), nil, "en-US", []string{ContentHash("了解更多")})
	if tr.Size() != 0 {
		t.Fatalf("nil 存储应得空索引，实际 %d", tr.Size())
	}
	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "了解更多" {
		t.Fatalf("nil 存储应回退原文，实际 %q", got)
	}

	// 存储报错（表缺失 / 查询失败）。
	broken := &stubContentStore{err: errors.New("relation \"sys_translation\" does not exist")}
	tr = NewContentTranslatorWith(context.Background(), broken, "en-US", []string{ContentHash("了解更多")})
	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "了解更多" {
		t.Fatalf("存储报错应回退原文，实际 %q", got)
	}
	if broken.calls != 1 {
		t.Fatalf("应尝试过一次查询，实际 %d", broken.calls)
	}

	// 非空原文的任何路径都不返回空串。
	for _, ctxName := range []string{"", "core.button.text", "unknown.field"} {
		if got := tr.TranslateContent("联系我们", ctxName); got == "" {
			t.Fatalf("语境 %q 下不应返回空串", ctxName)
		}
	}
}

// TestContentTranslatorSnapshotFrozen 索引在构造后冻结：存储侧后续变化不影响本次取词。
func TestContentTranslatorSnapshotFrozen(t *testing.T) {
	hash := ContentHash("了解更多")
	store := &stubContentStore{targets: map[string]string{
		ContentIndexKey(hash, "core.button.text"): "Learn more",
	}}
	tr := NewContentTranslatorWith(context.Background(), store, "en-US", []string{hash})

	// 构建中途改「表」（stub 里的 map）→ 已构造的取词器不受影响。
	store.targets[ContentIndexKey(hash, "core.button.text")] = "Changed"

	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "Learn more" {
		t.Fatalf("索引应冻结，实际 %q", got)
	}
}
