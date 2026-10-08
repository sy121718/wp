package producthttp

// product_err.go — 商品后台页**写侧**回执文案的收口。
//
// 传输通道已改为 shell.RenderJump 渲染整页提示（见 product_jump.go）：写动作的结论走
// 响应体，**不再**经 302/303 + `?err=` / `?done=` / `?applied=` / `?saved=` 回带列表页。
//
// **读侧（?err= / ?done= 的受控文案集合与判定）已整批删除**：那条通道的代价是本模块要维护
// 一份「受控文案 + 数字归一模板」来证明提示出自本仓（productPageErr / productPageDone /
// productNoticeTexts / productFacingNotice / productVariantNoticeMatches / productSkipListMatches /
// productReasonTexts / productOwnPageTexts / productAppliedToken / productBulkResultTemplates
// 就是那套），而查询参数不是可信边界（任何人手拼一个 /admin/products?err=任意文案 就能往
// 页面上塞一条伪造消息）。文案走响应体之后，那套判定随之不需要了。
//
// 本文件剩下的都是**写侧**：把 service 的错误 / 计数结论渲染成可展示的成品文案
//（productErrText 在 product_page.go，本文件放「结论文案模板」与它们的取词）。

import (
	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
)

// —— 批量删除的结论文案模板（写侧 Sprintf 用）——
//
// **key 与中文原文只有这一份**：写侧各页的 BulkDelete 拿它 Sprintf 出文案。
// 读侧已删除，不再需要「同一份值」的候选集合。
type productBulkText struct{ key, fallback string }

// productBulkTextOf 取一条批量结论文案的当前语言模板。
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

// —— 列表页 / 详情页的自造回执文案 ——

// productQuantityInvalidText 新建商品时数量字段的校验文案（写侧取词）。
//
// key + 中文兜底：写侧取当前语言；词条缺失时页面上仍是中文。
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

// 变体清单保存的结论文案（详情页提示页）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// productSaved 商品编辑页保存成功的回执（无占位符）。
//
// 保存后页面刷新，字段值与保存前看起来一模一样 —— 没有这条提示，用户无法区分
// 「保存成功了」与「按钮点了没反应」。
var productSaved = productBulkText{productenums.MsgProductSaved, "商品已保存"}
