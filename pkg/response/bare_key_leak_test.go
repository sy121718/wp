package response

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

// 裸 key 泄漏围栏：JSON 出口的 message 必须是「一句话」，
// 不许因为词库缺词就把内部 key（或带 key 的兜底）交给前端。
// 判据是语义而非字面量：key 出现在 message 里即失败。

func missingKeyCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/probe", nil)
	return c
}

func withEmptyWord(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(map[string]map[string]string{}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })
}

// ArgsText 缺词 + 空兜底：此前 lookupText 会原样返回 key。
func TestArgsTextMissingWordEmptyFallbackGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	const key = "product.err.notSeeded"
	got := ArgsText(c, key, "", nil)
	if strings.Contains(got, key) {
		t.Fatalf("缺词且无兜底时 message 含裸 key：%q", got)
	}
	if got != InternalText(c) {
		t.Fatalf("缺词应给归口文案 %q，实际 %q", InternalText(c), got)
	}
}

// ArgsText 缺词 + 兜底等于 key：同样不算兜底。
func TestArgsTextMissingWordFallbackEqualsKeyGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	const key = "order.err.alreadyPaid"
	if got := ArgsText(c, key, key, nil); strings.Contains(got, key) {
		t.Fatalf("兜底等于 key 时 message 仍含裸 key：%q", got)
	}
}

// ArgsText 缺词 + 有参数：不能退成 fmt.Sprintf(key, args) 的残留形态。
func TestArgsTextMissingWordWithArgsGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	const key = "inventory.err.stockInsufficient"
	got := ArgsText(c, key, "", []string{"3"})
	for _, leak := range []string{key, "%!s(", "%!"} {
		if strings.Contains(got, leak) {
			t.Fatalf("message 里有拼接残留 %q：%q", leak, got)
		}
	}
}

// 词条存在时照常取词并填参数 —— 围栏不能把正常链路也压成归口。
func TestArgsTextWithWordFillsPlaceholders(t *testing.T) {
	c := missingKeyCtx(t)
	ensureTranslateFixture(t)

	got := ArgsText(c, "ErrAccountLocked", "", []string{"30 分钟"})
	if want := "账号已被锁定，请 30 分钟 后重试"; got != want {
		t.Fatalf("取词+填参 = %q，want %q", got, want)
	}
}

// 兜底文案是消息事实，合法保留：词库缺词但声明处给了中文时用它。
func TestArgsTextMissingWordKeepsRealFallback(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	const fallback = "商品已下架"
	if got := ArgsText(c, "product.err.delisted", fallback, nil); got != fallback {
		t.Fatalf("有真实兜底时应原样使用，实际 %q", got)
	}
}

// FillText 缺词 + 兜底等于 key：与空兜底同一条归口规则。
func TestFillTextMissingWordFallbackEqualsKeyGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	const key = "page.err.pathOccupied"
	got := FillText(c, key, key, map[string]string{"path": "/a"})
	if strings.Contains(got, key) {
		t.Fatalf("兜底等于 key 时 message 仍含裸 key：%q", got)
	}
	if got != InternalText(c) {
		t.Fatalf("应给归口文案，实际 %q", got)
	}
}

// FillText 词条存在但命名参数没给全：退归口，不把 {placeholder} 拼上屏。
func TestFillTextMissingNamedParamGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	i18n.InjectForTest(map[string]map[string]string{
		"product.err.detailNamed": {i18n.GetDefaultLang(): "缺少 {missing} 字段"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	got := FillText(c, "product.err.detailNamed", "", map[string]string{})
	if got != InternalText(c) {
		t.Fatalf("参数不齐应给归口文案，实际 %q", got)
	}
}

// 归口文案自己必须永远可取：它的 key 与兜底都写死在共享出口，
// 缺词时退回 Fallback 是唯一允许的「key 兜底」场景。
func TestInternalTextAlwaysResolvable(t *testing.T) {
	c := missingKeyCtx(t)
	withEmptyWord(t)

	if got := InternalText(c); got != InternalFallback {
		t.Fatalf("InternalText 缺词时应退回 %q，实际 %q", InternalFallback, got)
	}
}

// ---- 参数与这句话填不进去时：一律归口，绝不吐 fmt 的错误标记 ----
//
// i18n.StringPlaceholderCount 是唯一判定：它既数可填位置数，也拒绝非 %s 动词、
// 显式索引与宽度/精度。只有 (count==len(args), ok) 同时成立才交给 Sprintf，否则归口：
//   少参  → "共 %!s(MISSING) 条"
//   多参  → "共 3 条%!(EXTRA string=多余)"
//   非 %s → "%!d(int=3)"
//   有 %s 却零参数 → 原样 "共 %s 条"（占位符直接上屏）

func withWords(t *testing.T, words map[string]map[string]string) {
	t.Helper()
	i18n.InjectForTest(words, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })
}

func TestArgsTextFewerArgsThanPlaceholdersGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.batchResult": {i18n.GetDefaultLang(): "成功 %s 条，失败 %s 条"},
	})

	got := ArgsText(c, "order.msg.batchResult", "", []string{"3"})
	if got != InternalText(c) {
		t.Fatalf("少参应归口，实际 %q", got)
	}
}

