package response

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/jsonx"
)

// TestMissingMessageLogsKeyLangScenePath 守住「词库缺词可定位」：
// 业务错误声明了 key、词库没有词条时，前端只拿到归口提示，日志里必须留 key / 语言 / 调用处 / 路径。
func TestMissingMessageLogsKeyLangScenePath(t *testing.T) {
	dir := initLoggerTo(t)
	rec := newProbeCtx(t, func(c *gin.Context) {
		ErrorStatusAuto(c, "product", facing.New("product.err.notSeeded"))
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("业务错误状态应为 400，得到 %d", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "product.err.notSeeded") {
		t.Fatalf("响应体不该出现裸 key：%s", body)
	}

	logs := readSceneLogs(t, dir, missingMessageScene)
	if logs == "" {
		t.Fatal("缺词必须留一条日志，否则线上没法定位要补哪条词")
	}
	record := lastMissingMessageRecord(t, logs, "product.err.notSeeded")
	for field, want := range map[string]string{
		"key":    "product.err.notSeeded",
		"path":   "/probe",
		"caller": "product",
	} {
		if got := record[field]; got != want {
			t.Fatalf("缺词日志字段 %s 应为 %q，实际 %q（整行：%v）", field, want, got, record)
		}
	}
	if lang, _ := record["lang"].(string); lang == "" {
		t.Fatalf("缺词日志必须带请求语言：%v", record)
	}
}

// lastMissingMessageRecord 把最后一条缺词日志解析成结构再取字段：
// 固定子串断言会被字段顺序、宽度与转义骗过，要断言 key/lang/caller/path 的准确值只能先解析。
// 注意 logger 自己也会写 caller 字段（zap 的调用点），本模块记的调用场景同名，因此按 key 值定位本行。
func lastMissingMessageRecord(t *testing.T, logs, wantKey string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var record map[string]any
		if err := jsonx.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("缺词日志不是合法 JSON 行（%v）：%s", err, line)
		}
		if record["key"] == wantKey {
			return record
		}
	}
	t.Fatalf("日志里没有 key=%s 的缺词记录：%s", wantKey, logs)
	return nil
}

// TestMissingMessageCarriesNoTailOrSecrets 守住日志纪律：带 Tail 的业务错误也只记定位字段，
// 请求参数（可能含用户输入）与明细原文、堆栈都不进这条日志 —— 缺词不是故障，不需要排查上下文。
func TestMissingMessageCarriesNoTailOrSecrets(t *testing.T) {
	dir := initLoggerTo(t)
	const secret = "user-secret-token-123"
	const tailDetail = "detail-should-not-be-logged"

	newProbeCtx(t, func(c *gin.Context) {
		c.Request.URL.RawQuery = "token=" + secret
		// 生产里带 Tail 的业务错误走 facing.FactOf(err) → businessText，这里直接喂同一形状的事实。
		businessText(c, "product", facing.Fact{Key: "product.err.notSeeded", Tail: tailDetail, Business: true})
	})

	logs := readSceneLogs(t, dir, missingMessageScene)
	if logs == "" {
		t.Fatal("缺词必须留一条日志，否则线上没法定位要补哪条词")
	}
	for _, leak := range []string{secret, "token=", tailDetail, `"tail"`, `"stack"`, "goroutine"} {
		if strings.Contains(logs, leak) {
			t.Fatalf("缺词日志不该带 %q：%s", leak, logs)
		}
	}
}

// TestBusinessTextDoesNotLogWhenTranslationIsGenericHint 守住「不猜缺词」：
// 词库里真有这条词、只是译文恰好等于归口文案时，不算缺词。
func TestBusinessTextDoesNotLogWhenTranslationIsGenericHint(t *testing.T) {
	dir := initLoggerTo(t)
	i18n.InjectForTest(map[string]map[string]string{
		"product.err.sameAsInternal": {i18n.GetDefaultLang(): InternalFallback},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	var out string
	newProbeCtx(t, func(c *gin.Context) {
		out = businessText(c, "product", facing.Fact{Key: "product.err.sameAsInternal", Business: true})
	})
	if out != InternalFallback {
		t.Fatalf("取词结果应为该词条译文：%q", out)
	}
	if logs := readSceneLogs(t, dir, missingMessageScene); logs != "" {
		t.Fatalf("译文等于归口文案不等于缺词：%s", logs)
	}
}

// TestDetailTextDoesNotLogWhenPlaceholderArgsMissing 守住「不猜缺词」：
// 词条存在、只是命名占位符没给全时退成归口文案 —— 那是参数问题，不是词库缺词。
func TestDetailTextDoesNotLogWhenPlaceholderArgsMissing(t *testing.T) {
	dir := initLoggerTo(t)
	i18n.InjectForTest(map[string]map[string]string{
		"product.err.detailNamed": {i18n.GetDefaultLang(): "缺少 {missing} 字段"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	var out, want string
	newProbeCtx(t, func(c *gin.Context) {
		want = InternalText(c)
		out = detailText(c, i18n.ErrorDetail("product.err.detailNamed", "name", "x"))
	})
	if out != want {
		t.Fatalf("命名占位符没填全应退成归口文案，实际 %q（期望 %q）", out, want)
	}
	if logs := readSceneLogs(t, dir, missingMessageScene); logs != "" {
		t.Fatalf("参数不齐不是缺词，不该记缺词日志：%s", logs)
	}
}
