package producthttp

// product_bulk_i18n_language_test.go — 多段合成句的**当前语言一致性**回归。
//
// 为什么必须有这一条：变体清单保存的回执是一句合成句（模板 + 跳过段前缀 + 原因），
// 两段都经 productBulkTextOf 取当前语言模板再 Sprintf。只要有人把某一处重新写成中文
// 常量（或忘了带 c），中文环境下一切正常，英文页面上那条回执就**静默变回中文** ——
// 不报错、不记日志。
//
// 批量删除与批量改价的回执**不在这里**：它们的页面 POST 已删除，结论走 JSON 回执
// （productBulkDeletedView / productBulkPricedView 给计数与逐条原因），整句由
// ui/json_write.js 按当前页面语言拼装 —— 判据在 json_write.test.cjs 里。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条，不建库、不起装配。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// productBulkInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var productBulkInjectedEntries = map[string]map[string]string{
	productenums.BulkVariantNoChange: {"zh-CN": "变体清单与库里一致，没有需要保存的变化。", "en-US": "EN-VARIANT-NOCHANGE"},
	productenums.BulkVariantSaved:    {"zh-CN": "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。", "en-US": "EN-VARIANT %s %s %s"},
	productenums.BulkVariantSkipped:  {"zh-CN": "跳过 %s 个：%s", "en-US": "EN-VARIANT-SKIP %s %s"},
}

// productBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
func productBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/product/variant/save", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestProductVariantSaveNoticeFollowsRequestLanguage 变体清单保存的合成句随当前语言拼装。
func TestProductVariantSaveNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(productBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := productBulkLangCtx(t, "en-US")

	// 1) 模板段与计数：三个计数都按当前语言填。
	saved := adminui.MsgText(en, variantSaveMsg(en, &productdto.SaveVariantListResp{Created: 1, Updated: 2, Deleted: 3}))
	if saved != "EN-VARIANT 1 2 3" {
		t.Errorf("变体清单保存应按当前语言拼装，实际 %q", saved)
	}

	// 2) 跳过段：前缀 + 逐条原因都要按当前语言。
	withSkip := adminui.MsgText(en, variantSaveMsg(en, &productdto.SaveVariantListResp{
		Created: 1,
		Skipped: []productdto.VariantSaveSkip{{Reason: productenums.VariantSkipHasStock}},
	}))
	if !strings.HasPrefix(withSkip, "EN-VARIANT 1 0 0 EN-VARIANT-SKIP 1 ") {
		t.Errorf("变体清单保存（含跳过）应按当前语言拼装，实际 %q", withSkip)
	}

	// 3) 没有变化：单段模板也走同一条取词路径。
	if noChange := adminui.MsgText(en, variantSaveMsg(en, &productdto.SaveVariantListResp{})); noChange != "EN-VARIANT-NOCHANGE" {
		t.Errorf("「没有需要保存的变化」应按当前语言，实际 %q", noChange)
	}

	// 4) 中文请求走中文词条（productBulkTextOf 的文本形态）。
	zh := productBulkLangCtx(t, "zh-CN")
	if text := productBulkTextOf(zh, productVariantSaveSaved); text != "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。" {
		t.Errorf("中文请求应取中文词条，实际 %q", text)
	}
}
