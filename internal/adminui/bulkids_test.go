package adminui

// bulkids_test.go — 批量 id 超限的**受控文案出口**守卫（自 internal/shell/bulk_test.go 迁入）。
//
// 这里守的不是「文案长什么样」，而是三条会静默失效的性质：
//   · 判据是**类型**（*bulkids.IDsError）而不是 err.Error() 的文本 —— 调用方把内部原文
//     （表名 / SQLSTATE）包进同一个错误链时，一个字都不许跟着出去；
//   · 两个数字取自类型字段（上限 Max 与**去重后**的 Count），不是调用方拿 MaxIDs 重算的；
//   · 未命中 sentinel 一律回落归口文案，原文只进日志。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

// facingCtx 造一个空的 gin 上下文（受控出口只需要当前语言与请求路径）。
func facingCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	target := "/admin/bulk"
	if lang != "" {
		target += "?lang=" + lang
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, target, nil)
	return c
}

// TestBulkIDsFacingTextUsesTypedCounts 命中 sentinel 时给出的文案含**两个数字**（上限与本次条数），
// 且不含任何内部细节指纹（sentinel 的英文诊断文本、类型名都不许出现）。
func TestBulkIDsFacingTextUsesTypedCounts(t *testing.T) {
	err := &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs}
	got := BulkIDsFacingText(facingCtx(t, ""), err)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("应给出受控提示，got %q", got)
	}
	if !strings.Contains(got, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(got, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控文案应含上限与本次条数（当前 N 项由类型给回），got %q", got)
	}
	for _, tok := range []string{"shell:", "bulk ids exceeded", "IDsError"} {
		if strings.Contains(got, tok) {
			t.Fatalf("受控文案里出现了错误原文/类型名 %q：%q", tok, got)
		}
	}
}

// TestBulkIDsFacingTextIgnoresWrappedContext 出口只认**类型**，不认文本：
// 调用方把内部原文（表名 / SQLSTATE）包进同一个错误链时，一个字都不许跟着出去。
func TestBulkIDsFacingTextIgnoresWrappedContext(t *testing.T) {
	wrapped := fmt.Errorf("加载货源失败: SQLSTATE 42P01 relation `inventory_sources`: %w",
		&bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs})
	got := BulkIDsFacingText(facingCtx(t, ""), wrapped)
	if !strings.Contains(got, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控文案应取类型里的 Count，got %q", got)
	}
	for _, tok := range []string{"SQLSTATE", "42P01", "inventory_sources", "加载货源失败"} {
		if strings.Contains(got, tok) {
			t.Fatalf("包装进来的内部上下文 %q 不许出现在文案里：%q", tok, got)
		}
	}
}

// TestBulkIDsFacingTextFallsBackForUnknownError 未命中 sentinel 一律回落归口文案，
// 且原文只进日志 —— 这是「入口对未知错误也接得住」的那一半契约。
func TestBulkIDsFacingTextFallsBackForUnknownError(t *testing.T) {
	got := BulkIDsFacingText(facingCtx(t, ""), errors.New("ERROR: relation `orders` does not exist (SQLSTATE 42P01)"))
	for _, tok := range []string{"SQLSTATE", "42P01", "relation", "orders"} {
		if strings.Contains(got, tok) {
			t.Fatalf("未命中的错误原文不许出现在文案里（%q）：%q", tok, got)
		}
	}
	// 归口文案可能是 key 本身（i18n 未加载）或译文，两者都算命中。
	if got != MsgInternalError && !strings.Contains(got, "内部错误") {
		t.Fatalf("未命中应回落 MsgInternalError（或其译文），got %q", got)
	}
}

// TestBulkIDsFacingTextFollowsRequestLanguage 命中 sentinel 时按**当前语言**给文案。
//
// 只说「受控」不够：受控文案还必须是可翻译的 —— 否则中文原文会原样出现在英文界面上。
// 用 i18n.InjectForTest 注入词条（生产由迁移 277 seed 进 sys_i18n），因此不依赖数据库；
// 语言走 query `lang`（response.requestLanguage 取值链里的一环）。
func TestBulkIDsFacingTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {
			"zh-CN": bulkIDsTooManyTemplate,
			"en-US": "At most %s items can be operated at once; %s selected, please split into batches.",
		},
	}, nil)

	err := &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs}

	zh := BulkIDsFacingText(facingCtx(t, "zh-CN"), err)
	if !strings.Contains(zh, "一次最多操作") {
		t.Fatalf("中文请求应给中文文案，got %q", zh)
	}
	en := BulkIDsFacingText(facingCtx(t, "en-US"), err)
	if !strings.Contains(en, "At most") {
		t.Fatalf("英文请求应给英文文案（词条缺失时会退回中文兜底），got %q", en)
	}
	for _, want := range []string{strconv.Itoa(bulkids.MaxIDs), strconv.Itoa(bulkids.MaxIDs + 1)} {
		if !strings.Contains(en, want) {
			t.Fatalf("英文文案也应带上限与本次条数 %s，got %q", want, en)
		}
	}
}
