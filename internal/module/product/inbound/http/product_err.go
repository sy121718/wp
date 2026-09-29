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

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
)

// —— 批量删除的结论文案模板（写侧与读侧共用这一份字面量）——
//
// 写侧各页的 BulkDelete 用它 Sprintf 出文案，读侧 productNoticeTexts 用它
// （经 shell.NoticeTemplate 归一）判定 URL 回显。各写一份的后果是静默的：
// 写侧改了措辞，读侧白名单不再命中，运营看到的就从「已删除 3 个标签」退化成归口文案。
// productBulkText 批量结论文案的一条模板（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧各页的 BulkDelete / 批量改价 / 变体清单保存拿它 Sprintf
// 出文案，读侧 productNoticeTexts 拿**同一个值**、经同一处取词（productBulkTextOf）得到
// 当前语言模板再归一比对。读侧另抄一份中文的后果是静默的 —— 写侧改了措辞候选就失配，
// 运营看到的就从「已删除 3 个标签」退化成归口文案。
type productBulkText struct{ key, fallback string }

// productBulkTextOf 取一条批量结论文案的当前语言模板（写侧与读侧**共用这一个取法**）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经 strconv.Itoa）——
// 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看（与 shell.BulkIDsFacingText 同理）。
func productBulkTextOf(c *gin.Context, t productBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 批量删除：五个实体各一对（部分成功 / 全部成功）。%s 是计数（Go 侧 strconv.Itoa 后填入）。
var (
	productTagBulkPartial      = productBulkText{productenums.BulkTagPartial, "已删除 %s 个，%s 个未能删除（标签不存在或已被删除）"}
	productTagBulkDone         = productBulkText{productenums.BulkTagDone, "已删除 %s 个标签"}
	productAttrBulkPartial     = productBulkText{productenums.BulkAttrPartial, "已删除 %s 个，%s 个未能删除（属性组不存在或被商品引用）"}
	productAttrBulkDone        = productBulkText{productenums.BulkAttrDone, "已删除 %s 个属性组（连同其全部属性值）"}
	productCategoryBulkPartial = productBulkText{productenums.BulkCategoryPartial, "已删除 %s 个，%s 个未能删除（有子分类或被商品引用）"}
	productCategoryBulkDone    = productBulkText{productenums.BulkCategoryDone, "已删除 %s 个分类"}
	productBrandBulkPartial    = productBulkText{productenums.BulkBrandPartial, "已删除 %s 个，%s 个未能删除（品牌不存在或被商品引用）"}
	productBrandBulkDone       = productBulkText{productenums.BulkBrandDone, "已删除 %s 个品牌"}
	productBulkPartial         = productBulkText{productenums.BulkProductPartial, "已删除 %s 个，%s 个未能删除（商品不存在或被其它数据引用）"}
	productBulkDone            = productBulkText{productenums.BulkProductDone, "已删除 %s 个商品（连同其全部变体）"}
)

// productBulkResultTemplates 上面那组模板的集合（读侧候选直接由它派生）。
var productBulkResultTemplates = []productBulkText{
	productTagBulkPartial, productTagBulkDone,
	productAttrBulkPartial, productAttrBulkDone,
	productCategoryBulkPartial, productCategoryBulkDone,
	productBrandBulkPartial, productBrandBulkDone,
	productBulkPartial, productBulkDone,
}

// —— 列表页 / 详情页的自造回执文案 ——

// productQuantityInvalidText 新建商品时数量字段的校验文案（写侧取词，读侧登记）。
//
// key + 中文兜底：写侧与读侧各持有同一份 key 与兜底（读侧还要多收一份译文，
// 否则英文站点上写侧产出英文、读侧候选里只有中文，整条提示被归口文案顶掉）。
const (
	productQuantityInvalidKey      = "admin.products.create.quantityInvalid"
	productQuantityInvalidFallback = "数量必须是非负整数"
)

// 批量改价的结论文案模板（%s 是计数或跳过原因）。
var (
	productPricingNoChange = productBulkText{productenums.BulkPricingNoChange, "按该规则算下来没有价格变化：%s 个商品已是目标价，未写入调价记录。"}
	productPricingApplied  = productBulkText{productenums.BulkPricingApplied, "已按规则改价：共改 %s 个变体（%s 个商品已是目标价）。"}
	productPricingAllSkip  = productBulkText{productenums.BulkPricingAllSkip, "没有可改价的变体：%s 个商品被跳过（%s）。"}
	productPricingPartial  = productBulkText{productenums.BulkPricingPartial, "已改 %s 个变体，另有 %s 个商品被跳过（%s）。"}
)

// 变体清单保存的结论文案（详情页 ?done=）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// productSaved 商品编辑页保存成功的回执（无占位符，编辑页 ?done=）。
//
// 保存后页面刷新，字段值与保存前看起来一模一样 —— 没有这条提示，用户无法区分
// 「保存成功了」与「按钮点了没反应」。
var productSaved = productBulkText{productenums.MsgProductSaved, "商品已保存"}

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
//
// c 是给「依赖未装配」那条 i18n 文案用的：它经 detailTemplateDepsMissingText 取词，
// 于是同一句话有三种形态要收（key / 中文兜底 / 当前语言译文）—— 只登记中文原文的话，
// 英文站点（写侧取到英文）与 i18n 尚未初始化的环境会各自漏掉一边。
func productOwnPageTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(detailTemplateFacingMessages)+len(detailTemplateTemplateMessages)+24)
	// 下面这几组是「写侧已经取词」的提示：key / 中文兜底 / 当前语言译文三种形态都要进候选。
	// 只登记中文原文的话，英文站点上写侧产出的是英文译文，读侧一条都匹配不上，
	// 表现是「写侧发了提示、页面上什么都不显示」——不报错、不记日志。
	out = append(out,
		detailTemplateDepsMissingKey,
		errTemplateDepsMissing,
		tr(detailTemplateDepsMissingKey, errTemplateDepsMissing),
		productDetailTemplateNameRequiredKey,
		productDetailTemplateNameRequiredFallback,
		tr(productDetailTemplateNameRequiredKey, productDetailTemplateNameRequiredFallback),
		productDetailTemplatePathRequiredKey,
		productDetailTemplatePathRequiredFallback,
		tr(productDetailTemplatePathRequiredKey, productDetailTemplatePathRequiredFallback),
		// 菜单入口缺 product 时的引导（详情页模板页的缺参分支，同样进 ?err=）。
		productDetailTemplateNoProductPromptKey,
		productDetailTemplateNoProductPromptFallback,
		tr(productDetailTemplateNoProductPromptKey, productDetailTemplateNoProductPromptFallback),
		productBundleNoProductKey,
		productBundleNoProductFallback,
		tr(productBundleNoProductKey, productBundleNoProductFallback),
	)
	// 两张白名单同样要收三种形态：facingLookup 现在按请求语言取词（裸 enums key 就是
	// i18n key），写侧产出的是**译文**——只登记中文兜底会让整条提示被归口文案顶掉。
	for key, fallback := range detailTemplateFacingMessages {
		out = append(out, key, fallback, tr(key, fallback))
	}
	for key, fallback := range detailTemplateTemplateMessages {
		out = append(out, key, fallback, tr(key, fallback))
	}
	return out
}

