package view

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
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

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

func TestBulkIDsFacingTextUsesTypedCounts(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
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

func TestBulkIDsFacingTextIgnoresWrappedContext(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
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

func TestBulkIDsFacingTextFallsBackForUnknownError(t *testing.T) {
	got := BulkIDsFacingText(facingCtx(t, ""), errors.New("ERROR: relation `orders` does not exist (SQLSTATE 42P01)"))
	for _, tok := range []string{"SQLSTATE", "42P01", "relation", "orders"} {
		if strings.Contains(got, tok) {
			t.Fatalf("未命中的错误原文不许出现在文案里（%q）：%q", tok, got)
		}
	}

	if got != MsgInternalError && !strings.Contains(got, "内部错误") {
		t.Fatalf("未命中应回落 MsgInternalError（或其译文），got %q", got)
	}
}

func TestBulkIDsFacingTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {
			"zh-CN": bulkids.TooManyTemplate,
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

func TestBulkIDsFactHasNoParameterTail(t *testing.T) {
	err := &bulkids.IDsError{Max: 200, Count: 201}
	fact, ok := facing.FactOf(err)
	if !ok || fact.Key != bulkids.MsgTooMany+"|200|201" || fact.Tail != "" {
		t.Fatalf("参数属于完整key，不是明细: ok=%v fact=%+v", ok, fact)
	}
	i18n.InjectForTest(map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	if got := BulkIDsFacingText(facingCtx(t, "zh-CN"), err); got != "一次最多操作 200 项，当前 201 项，请分批进行" {
		t.Fatalf("消息必须精确匹配且无参数尾巴: %q", got)
	}
}
