package response

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
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

func newLangRecorder(t *testing.T, lang string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c, w
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

// TestBusinessTextKeepsOnlyMainMessage 守住主 message 只播报公共一句：
// 带 Tail 的业务错误不再把明细拼进 message，两种语言都一样。
func TestBusinessTextKeepsOnlyMainMessage(t *testing.T) {
	injectTestWords(t)
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", testErrZh},
		{"en-US", "Duplicate variant stock"},
	} {
		c := newLangContext(t, tc.lang)
		got := businessText(c, "product", facing.Fact{
			Key: testErrKey, Business: true, Tail: detailTailWithArgs(),
		})
		if got != tc.want {
			t.Fatalf("%s 主 message = %q，want %q", tc.lang, got, tc.want)
		}
		for _, leak := range []string{"SKU-9", "变体 {sku}", "Variant {sku}", "product.err.detail", "："} {
			if strings.Contains(got, leak) {
				t.Fatalf("%s 主 message 混入了明细 %q：%q", tc.lang, leak, got)
			}
		}
		assertNoControlChars(t, tc.lang+"主 message", got)
	}
}

// TestErrorDetailsOfExtractsControlledEncoding 证明受控编码变成结构化 data.details，
// 保留 key 与 args 原样，不在此处二次取词。
func TestErrorDetailsOfExtractsControlledEncoding(t *testing.T) {
	tail := i18n.JoinErrorDetails(
		i18n.ErrorDetail("product.err.detail", "sku", "SKU-9", "n", "3", "max", "2"),
		i18n.ErrorDetail("product.err.detail", "sku", "SKU-10", "n", "4", "max", "3"),
	)
	details := errorDetailsOf(tail)
	if len(details) != 2 {
		t.Fatalf("details 段数 = %d，want 2", len(details))
	}
	if got, _ := details[0]["key"].(string); got != "product.err.detail" {
		t.Fatalf("details[0].key = %q", got)
	}
	args, _ := details[0]["args"].(map[string]string)
	if args["sku"] != "SKU-9" || args["n"] != "3" || args["max"] != "2" {
		t.Fatalf("details[0].args = %#v", args)
	}
	assertNoControlChars(t, "details key", fmt.Sprint(details[0]["key"]))
	assertNoControlChars(t, "details args", fmt.Sprint(details[0]["args"]))
}

// TestErrorDetailsOfDropsUncontrolledText 证明明文 tail、损坏编码与内部原文一律不转成 details。
func TestErrorDetailsOfDropsUncontrolledText(t *testing.T) {
	for _, raw := range []string{
		"",
		"不存在（2 个：a@1，b@1）",
		"private SQLSTATE 23505",
		i18n.ErrDetailSep + "product.err.detail" + i18n.ErrDetailSep + "broken",
		"\\x1fproduct.err.detail",
	} {
		if got := errorDetailsOf(raw); len(got) != 0 {
			t.Fatalf("非受控编码 %q 不应产出 details：%#v", raw, got)
		}
	}
}

type testBlockedError struct{ items []facing.BlockedItem }

