package producthttp

// product_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归。
//
// 为什么必须有这一条：写侧（各页 BulkDelete / bulkPricingResultMsg / variantSaveNotice）与
// 读侧（productNoticeTexts / productVariantNoticeMatches）共用 productBulkTextOf 这一个取法，
// 但「共用」是结构事实、不是可观测事实 —— 只要有人把读侧候选重新写成中文常量（或忘了带 c），
// 中文环境下一切正常，英文页面上真实的回执却被判成伪造而**静默消失**
//（?done= 落空串、?err= 落归口文案），既没有报错也没有日志。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条，不建库、不起装配。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// productBulkInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var productBulkInjectedEntries = map[string]map[string]string{
	productenums.BulkTagDone:             {"zh-CN": "已删除 %s 个标签", "en-US": "EN-TAG %s"},
	productenums.BulkTagPartial:          {"zh-CN": "已删除 %s 个，%s 个未能删除（标签不存在或已被删除）", "en-US": "EN-TAG-PART %s %s"},
	productenums.BulkAttrDone:            {"zh-CN": "已删除 %s 个属性组（连同其全部属性值）", "en-US": "EN-ATTR %s"},
	productenums.BulkAttrPartial:         {"zh-CN": "已删除 %s 个，%s 个未能删除（属性组不存在或被商品引用）", "en-US": "EN-ATTR-PART %s %s"},
	productenums.BulkCategoryDone:        {"zh-CN": "已删除 %s 个分类", "en-US": "EN-CAT %s"},
	productenums.BulkCategoryPartial:     {"zh-CN": "已删除 %s 个，%s 个未能删除（有子分类或被商品引用）", "en-US": "EN-CAT-PART %s %s"},
	productenums.BulkBrandDone:           {"zh-CN": "已删除 %s 个品牌", "en-US": "EN-BRAND %s"},
	productenums.BulkBrandPartial:        {"zh-CN": "已删除 %s 个，%s 个未能删除（品牌不存在或被商品引用）", "en-US": "EN-BRAND-PART %s %s"},
	productenums.BulkProductDone:         {"zh-CN": "已删除 %s 个商品（连同其全部变体）", "en-US": "EN-PRODUCT %s"},
	productenums.BulkProductPartial:      {"zh-CN": "已删除 %s 个，%s 个未能删除（商品不存在或被其它数据引用）", "en-US": "EN-PRODUCT-PART %s %s"},
	productenums.BulkPricingNoneSelected: {"zh-CN": "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。", "en-US": "EN-PRICE-NONE-SELECTED"},
	productenums.BulkPricingNoChange:     {"zh-CN": "按该规则算下来没有价格变化：%s 个商品已是目标价，未写入调价记录。", "en-US": "EN-PRICE-NOCHANGE %s"},
	productenums.BulkPricingApplied:      {"zh-CN": "已按规则改价：共改 %s 个变体（%s 个商品已是目标价）。", "en-US": "EN-PRICE-APPLIED %s %s"},
	productenums.BulkPricingAllSkip:      {"zh-CN": "没有可改价的变体：%s 个商品被跳过（%s）。", "en-US": "EN-PRICE-SKIP %s %s"},
	productenums.BulkPricingPartial:      {"zh-CN": "已改 %s 个变体，另有 %s 个商品被跳过（%s）。", "en-US": "EN-PRICE-PART %s %s %s"},
	productenums.BulkVariantNoChange:     {"zh-CN": "变体清单与库里一致，没有需要保存的变化。", "en-US": "EN-VARIANT-NOCHANGE"},
	productenums.BulkVariantSaved:        {"zh-CN": "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。", "en-US": "EN-VARIANT %s %s %s"},
	productenums.BulkVariantSkipped:      {"zh-CN": "跳过 %s 个：%s", "en-US": "EN-VARIANT-SKIP %s %s"},
}

