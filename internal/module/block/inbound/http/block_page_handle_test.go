package blockhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	view "go_wp/pkg/view"

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

// 失败文案由共享出口按错误事实取词：业务错误保留自己的 key，非业务错误落归口文案，
// 驱动原文（SQLSTATE / 表名）不得泄漏到页面。页面侧不再有私有取词包装。
func TestBlockFailureFactUsesSharedExit(t *testing.T) {
	c := blockErrTestContext()
	business := view.PageErrorFactQuiet(c, blockErrScene, blockcontract.ErrNotFound)
	if business.Key == "" || business.Key == view.InternalMsg().Key {
		t.Fatalf("业务错误应保留自己的 key，实际 %+v", business)
	}
	for _, err := range []error{
		errors.New(`pq: relation "blocks" does not exist (SQLSTATE 42P01)`),
		gorm.ErrRecordNotFound,
	} {
		fact := view.PageErrorFactQuiet(c, blockErrScene, err)
		if fact.Key != view.InternalMsg().Key {
			t.Fatalf("非业务错误应落归口事实，实际 %+v（原文 %q）", fact, err.Error())
		}
		for _, leak := range []string{"pq:", "blocks", "SQLSTATE", "record not found"} {
			if strings.Contains(fact.Key, leak) || strings.Contains(fact.Fallback, leak) {
				t.Fatalf("驱动原文泄漏到错误事实里（%q 出现在 %+v）", leak, fact)
			}
		}
	}
}
