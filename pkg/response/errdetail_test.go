package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

const (
	testErrKey   = "product.err.variantStockDuplicated"
	testErrZh    = "变体库存重复"
	testDetailZH = "变体 {sku} 的数量 {n} 超过上限 {max}"
	testDetailEN = "Variant {sku} quantity {n} exceeds the limit {max}"
)

func newLangContext(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x?lang="+lang, nil)
	return c
}

// injectTestWords 在本用例缓存上叠加词条，结束后还原包内既有夹具（translateSeeds）：
// ensureTranslateFixture 的 sync.Once 不会重跑，cleanup 清成空表会让后跑的用例一起缺词。
func injectTestWords(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	words := map[string]map[string]string{
		testErrKey: {"zh-CN": testErrZh, "en-US": "Duplicate variant stock"},
		"product.err.detail": {
			"zh-CN": testDetailZH,
			"en-US": testDetailEN,
		},
		InternalKey: {"zh-CN": InternalFallback, "en-US": "Internal error, please try again later"},
	}
	codes := map[string]int{}
	for key, byLang := range translateSeeds {
		words[key] = byLang
	}
	for key, code := range translateHttpCodes {
		codes[key] = code
	}
	i18n.InjectForTest(words, codes)
}

func detailTailWithArgs() string {
	return i18n.ErrorDetail("product.err.detail", "sku", "SKU-9", "n", "3", "max", "2")
}

func assertNoControlChars(t *testing.T, label, text string) {
	t.Helper()
	if strings.ContainsAny(text, i18n.ErrDetailSep+i18n.ErrDetailPartSep) {
		t.Fatalf("%s 里出现了编码明细的控制字符: %q", label, text)
	}
	if i18n.EncodedDetailLike(text) {
		t.Fatalf("%s 里出现了编码明细的转义残留: %q", label, text)
	}
}

func TestBusinessTextRendersChineseDetailWithArgs(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	got := businessText(c, "product", facing.Fact{
		Key: testErrKey, Business: true, Tail: detailTailWithArgs(),
	})
	if want := testErrZh + "：变体 SKU-9 的数量 3 超过上限 2"; got != want {
		t.Fatalf("成品串 = %q，want %q", got, want)
	}
	assertNoControlChars(t, "中文明细", got)
}

func TestBusinessTextRendersEnglishDetailWithArgs(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "en-US")

	got := businessText(c, "product", facing.Fact{
		Key: testErrKey, Business: true, Tail: detailTailWithArgs(),
	})
	if want := "Duplicate variant stock：Variant SKU-9 quantity 3 exceeds the limit 2"; got != want {
		t.Fatalf("成品串 = %q，want %q", got, want)
	}
}

func TestBusinessTextJoinsMultipleDetailParts(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	tail := i18n.JoinErrorDetails(
		i18n.ErrorDetail("product.err.detail", "sku", "SKU-9", "n", "3", "max", "2"),
		i18n.ErrorDetail("product.err.detail", "sku", "SKU-10", "n", "4", "max", "3"),
	)
	got := businessText(c, "product", facing.Fact{Key: testErrKey, Business: true, Tail: tail})
	want := testErrZh + "：变体 SKU-9 的数量 3 超过上限 2；变体 SKU-10 的数量 4 超过上限 3"
	if got != want {
		t.Fatalf("多段明细 = %q，want %q", got, want)
	}
}

func TestDetailTextFallsBackWhenArgsIncomplete(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	// 词条要 {sku}/{n}/{max} 三个参数，这里只给一个：不能把没替换的占位符当译文。
	tail := i18n.ErrorDetail("product.err.detail", "sku", "SKU-9")
	got := detailText(c, tail)
	if got != InternalText(c) {
		t.Fatalf("参数不足应给共享归口文案 %q，实际 %q", InternalText(c), got)
	}
	if strings.Contains(got, "{") {
		t.Fatalf("参数不足时不该留下占位符: %q", got)
	}
	assertNoControlChars(t, "缺参明细", got)
}

func TestDetailTextFallsBackWhenWordMissing(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	got := detailText(c, i18n.ErrorDetail("product.err.detailNotSeeded", "sku", "SKU-9"))
	if got != InternalText(c) {
		t.Fatalf("明细缺词应给共享归口文案 %q，实际 %q", InternalText(c), got)
	}
	if strings.Contains(got, "detailNotSeeded") {
		t.Fatalf("明细缺词不该露 key: %q", got)
	}
}

func TestDetailTextDropsBrokenEncoding(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	for _, tail := range []string{
		i18n.ErrDetailSep + "product.err.detail" + "broken",
		"\\x1fproduct.err.detail",
	} {
		got := detailText(c, tail)
		assertNoControlChars(t, "异常编码", got)
		if got != InternalText(c) {
			t.Fatalf("异常编码应给共享归口文案，实际 %q", got)
		}
	}
}

func TestDetailTextKeepsPlainTail(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	const plain = "不存在（2 个：a@1，b@1）"
	if got := detailText(c, plain); got != plain {
		t.Fatalf("受控业务明细应原样保留，实际 %q", got)
	}
}

func TestBusinessTextHidesMissingWordKey(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	got := businessText(c, "product", facing.Fact{Key: "product.err.keyWithoutSeed", Business: true})
	if got != InternalText(c) {
		t.Fatalf("缺词应给共享归口文案，实际 %q", got)
	}
	if strings.Contains(got, "keyWithoutSeed") {
		t.Fatalf("缺词不该露 key: %q", got)
	}
}

type testBlockedError struct{ items []facing.BlockedItem }

func (e *testBlockedError) Error() string                      { return "blocked" }
func (e *testBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

func TestBlockedItemsReuseBusinessText(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")

	err := &testBlockedError{items: []facing.BlockedItem{
		{ID: "v1", Name: "SKU1", ReasonKey: "product.err.keyWithoutSeed"},
		{ID: "v2", Name: "SKU2", ReasonKey: "product.err.skipHasStock", Fallback: "仍有库存，未删除"},
	}}
	data, ok := blockedDataOf(c, "product", err)
	if !ok {
		t.Fatal("拒绝回执应产出 items")
	}
	rows, _ := data["items"].([]gin.H)
	if len(rows) != 2 {
		t.Fatalf("items 行数 = %d，want 2", len(rows))
	}
	if got, _ := rows[0]["message"].(string); got != InternalText(c) {
		t.Errorf("无兜底的行应给共享归口文案，实际 %q", got)
	}
	if got, _ := rows[1]["message"].(string); got != "仍有库存，未删除" {
		t.Errorf("带兜底的行应保留兜底文案，实际 %q", got)
	}
	if key, _ := rows[0]["reasonKey"].(string); key != "product.err.keyWithoutSeed" {
		t.Errorf("reasonKey 仍应是原始 key（机器可读字段），实际 %q", key)
	}
}
