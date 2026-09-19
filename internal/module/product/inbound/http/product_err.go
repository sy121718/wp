package producthttp

// product_err.go — 商品后台页**读侧**回执文案（?err= / ?done=）的收口。
//
// 写侧早已是受控的（productErrText / productPageHandle 的各批量入口），但商品域有
// 六七个页面，读侧此前一律把 query 参数**原样**塞进渲染数据
//（`"Err": strings.TrimSpace(c.Query("err"))`）：任何人手拼一个
// /admin/products?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」样式的伪造消息。
// 查询参数与响应体、模板数据一样**不是可信边界**。
//
// 判定分两层：
//
//  1. 通用形态 —— shell.FacingNotice：与候选文案逐字相等 / 数字归一后相等 /
//     以「候选文案 + ：」开头。候选 = 本模块全部业务错误文案（productErrFallbacks 的
//     key、中文兜底与当前语言译文）+ 各页自造的结论文案模板 + shell 的批量上限提示；
//  2. 变体清单保存的**专用形态** —— 它的回执是「前半句 + 跳过 N 个：原因1；原因2」，
//     分号列表无法用单条候选覆盖，因此逐段校验每个原因都必须是受控文案
//     （productVariantNoticeMatches）。
//
// 落 fallback 的规矩与其它模块一致：?err= 未命中落 shell.PageInternalText(c)，
// ?done= 未命中落空串。

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// —— 批量删除的结论文案模板（写侧与读侧共用这一份字面量）——
//
// 写侧各页的 BulkDelete 用它 Sprintf 出文案，读侧 productNoticeTexts 用它
// （经 shell.NoticeTemplate 归一）判定 URL 回显。各写一份的后果是静默的：
// 写侧改了措辞，读侧白名单不再命中，运营看到的就从「已删除 3 个标签」退化成归口文案。
const (
	productTagBulkPartial      = "已删除 %d 个，%d 个未能删除（标签不存在或已被删除）"
	productTagBulkDone         = "已删除 %d 个标签"
	productAttrBulkPartial     = "已删除 %d 个，%d 个未能删除（属性组不存在或被商品引用）"
	productAttrBulkDone        = "已删除 %d 个属性组（连同其全部属性值）"
	productCategoryBulkPartial = "已删除 %d 个，%d 个未能删除（有子分类或被商品引用）"
	productCategoryBulkDone    = "已删除 %d 个分类"
	productBrandBulkPartial    = "已删除 %d 个，%d 个未能删除（品牌不存在或被商品引用）"
	productBrandBulkDone       = "已删除 %d 个品牌"
	productBulkPartial         = "已删除 %d 个，%d 个未能删除（商品不存在或被其它数据引用）"
	productBulkDone            = "已删除 %d 个商品（连同其全部变体）"
)

// productBulkResultTemplates 上面那组模板的集合（读侧候选直接由它派生）。
var productBulkResultTemplates = []string{
	productTagBulkPartial, productTagBulkDone,
	productAttrBulkPartial, productAttrBulkDone,
	productCategoryBulkPartial, productCategoryBulkDone,
	productBrandBulkPartial, productBrandBulkDone,
	productBulkPartial, productBulkDone,
}

// —— 列表页 / 详情页的自造回执文案 ——

// productQuantityInvalidText 新建商品时数量字段的校验文案（写侧硬编码，读侧登记）。
const productQuantityInvalidText = "数量必须是非负整数"

// 批量改价的结论文案模板（%d 是计数，%s 是跳过原因）。
const (
	productPricingNoChange = "按该规则算下来没有价格变化：%d 个商品已是目标价，未写入调价记录。"
	productPricingApplied  = "已按规则改价：共改 %d 个变体（%d 个商品已是目标价）。"
	productPricingAllSkip  = "没有可改价的变体：%d 个商品被跳过（%s）。"
	productPricingPartial  = "已改 %d 个变体，另有 %d 个商品被跳过（%s）。"
)

// 变体清单保存的结论文案（详情页 ?done=）。
const (
	productVariantSaveNoChange = "变体清单与库里一致，没有需要保存的变化。"
	productVariantSaveSaved    = "已保存变体清单：新增 %d 个、修改 SKU %d 个、删除 %d 个。"
	productVariantSaveSkipped  = "跳过 %d 个：%s"
)

// productReasonTexts 可原样展示的**业务错误文案**（当前语言）。
//
// 三类形态都被接受：enums 的 key（未接 i18n 时的取值）、productErrFallbacks 的中文兜底
// （service 在 reason 位置直接引用它）、以及当前语言译文（productErrText 的正常产物）。
func productReasonTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(productErrFallbacks)*3+len(productErrControlledMessages))
	// 键集取 productErrSentinels（productErrKey 的判据）：写侧只会产出这些 key 的
	// 译文（productErrText 用 tr(key, productErrFallbacks[key])）与裸 key。
	for _, key := range productErrSentinels {
		fallback := productErrFallbacks[key]
		out = append(out, key, fallback, tr(key, fallback), tr(key, key))
	}
	for _, tpl := range productErrControlledMessages {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	return out
}

