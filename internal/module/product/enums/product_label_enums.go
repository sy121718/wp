package productenums

// product_label_enums.go — 商品域「业务取值 → 文案事实」的映射（internal/module/CLAUDE.md §enums）。
//
// 为什么这些 switch 住在这里而不是控制器：值的域属于本模块的业务（商品状态、标签类型、
// 捆绑成员来源、实体类型、评分页型），它的展示文案就是本模块 enums 的事。放在控制器里的代价
// 不是「不好看」，而是加一门语言要回头改控制器，且同一个值在多处各抄一份映射 ——
// 先例：`admin.products.status.*` 三档在列表页是一张手抄的 (值, key, 兜底) 三元表，
// 而模板里的状态徽章另写一遍同一批 key。
//
// 形状统一为 `XxxLabel(value) (key, fallback string)`：i18n key + 中文兜底成对返回，
// 控制器只做一次调用与赋值。**未知值的取舍写在各自注释里** —— 不返回空 key + 空兜底那种
// 「没有这条文案」的伪信号（调用点会把空当成「不用提示」，未知值就被静默吞掉了）。
//
// key 常量在 product_text_keys.go（点分 key 组），中文兜底跟着映射走（就在这里）：
// 「值 → (key, 兜底)」是一条完整的事实，拆到两个文件里改一半会静默回落中文。

import "strconv"

// —— 商品状态（products.status）——
//
// 三档显示名与筛选下拉的「全部状态」都只出现在模板里（products.html / product_edit.html
// 按取值分支取词）：后端只给取值与选中态，不再有「取值 → 文案」的映射表。

// —— 标签类型（tags.kind）——
//
// 类型的显示名与「未重算过」这句话都只出现在模板里（product_tags.html 按 Kind 分支取词）：
// 后端不再有「取值 → 文案」的映射表，也就不会出现第二份措辞。

// —— 捆绑成员的来源（products.bundle_items.options[].sourceKind）——
//
// 三种来源都走同一条出口（ResolveBundleMembers），空值 = 迁移 238 之前的历史配置 /
// 手工指定。取值常量见 product_enums.go 的 BundleSource*。显示名一律在渲染期按取值取词：
// 模板（product_bundle.html / product_bundle_form.html）与 bundle_members.js 各按同一份
// 取值分支走，后端不再有「取值 → 文案」的映射表。

// —— 商品域实体类型（译文工作台的分组标题）——
//
// 取值即 productcontract.EntityType*（实体类型注册表与内容模板 entityType 的取值）。
// 本包零依赖，不 import contract（contract → enums 已有依赖，反向即环）：这里放一份**镜像**，
// 由 product_label_enums_test.go 的 TestEntityTypeMirrorsContract 对账 —— contract 改了取值
// 而这里没跟上时那条测试会变红（否则下面这个 switch 会静默走到 default，五类实体全叫「商品」）。
const (
	EntityTypeProduct   = "product"
	EntityTypeCategory  = "product_category"
	EntityTypeBrand     = "product_brand"
	EntityTypeTag       = "product_tag"
	EntityTypeAttribute = "product_attribute"
)

// EntityTypeLabel 实体类型 → 文案事实（后台翻译页的分组标题）。
//
// 未知类型回落到「商品」：product 是实体类型注册表里唯一的总称（其余四类都是它的子域），
// 认不出时按最宽的那一类说，比空着或说成别的子域错得更轻。key 见迁移 449。
func EntityTypeLabel(entityType string) (key, fallback string) {
	switch entityType {
	case EntityTypeCategory:
		return ProductTranslationsEntityTypeCategory, "商品分类"
	case EntityTypeBrand:
		return ProductTranslationsEntityTypeBrand, "商品品牌"
	case EntityTypeTag:
		return ProductTranslationsEntityTypeTag, "商品标签"
	case EntityTypeAttribute:
		return ProductTranslationsEntityTypeAttribute, "商品属性"
	default:
		return ProductTranslationsEntityTypeProduct, "商品"
	}
}

// —— 评分端点的页型（SEO 评分三个 POST 端点的 kind）——
//
// 取值即 internal/seo/scoring 的 PageKind（三个端点各自传入的「用哪套尺子」）。
// 本包零依赖，不 import scoring：这里放一份**镜像**，由
// product_label_enums_test.go 的 TestScoreKindMirrorsScoring 对账。
const (
	ScoreKindProduct  = "product"
	ScoreKindCategory = "category"
	ScoreKindBrand    = "brand"
)