// productNoticeTexts 商品后台页可以原样展示的回执文案（当前语言）。
func productNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	reasons := productReasonTexts(c)
	own := productOwnPageTexts(c)
	out := make([]string, 0, len(reasons)*5+len(productBulkResultTemplates)+len(own)+8)
	out = append(out, reasons...)
	out = append(out,
		tr(shell.MsgInternalError, productErrInternalFallback),
		shell.BulkIDsNoticeTemplate(c),
		productQuantityInvalidKey,
		productQuantityInvalidFallback,
		tr(productQuantityInvalidKey, productQuantityInvalidFallback),
		productBulkTextOf(c, bulkPricingNothingSelected),
		productBulkTextOf(c, productVariantSaveNoChange),
		// 商品编辑页保存成功（无占位符，直接取词即可）。
		productBulkTextOf(c, productSaved),
	)
	out = append(out, own...)
	// 纯计数模板直接过 NoticeTemplate（它把 %s 换成占位并归一数字）。
	// 不能写成 Sprintf(tpl, 0, 0)：单占位符的模板会多出 %!(EXTRA int=0)，
	// 候选与写侧文案从此**永远**不相等 —— 五条「已删除 N 个XX」的批量回执
	// 会静默变成「没有这条提示」。
	for _, tpl := range productBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(productBulkTextOf(c, tpl)))
	}
	out = append(out,
		shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productVariantSaveSaved), "0", "0", "0")),
		shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productPricingNoChange), "0")),
		shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productPricingApplied), "0", "0")),
	)
	// 带跳过原因的两种形态：原因取值的每一种都与模板组合一次（reason 本身是受控文案）。
	for _, reason := range reasons {
		out = append(out,
			shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productPricingAllSkip), "0", reason)),
			shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productPricingPartial), "0", "0", reason)),
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
	if msg == productBulkTextOf(c, productVariantSaveNoChange) {
		return true
	}
	saved := shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productVariantSaveSaved), "0", "0", "0"))
	if shell.NormalizeNoticeDigits(msg) == saved {
		return true
	}
	reasons := productReasonTexts(c)
	// 写侧用**单个空格**把「已保存…」与「跳过…」两段 join 起来，而两段各自都可能含空格 ——
	// 英文模板就是如此（"Variant list saved: %s added, %s SKU updated, %s deleted."）。
	// 此前按**第一个**空格切（strings.Cut）是拿中文模板当隐含前提：中文句子里没有空格，
	// 于是它一直是对的；一旦按当前语言取到英文，第一个空格落在句首那段里，
	// 「已保存变体清单」这条回执就会被判成伪造而静默消失。
	// 现在逐个空格位置试切：前缀与模板归一后相等、后缀是合法的跳过列表即命中。
	for i, r := range msg {
		if r != ' ' {
			continue
		}
		if shell.NormalizeNoticeDigits(msg[:i]) != saved {
			continue
		}
		if productSkipListMatches(c, msg[i+1:], reasons) {
			return true
		}
	}
	return false
}

// productSkipListMatches 判定「跳过 N 个：原因1；原因2」形态。
//
// 前缀模板同样走 productBulkTextOf（写侧 variantSaveNotice 用的是同一个取法），
// 否则英文页面上这条逐段校验会整体失配、把真实的保存回执判成伪造。
func productSkipListMatches(c *gin.Context, tail string, reasons []string) bool {
	prefix := shell.NoticeTemplate(fmt.Sprintf(productBulkTextOf(c, productVariantSaveSkipped), "0", ""))
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
