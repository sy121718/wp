package producthttp

// product_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归（写侧）。
//
// 为什么必须有这一条：写侧各页 BulkDelete / bulkPricingResultMsg / variantSaveNotice 都经
// productBulkTextOf 取当前语言模板再 Sprintf。只要有人把某一处重新写成中文常量（或忘了带 c），
// 中文环境下一切正常，英文页面上那条回执就**静默变回中文** —— 不报错、不记日志。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条，不建库、不起装配。
// 读侧判定（productNoticeTexts / productVariantNoticeMatches）已随「结论走 shell.RenderJump」
// 整批删除，本文件不再涉及它们。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
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

// productBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
func productBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestProductBulkNoticeFollowsRequestLanguage 每个批量结论都随当前语言拼装。
func TestProductBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(productBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := productBulkLangCtx(t, "en-US")

	// 1) 十个批量删除模板：写侧取到的必须是英文模板。
	for _, tpl := range []productBulkText{
		productTagBulkPartial, productTagBulkDone,
		productAttrBulkPartial, productAttrBulkDone,
		productCategoryBulkPartial, productCategoryBulkDone,
		productBrandBulkPartial, productBrandBulkDone,
		productBulkPartial, productBulkDone,
	} {
		text := productBulkTextOf(en, tpl)
		if !strings.HasPrefix(text, "EN-") {
			t.Errorf("%s 没有取到英文词条，实际 %q", tpl.key, text)
		}
	}

	// 2) 批量改价的四个分支（写侧 bulkPricingResultMsg）。
	reason := "EN-REASON"
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
		}
	}

	// 3) 变体清单保存：模板 + 跳过段前缀都要按当前语言。
	saved := variantSaveNotice(en, &productdto.SaveVariantListResp{Created: 1, Updated: 2, Deleted: 3})
	if saved != "EN-VARIANT 1 2 3" {
		t.Errorf("变体清单保存应按当前语言拼装，实际 %q", saved)
	}
	withSkip := variantSaveNotice(en, &productdto.SaveVariantListResp{
		Created: 1,
		Skipped: []productdto.VariantSaveSkip{{Reason: productenums.VariantSkipHasStock}},
	})
	if !strings.HasPrefix(withSkip, "EN-VARIANT 1 0 0 EN-VARIANT-SKIP 1 ") {
		t.Errorf("变体清单保存（含跳过）应按当前语言拼装，实际 %q", withSkip)
	}
	noChange := variantSaveNotice(en, &productdto.SaveVariantListResp{})
	if noChange != "EN-VARIANT-NOCHANGE" {
		t.Errorf("「没有需要保存的变化」应按当前语言，实际 %q", noChange)
	}

	// 4) 批量删除结论（productBulkDeleteResult）。
	if _, msg := productBulkDeleteResult(en, 3, 0, productTagBulkPartial, productTagBulkDone); msg != "EN-TAG 3" {
		t.Errorf("批量删除结论应按当前语言，实际 %q", msg)
	}
	if _, msg := productBulkDeleteResult(en, 3, 2, productTagBulkPartial, productTagBulkDone); msg != "EN-TAG-PART 3 2" {
		t.Errorf("批量删除部分成功结论应按当前语言，实际 %q", msg)
	}

	// 5) 中文请求走中文词条。
	zh := productBulkLangCtx(t, "zh-CN")
	if text := productBulkTextOf(zh, productTagBulkDone); text != "已删除 %s 个标签" {
		t.Errorf("中文请求应取中文词条，实际 %q", text)
	}
}