// ScoreMissingEntityLabel 缺实体 id 时的空态文案事实。
//
// 未知页型回落到「缺少商品」：商品是三个端点里唯一的总称（行内按钮就长在商品详情页上），
// 说「缺少商品」比把分类页的缺参说成「缺少分类」错得更轻。
func ScoreMissingEntityLabel(kind string) (key, fallback string) {
	switch kind {
	case ScoreKindCategory:
		return SEOEmptyMissingCategory, "缺少分类，无法评分"
	case ScoreKindBrand:
		return SEOEmptyMissingBrand, "缺少品牌，无法评分"
	default:
		return SEOEmptyMissingProduct, "缺少商品，无法评分"
	}
}

// —— 按计数选整句（批量动作的结论）——
//
// 批量动作的结论不是「值 → 一句话」，而是「几个计数 → 哪一句 + 带哪几个参数」：候选句本身
// 是常量，要选的只有那一条。它同样是**挑文案**，所以住在这里（internal/module/CLAUDE.md
// §enums 的扩展形状），控制器只做一次转换。

// Outcome 一条结论的**事实**：i18n key + 中文兜底 + 已 strconv 的 `%s` 参数。
//
// 与 adminui.Msg 同形，控制器一次转换即可（productOutcomeMsg）；取词在提示页出口 / 模板。
type Outcome struct {
	// Key 词条 key（sys_i18n 的 item_key）。
	Key string
	// Fallback 中文兜底（词条缺失时原样输出，绝不报错）。
	Fallback string
	// Args `%s` 占位符实参（一律在这里 strconv：模板侧只认字符串，传 int 会渲染成 %!s(int=3)）。
	Args []string
}

// BulkDeleteOutcome 批量删除的结论（成功 N / 跳过 M）→ 提示页的 ok 与事实。
//
// partial / done 是各实体自己的两条结论文案（调用方只给句子，不给选择）；「哪一条 + 哪几个
// 参数」在这里定。一个都没删也没跳过时给 ProductsBulkNoneSelected —— 写成「已删除 0 个」
// 读起来像成功，实际什么都没做（与批量改价的「一个都没勾选」同一口径）。
func BulkDeleteOutcome(deleted, skipped int, partial, done Outcome) (ok bool, out Outcome) {
	switch {
	case skipped > 0:
		return false, Outcome{Key: partial.Key, Fallback: partial.Fallback,
			Args: []string{itoa(deleted), itoa(skipped)}}
	case deleted > 0:
		return true, Outcome{Key: done.Key, Fallback: done.Fallback, Args: []string{itoa(deleted)}}
	default:
		return false, Outcome{Key: ProductsBulkNoneSelected, Fallback: "没有选中任何项，列表未改动。"}
	}
}

// PricingBulkOutcome 批量改价的结论（四类结果各一条词条 + `%s` 参数）。
//
// 三个计数各自独立：改了几个变体、本来就在目标价上的商品数、整批跳过的商品数 ——
// 「一个都没改」与「跳过若干」必须能区分，两者都写成「没有变化」时用户无法判断是规则填错了、
// 商品本来就在目标价上，还是这批商品根本没有变体。
//
// reason 是**已取词的成品串**（逐商品的跳过原因）：它是数据不是结论，由调用方先取好。
func PricingBulkOutcome(changedVariants, unchangedProducts, skipped int, reason string) Outcome {
	switch {
	case skipped == 0 && changedVariants == 0:
		return Outcome{Key: BulkPricingNoChange,
			Fallback: "按该规则算下来没有价格变化：%s 个商品已是目标价，未写入调价记录。",
			Args:     []string{itoa(unchangedProducts)}}
	case skipped == 0:
		return Outcome{Key: BulkPricingApplied,
			Fallback: "已按规则改价：共改 %s 个变体（%s 个商品已是目标价）。",
			Args:     []string{itoa(changedVariants), itoa(unchangedProducts)}}
	case changedVariants == 0:
		return Outcome{Key: BulkPricingAllSkip,
			Fallback: "没有可改价的变体：%s 个商品被跳过（%s）。",
			Args:     []string{itoa(skipped), reason}}
	default:
		return Outcome{Key: BulkPricingPartial,
			Fallback: "已改 %s 个变体，另有 %s 个商品被跳过（%s）。",
			Args:     []string{itoa(changedVariants), itoa(skipped), reason}}
	}
}

// itoa 计数一律在 enums 里转成字符串（词条只允许 `%s`）。
func itoa(n int) string { return strconv.Itoa(n) }