func TestArgsTextMoreArgsThanPlaceholdersGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.oneOnly": {i18n.GetDefaultLang(): "只处理 %s 条"},
	})

	got := ArgsText(c, "order.msg.oneOnly", "", []string{"3", "多余"})
	if got != InternalText(c) {
		t.Fatalf("多参应归口，实际 %q", got)
	}
}

func TestArgsTextNonStringVerbGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.intVerb": {i18n.GetDefaultLang(): "共 %d 条"},
	})

	got := ArgsText(c, "order.msg.intVerb", "", []string{"3"})
	if got != InternalText(c) {
		t.Fatalf("非 %%s 动词应归口（出口只支持字符串参数），实际 %q", got)
	}
}

// 有 %s 却一个参数都不传：是最容易漏的一条，旧实现直接原样返回 "已删除 %s 个页面。"
func TestArgsTextNoArgsButPlaceholderGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"page.bulk.pageAllDeleted": {i18n.GetDefaultLang(): "已删除 %s 个页面。"},
	})

	got := ArgsText(c, "page.bulk.pageAllDeleted", "", nil)
	if got != InternalText(c) {
		t.Fatalf("有占位符却无参数应归口，实际 %q", got)
	}
	if strings.Contains(got, "%s") {
		t.Fatalf("占位符上屏：%q", got)
	}
}

// 索引 / 宽度 / 精度：这些形态里 %s 仍在，但 Sprintf 会因参数消费规则不同而吐标记
// （索引越界、宽度改写输出），所以判定不能只数个数，必须连形态一起拒。
func TestArgsTextExoticFormatGivesInternalText(t *testing.T) {
	c := missingKeyCtx(t)

	for _, tc := range []struct {
		name, word string
		args       []string
	}{
		{"显式索引", "从 %[1]s 到 %[3]s", []string{"a", "b", "c"}},
		{"宽度与精度", "补零 %05s 与 %.2s", []string{"a", "b"}},
		{"星号宽度", "%*s", []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withWords(t, map[string]map[string]string{
				"order.msg.exotic": {i18n.GetDefaultLang(): tc.word},
			})
			got := ArgsText(c, "order.msg.exotic", "", tc.args)
			if got != InternalText(c) {
				t.Fatalf("%s 应归口，实际 %q", tc.name, got)
			}
			for _, leak := range []string{"%!", "%[", "%0", "%*"} {
				if strings.Contains(got, leak) {
					t.Fatalf("%s 漏出格式标记 %q：%q", tc.name, leak, got)
				}
			}
		})
	}
}

// %% 是字面百分号，不是占位符：既不能被当成待填位置，也不能让整句归口。
func TestArgsTextPercentLiteralIsNotPlaceholder(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.discount": {i18n.GetDefaultLang(): "已打 %s 折，相当于 %d%% 优惠"},
	})

	// 句中另有 %d：非 %s 动词存在时仍归口（出口只支持字符串参数）。
	if got := ArgsText(c, "order.msg.discount", "", []string{"8"}); got != InternalText(c) {
		t.Fatalf("含 %%d 应归口，实际 %q", got)
	}

	withWords(t, map[string]map[string]string{
		"order.msg.rate": {i18n.GetDefaultLang(): "占比 %s%%"},
	})
	if got, want := ArgsText(c, "order.msg.rate", "", []string{"80"}), "占比 80%"; got != want {
		t.Fatalf("%%%% 应原样保留字面百分号，实际 %q want %q", got, want)
	}
}

func TestArgsTextExactArityFillsAllPlaceholders(t *testing.T) {
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.batchResult": {i18n.GetDefaultLang(): "成功 %s 条，失败 %s 条"},
	})

	got := ArgsText(c, "order.msg.batchResult", "", []string{"3", "1"})
	if want := "成功 3 条，失败 1 条"; got != want {
		t.Fatalf("个数正确时应正常填充，实际 %q want %q", got, want)
	}
}

// 参数不齐不是缺词：它不该在 i18n 场景留下任何日志记录。
// 反过来，「词条存在 + 参数不齐」若记成缺词，巡检会去补一条早就存在的词。
func TestArgsTextArgMismatchDoesNotLogMissingWord(t *testing.T) {
	dir := initLoggerTo(t)
	c := missingKeyCtx(t)
	withWords(t, map[string]map[string]string{
		"order.msg.needsTwo": {i18n.GetDefaultLang(): "%s 与 %s"},
	})

	for _, args := range [][]string{{"x"}, {"x", "y", "z"}, nil} {
		if got := ArgsText(c, "order.msg.needsTwo", "", args); got != InternalText(c) {
			t.Fatalf("args=%v 应归口，实际 %q", args, got)
		}
	}

	if logs := readSceneLogs(t, dir, missingMessageScene); strings.Contains(logs, "order.msg.needsTwo") {
		t.Fatalf("参数不齐被记成缺词：%s", logs)
	}

	// 同一个上下文里真缺一个词时，日志必须只出现缺词那一条。
	ArgsText(c, "order.err.absentKey", "", []string{"x"})
	logs := readSceneLogs(t, dir, missingMessageScene)
	if !strings.Contains(logs, "order.err.absentKey") {
		t.Fatalf("真缺词必须留日志：%s", logs)
	}
	if strings.Contains(logs, "order.msg.needsTwo") {
		t.Fatalf("缺词日志里混进了参数不齐的条目：%s", logs)
	}
}
