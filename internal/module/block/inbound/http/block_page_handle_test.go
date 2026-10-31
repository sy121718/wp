package blockhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
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
		fact, ok := adminui.BusinessErrFact(err)
		if !ok {
			t.Fatalf("业务错误 %v 未被判定为可展示", err)
		}
		if fact.Key != err.Error() {
			t.Fatalf("业务错误的词条 key 应等于其 Error()（enums 常量即 key），实际 %q vs %q", fact.Key, err.Error())
		}
	}
}

func TestBlockErrFactSplitsCompositeError(t *testing.T) {
	err := blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
		{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
	})
	fact, ok := adminui.BusinessErrFact(err)
	if !ok || fact.Key != blockcontract.ErrBlockInUse.Error() {
		t.Fatalf("复合错误应从前段认出业务 key，实际 key=%q ok=%v（原文 %q）", fact.Key, ok, err.Error())
	}
	if strings.TrimSpace(fact.Tail) == "" {
		t.Fatalf("明细段应原样带出（引用类别与定位由模块自己解码），实际空：%+v", fact)
	}
}

func TestBlockErrTextBusinessErrorKeepsKey(t *testing.T) {
	c := blockErrTestContext()
	got := blockErrText(c, blockcontract.ErrNotFound)
	if strings.TrimSpace(got) == "" {
		t.Fatal("业务错误必须给出一句可展示文案（空串会让失败在页面上完全不可见）")
	}

	if got == adminui.MsgText(c, adminui.InternalMsg()) {
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
		if got != adminui.MsgText(c, adminui.InternalMsg()) {
			t.Fatalf("非业务错误应落归口文案，实际 %q（原文 %q）", got, err.Error())
		}
		for _, leak := range []string{"pq:", "blocks", "SQLSTATE", "record not found"} {
			if strings.Contains(got, leak) {
				t.Fatalf("驱动原文泄漏到页面文案里（%q 出现在 %q）", leak, got)
			}
		}
	}
}