// productOwnPageTexts 商品域里**不走 productErrText** 的那几条读侧回执文案。
//
// 详情页模板页与捆绑页各有自己拼的提示：前者把错误翻过一次
// （detailTemplateFacingError / detailTemplateTemplateErrText 的两张白名单），
// 后者有参数级提示。它们都进 ?err=，因此必须与 productErrText 的产物一起登记 ——
// 漏登记的后果不是「提示不准」，而是这条业务提示被归口文案整体顶掉
// （运营看到「系统内部错误」，而实际原因只是「模板名不能为空」）。
func productOwnPageTexts() []string {
	out := make([]string, 0, len(detailTemplateFacingMessages)+len(detailTemplateTemplateMessages)+4)
	out = append(out,
		errTemplateDepsMissing,
		productDetailTemplateNameRequired,
		productDetailTemplatePathRequired,
		productBundleNoProductText,
	)
	for _, msg := range detailTemplateFacingMessages {
		out = append(out, msg)
	}
	for _, msg := range detailTemplateTemplateMessages {
		out = append(out, msg)
	}
	return out
}

// productNoticeTexts 商品后台页可以原样展示的回执文案（当前语言）。
func productNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	reasons := productReasonTexts(c)
	own := productOwnPageTexts()
	out := make([]string, 0, len(reasons)*5+len(productBulkResultTemplates)+len(own)+8)
	out = append(out, reasons...)
	out = append(out,
		tr(shell.MsgInternalError, productErrInternalFallback),
		shell.BulkIDsNoticeTemplate(c),
		productQuantityInvalidText,
		bulkPricingNothingSelected,
		productVariantSaveNoChange,
	)
	out = append(out, own...)
	// 模板直接过 NoticeTemplate（它已经把 %d 换成占位并归一数字）。
	// 不能写成 Sprintf(tpl, 0, 0)：单占位符的模板会多出 %!(EXTRA int=0)，
	// 候选与写侧文案从此**永远**不相等 —— 五条「已删除 N 个XX」的批量回执
	// 会静默变成「没有这条提示」。
	for _, tpl := range productBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	out = append(out, shell.NoticeTemplate(fmt.Sprintf(productVariantSaveSaved, 0, 0, 0)))
	out = append(out,
		shell.NoticeTemplate(fmt.Sprintf(productPricingNoChange, 0)),
		shell.NoticeTemplate(fmt.Sprintf(productPricingApplied, 0, 0)),
	)
	// 带跳过原因的两种形态：原因取值的每一种都与模板组合一次（reason 本身是受控文案）。
	for _, reason := range reasons {
		out = append(out,
			shell.NoticeTemplate(fmt.Sprintf(productPricingAllSkip, 0, reason)),
			shell.NoticeTemplate(fmt.Sprintf(productPricingPartial, 0, 0, reason)),
		)
	}
	return out
}

// productFacingNotice 商品页回执文案的判定：通用形态 + 变体清单的专用形态。
func productFacingNotice(c *gin.Context, raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" || len(msg) > shell.NoticeMaxBytes {
		return ""
	}
	if found := shell.FacingNotice(msg, productNoticeTexts(c)); found != "" {
		return found
	}
	if productVariantNoticeMatches(c, msg) {
		return msg
	}
	return ""
}

// productVariantNoticeMatches 变体清单保存回执的专用判定。
//
// 写侧形态（variantSaveNotice）：
//
//	「已保存变体清单：新增 N 个、修改 SKU M 个、删除 K 个。」[「 」+「跳过 S 个：原因1；原因2」]
//
// 前半句按模板归一比对；跳过段按**分号逐段**校验，每段都必须是 productReasonTexts 里的
// 受控原因文案 —— 也就是说这一句里除了计数与原因枚举，不可能夹带别的文字。
func productVariantNoticeMatches(c *gin.Context, msg string) bool {
	if msg == productVariantSaveNoChange {
		return true
	}
	head, tail, hasTail := strings.Cut(msg, " ")
	saved := shell.NoticeTemplate(fmt.Sprintf(productVariantSaveSaved, 0, 0, 0))
	if shell.NormalizeNoticeDigits(head) != saved {
		return false
	}
	if !hasTail {
		return true
	}
	return productSkipListMatches(tail, productReasonTexts(c))
}

// productSkipListMatches 判定「跳过 N 个：原因1；原因2」形态。
func productSkipListMatches(tail string, reasons []string) bool {
	prefix := shell.NoticeTemplate(fmt.Sprintf(productVariantSaveSkipped, 0, ""))
	norm := shell.NormalizeNoticeDigits(tail)
	if !strings.HasPrefix(norm, prefix) {
		return false
	}
	items := strings.Split(strings.TrimPrefix(norm, prefix), "；")
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		matched := false
		for _, reason := range reasons {
			if strings.TrimSpace(item) == shell.NormalizeNoticeDigits(strings.TrimSpace(reason)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// productPageErr 商品各页 ?err= 的统一出口（未命中落归口文案）。
func productPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return productFacingNotice(c, raw)
	})
}

// productPageDone 商品各页 ?done= 的统一出口（成功提示：未命中落空串）。
func productPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return productFacingNotice(c, raw)
	})
}

// productAppliedToken 定价页 ?applied= 的收口：它只承载「本次改了几个变体」的计数，
// 因此只放行纯数字（最长 12 位），其余一律回空串。
func productAppliedToken(c *gin.Context) string {
	raw := strings.TrimSpace(c.Query("applied"))
	if raw == "" || len(raw) > 12 {
		return ""
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return raw
}
