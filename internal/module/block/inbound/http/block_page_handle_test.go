package blockhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	view "go_wp/pkg/view"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"

	"gorm.io/gorm"
)

func blockErrTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/blocks", nil)
	return c
}

func TestBlockSentinelsAreBusinessErrors(t *testing.T) {
	sentinels := []error{
		blockcontract.ErrParamRequired,
		blockcontract.ErrNotFound,
		blockcontract.ErrProjectNotFound,
		blockcontract.ErrProjectRequired,
		blockcontract.ErrNameRequired,
		blockcontract.ErrInvalidDoc,
		blockcontract.ErrInvalidKind,
		blockcontract.ErrInvalidCategory,
		blockcontract.ErrDuplicate,
		blockcontract.ErrInvalidReuseMode,
		blockcontract.ErrBlockInUse,
	}
	for _, err := range sentinels {
		if !response.IsBusinessError(err) {
			t.Fatalf("业务错误 %v 过不了共享形态判据（会被当成内部故障：通用提示 + 日志）", err)
		}
		fact, ok := facing.FactOf(err)
		if !ok {
			t.Fatalf("业务错误 %v 未被判定为可展示", err)
		}
		if fact.Key != err.Error() {
			t.Fatalf("业务错误的词条 key 应等于其 Error()（enums 常量即 key），实际 %q vs %q", fact.Key, err.Error())
		}
	}
}

func TestBlockErrFactKeepsBusinessKeyWithoutInternalDetail(t *testing.T) {
	// 引用明细是界面事实，只走 BlockedItems 由失败片段逐条取词；
	// Error() 与错误事实都只交业务 key，绝不把 page_document 这类内部枚举带出去。
	err := blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
		{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
	})
	fact, ok := facing.FactOf(err)
	if !ok || fact.Key != blockcontract.ErrBlockInUse.Error() {
		t.Fatalf("复合错误应认出业务 key，实际 key=%q ok=%v（原文 %q）", fact.Key, ok, err.Error())
	}
	if strings.TrimSpace(fact.Tail) != "" {
		t.Fatalf("错误事实不得携带内部枚举明文，实际 %+v", fact)
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatal("引用错误必须实现 BlockedError，失败片段才能列出具体引用")
	}
	items := blocked.BlockedItems()
	if len(items) != 1 || items[0].Name != "/pricing" || items[0].ReasonKey != "MsgBlockUsagePageDocument" {
		t.Fatalf("引用条目应交界面事实（名称/位置/词条），实际 %+v", items)
	}
}

func TestBlockErrTextBusinessErrorKeepsKey(t *testing.T) {
	c := blockErrTestContext()
	got := blockErrText(c, blockcontract.ErrNotFound)
	if strings.TrimSpace(got) == "" {
		t.Fatal("业务错误必须给出一句可展示文案（空串会让失败在页面上完全不可见）")
	}

	if got == response.MsgText(c, view.InternalMsg()) {
		t.Fatalf("业务错误被吞成归口文案：%q", got)
	}
}

func TestBlockErrTextNonBusinessGivesInternal(t *testing.T) {
	c := blockErrTestContext()
	cases := []error{
		errors.New(`pq: relation "blocks" does not exist (SQLSTATE 42P01)`),
		gorm.ErrRecordNotFound,
	}
	for _, err := range cases {
		got := blockErrText(c, err)
		if got != response.MsgText(c, view.InternalMsg()) {
			t.Fatalf("非业务错误应落归口文案，实际 %q（原文 %q）", got, err.Error())
		}
		for _, leak := range []string{"pq:", "blocks", "SQLSTATE", "record not found"} {
			if strings.Contains(got, leak) {
				t.Fatalf("驱动原文泄漏到页面文案里（%q 出现在 %q）", leak, got)
			}
		}
	}
}