// productBulkLangCtx 构造一个绑定了 Accept-Language、且 done/err 都带同一个值的上下文。
func productBulkLangCtx(t *testing.T, lang, raw string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	q := url.Values{}
	if raw != "" {
		q.Set("done", raw)
		q.Set("err", raw)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/products?"+q.Encode(), nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestProductBulkNoticeFollowsRequestLanguage 每个批量结论都随当前语言，且读侧认得。
func TestProductBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(productBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := productBulkLangCtx(t, "en-US", "")

	// 1) 十个批量删除模板：渲染出来的实例读侧必须认得（否则英文页面上回执静默消失）。
	for _, tpl := range productBulkResultTemplates {
		text := productBulkTextOf(en, tpl)
		if !strings.HasPrefix(text, "EN-") {
			t.Errorf("%s 没有取到英文词条，实际 %q", tpl.key, text)
			continue
		}
		args := make([]any, strings.Count(text, "%s"))
		for i := range args {
			args[i] = "3"
		}
		rendered := fmt.Sprintf(text, args...)
		if got := productPageDone(productBulkLangCtx(t, "en-US", rendered)); got != rendered {
			t.Errorf("%s：英文回执被读侧判成伪造：got %q want %q", tpl.key, got, rendered)
		}
	}

	// 2) 批量改价的四个分支（写侧 bulkPricingResultMsg → 读侧 productPageErr / Done）。
	//
	// 跳过原因必须是**受控原因**（productReasonTexts 的取值之一）—— 读侧带跳过原因的两种形态
	// 是「模板 × 每一种受控原因」逐条组合出来的，随便造一个 "REASON" 当然对不上
	//（那正是这条链路的设计：原因也是白名单的一部分）。
	reason := productReasonTexts(en)[0]
	pricing := []struct {
		name                        string
		changed, unchanged, skipped int
		reason                      string
		want                        string
	}{
		{"没有价格变化", 0, 4, 0, "", "EN-PRICE-NOCHANGE 4"},
		{"已改价", 3, 4, 0, "", "EN-PRICE-APPLIED 3 4"},
		{"全部跳过", 0, 0, 2, reason, "EN-PRICE-SKIP 2 " + reason},
		{"部分改价", 3, 0, 2, reason, "EN-PRICE-PART 3 2 " + reason},
	}
	for _, tc := range pricing {
		msg := bulkPricingResultMsg(en, tc.changed, tc.unchanged, tc.skipped, tc.reason)
		if msg != tc.want {
			t.Errorf("批量改价「%s」应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
			continue
		}
		if got := productPageErr(productBulkLangCtx(t, "en-US", msg)); got != msg {
			t.Errorf("批量改价「%s」的英文回执被读侧判成伪造：got %q want %q", tc.name, got, msg)
		}
	}

	// 3) 变体清单保存：模板 + 跳过段的前缀 + 无变化分支都要按当前语言。
	saved := fmt.Sprintf(productBulkTextOf(en, productVariantSaveSaved), "1", "2", "3")
	if saved != "EN-VARIANT 1 2 3" {
		t.Errorf("变体清单保存应按当前语言拼装，实际 %q", saved)
	}
	if !productVariantNoticeMatches(en, saved) {
		t.Errorf("变体清单保存的英文回执被专用判定拒掉：%q", saved)
	}
	if got := productPageDone(productBulkLangCtx(t, "en-US", saved)); got != saved {
		t.Errorf("变体清单保存的英文回执被读侧判成伪造：got %q want %q", got, saved)
	}
	if noChange := productBulkTextOf(en, productVariantSaveNoChange); !productVariantNoticeMatches(en, noChange) {
		t.Errorf("「没有需要保存的变化」的英文回执被专用判定拒掉：%q", noChange)
	}

	// 4) 中文请求走中文词条。
	zh := productBulkLangCtx(t, "zh-CN", "")
	if text := productBulkTextOf(zh, productTagBulkDone); text != "已删除 %s 个标签" {
		t.Errorf("中文请求应取中文词条，实际 %q", text)
	}
}