func (e *testBlockedError) Error() string                      { return "blocked" }
func (e *testBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

// testBlockedDetailError 同时携带拒绝回执与编码明细，用来证明两者合并进同一份 data。
type testBlockedDetailError struct {
	items []facing.BlockedItem
	tail  string
}

func (e *testBlockedDetailError) Error() string {
	return testErrKey + ": " + e.tail
}

func (e *testBlockedDetailError) Facing() (string, string) { return testErrKey, "" }

func (e *testBlockedDetailError) BlockedItems() []facing.BlockedItem { return e.items }

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

// TestErrorAutoJSONKeepsDetailsOutOfMessage 用真实 JSON 出口证明：
// 主 message 不含 id/详情，data.items/data.details 仍然携带机器可读数据。
func TestErrorAutoJSONKeepsDetailsOutOfMessage(t *testing.T) {
	injectTestWords(t)
	c, w := newLangRecorder(t, "zh-CN")
	err := &testBlockedDetailError{
		items: []facing.BlockedItem{{ID: "v1", Name: "SKU1", ReasonKey: "product.err.keyWithoutSeed"}},
		tail:  detailTailWithArgs(),
	}

	ErrorAuto(c, http.StatusConflict, "product", err)

	if w.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d，want 409", w.Code)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				ReasonKey string `json:"reasonKey"`
			} `json:"items"`
			Details []struct {
				Key  string            `json:"key"`
				Args map[string]string `json:"args"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v %s", err, w.Body.String())
	}
	if body.Message != testErrZh {
		t.Fatalf("message = %q，want %q", body.Message, testErrZh)
	}
	for _, leak := range []string{"v1", "SKU1", "SKU-9", "product.err.", "变体 {sku}"} {
		if strings.Contains(body.Message, leak) {
			t.Fatalf("message 混入 %q：%q", leak, body.Message)
		}
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].ID != "v1" || body.Data.Items[0].ReasonKey != "product.err.keyWithoutSeed" {
		t.Fatalf("data.items 丢失：%+v", body.Data.Items)
	}
	if len(body.Data.Details) != 1 || body.Data.Details[0].Key != "product.err.detail" || body.Data.Details[0].Args["sku"] != "SKU-9" {
		t.Fatalf("data.details 丢失：%+v", body.Data.Details)
	}
}

// TestErrorAutoJSONDropsUncontrolledTail 证明明文 tail、坏编码与未知错误只给公共文案，
// 不泄漏进 message，也不产出 data.details。
func TestErrorAutoJSONDropsUncontrolledTail(t *testing.T) {
	injectTestWords(t)
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"plain-tail", fmt.Errorf("%w: summer-sale", facing.New(testErrKey)), testErrZh},
		{"broken-encoding", fmt.Errorf("%w: %s", facing.New(testErrKey), i18n.ErrDetailSep+"product.err.detail"+i18n.ErrDetailSep+"broken"), testErrZh},
		{"escaped-encoding", fmt.Errorf(`%w: \x1fproduct.err.detail`, facing.New(testErrKey)), testErrZh},
		{"raw-sqlstate", fmt.Errorf("%w: ERROR: duplicate key (SQLSTATE 23505)", facing.New(testErrKey)), testErrZh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newLangRecorder(t, "zh-CN")
			ErrorAuto(c, http.StatusConflict, "product", tc.err)
			var body struct {
				Message string         `json:"message"`
				Data    map[string]any `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是合法 JSON：%v %s", err, w.Body.String())
			}
			if body.Message != tc.want {
				t.Fatalf("message = %q，want %q", body.Message, tc.want)
			}
			for _, leak := range []string{"summer-sale", "SQLSTATE", "23505", "product.err.detail", `\x1f`} {
				if strings.Contains(w.Body.String(), leak) {
					t.Fatalf("响应泄漏 %q：%s", leak, w.Body.String())
				}
			}
			if _, has := body.Data["details"]; has {
				t.Fatalf("非受控 tail 不应产出 data.details：%s", w.Body.String())
			}
		})
	}
}

// TestErrorAutoJSONUnknownErrorNotLeaked 证明非业务错误统一归口，明细不因 data 通道而透出。
func TestErrorAutoJSONUnknownErrorNotLeaked(t *testing.T) {
	injectTestWords(t)
	c, w := newLangRecorder(t, "zh-CN")
	ErrorAuto(c, http.StatusBadRequest, "product", fmt.Errorf("private SQLSTATE 23505"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非业务错误应为 500，实际 %d", w.Code)
	}
	var body struct {
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v %s", err, w.Body.String())
	}
	if body.Message != InternalText(c) {
		t.Fatalf("message = %q，want 归口文案", body.Message)
	}
	for _, leak := range []string{"SQLSTATE", "23505", "private"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Fatalf("响应泄漏 %q：%s", leak, w.Body.String())
		}
	}
}

// TestMessageOfKeepsOnlyMainMessage 守住 MessageOf 与新主消息语义一致，不再拼明细。
func TestMessageOfKeepsOnlyMainMessage(t *testing.T) {
	injectTestWords(t)
	c := newLangContext(t, "zh-CN")
	err := fmt.Errorf("%w: %s", facing.New(testErrKey), detailTailWithArgs())
	if got := MessageOf(c, "product", err); got != testErrZh {
		t.Fatalf("MessageOf = %q，want %q", got, testErrZh)
	}
}
