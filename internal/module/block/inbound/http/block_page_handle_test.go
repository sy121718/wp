package blockhttp

// block_page_handle_test.go — 块错误文案归口的护栏（判定收敛到 adminui.PageErrorFact）。
//
// 现象（用户报告，历史）：在 /admin/blocks 点了「新建并编辑」之后，浏览器停在一张只有
// ErrBlockDuplicate 字样的空白页上 —— 没有页面壳、不是中文、也没有回列表的入口。
// 成因之一在这里守住：**业务错误必须能被认出来**。认不出来的会被当成内部故障（通用提示），
// 而同名冲突、被引用、类型非法这些都是用户自己就能改正的业务失败；漏认一条，用户看到的
// 提示与日志里那条 error 就对不上号。
//
// 判据已不再由本模块持有：原先的 blockErrSentinels + errors.Is 是**第二份真相**
// —— service 换了措辞、或某个 key 改由别的模块提供就静默失配。现在只有一条共享判据
//（pkg/response.IsBusinessError 的**形态**：`Err*` / `Msg*` 常量名，或 `模块.类别.语义` 的
// key，经 adminui.PageErrorFact 使用）。所以本文件守两件事：
//  1. 每个 sentinel 的值都过得了那条形态判据 —— 新增一个「值既不是 key 也不是常量名」的
//     sentinel 会在这里变红，而不是静默退化成「系统内部错误」；
//  2. 非业务错误（驱动 / ORM 原文）一律落归口文案，原文一个字节都不上页面。
//
// 另一半（「失败必须回到页面」）已随页面写路由一起退役：写动作现在由前端直调 JSON API，
// 页面提示页那条通道整批删除。写路径现在的守卫在：
//   · block_bulk_delete_test.go —— 批量端点（受控 JSON、逐条结果、不泄原文）；
//   · blocks_stale_impact_render_test.go 的 TestBlocksWriteFormsAreJSONWired ——
//     三个写入口都接 JSON，且不得残留页面 POST。

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

// blockErrTestContext 一个最小可用的 gin 上下文（取词链要读请求语言）。
//
// 不断言具体译文：i18n 的缓存是包级共享的（同包其它用例会 InjectForTest），
// 断言锁定在「命中的是哪条 key / 走的是哪条链」这类与词条无关的量上。
func blockErrTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/blocks", nil)
	return c
}

// TestBlockSentinelsAreBusinessErrors 每一个业务 sentinel 都要被共享判据认出来。
//
// 漏认一条 → 它被当成内部错误 → 用户只看到「系统内部错误，请稍后重试」，
// 而日志里那条 error 与用户看到的提示对不上号。
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

// TestBlockErrFactSplitsCompositeError 复合错误（`key: 明细`）按冒号切一刀、拿前段判定。
//
// 删除被拒时 service 给的是 `ErrBlockInUse: page_document /pricing (main); …`：
// 整体过不了形态判据（pkg/response 的正则是**锚定的**），必须能从前段认出 key ——
// 否则一眼可改的业务失败会退化成「系统内部错误」。
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

// TestBlockErrTextBusinessErrorKeepsKey 业务错误 → 自己的词条（缺词条时回落 key 本身）。
func TestBlockErrTextBusinessErrorKeepsKey(t *testing.T) {
	c := blockErrTestContext()
	got := blockErrText(c, blockcontract.ErrNotFound)
	if strings.TrimSpace(got) == "" {
		t.Fatal("业务错误必须给出一句可展示文案（空串会让失败在页面上完全不可见）")
	}
	// 业务分支与归口分支必须长得不一样：两者相同就意味着判定失效（全都归口）。
	if got == adminui.MsgText(c, adminui.InternalMsg()) {
		t.Fatalf("业务错误被吞成归口文案：%q", got)
	}
}

// TestBlockErrTextNonBusinessGivesInternal 基础设施错误一律归口：原文一个字节都不上页面。
func TestBlockErrTextNonBusinessGivesInternal(t *testing.T) {
	c := blockErrTestContext()
	cases := []error{
		errors.New(`pq: relation "blocks" does not exist (SQLSTATE 42P01)`),
		gorm.ErrRecordNotFound, // 驱动 / ORM 的内部错误，不是本模块的业务语义
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
