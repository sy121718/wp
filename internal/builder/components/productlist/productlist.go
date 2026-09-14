// Package productlist 实现 core.productList 商品列表组件（issue #23）。
//
// 定位：把一个商品的集合**渲染成网格或列表**。它只做三件事 ——
//
//  1. 取数：按 props 声明的筛选维度从集合源（默认 content:product）取一批商品；
//  2. 排序：按 props 声明的时间序重排（默认保持集合源的确定性序）；
//  3. 铺开：把每项映射成一张卡（字段槽位与 core.productCard 同口径）。
//
// 三条刻意的边界：
//
//	· **筛选是构建期下推的**：props 里的 status / categoryId / brandId / tagId 交给集合源的
//	  过滤维度机制（issue #21），组件自己不遍历数据 —— 越过白名单的维度由解析器拒绝；
//	· **不分页**：集合源单次上限 100，本组件用 collectionLimit 截断。真正的翻页需要
//	  「数据驱动页面」（一页一个 URL 产物）的能力，不塞进组件里做半套；
//	· **字段渲染口径与 core.productCard 一致**：图片取首元素、标签解析、链接前缀拼接都
//	  直接复用 productcard 的导出函数，避免两套口径各自漂移。
//
// 为什么不在组件里复刻集合维度常量：builder 是底层包，商品域依赖它、它不依赖商品域
// （依赖方向）；因此维度键（"status"/"categoryId"/"brandId"/"tagId"）与集合源标识
// （"content:product"）在这里是字面量 —— 契约改键名时这里要同步（cardstack 同样是既有做法）。
package productlist

import (
	_ "embed" // productlist.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.productList"

// 集合源标识（与 product contract 的 CollectionSourceProduct 一致）。
const collectionSourceProduct = "content:product"

// 集合过滤维度的键（与集合源契约的维度白名单一致，issue #21）。
const (
	filterKeyStatus     = "status"
	filterKeyCategoryID = "categoryId"
	filterKeyBrandID    = "brandId"
	filterKeyTagID      = "tagId"
	filterKeyTagIDs     = "tagIds"
	filterKeyTagMode    = "tagMode"
	filterKeyOnSale     = "onSale"
	filterKeyMinRating  = "minRating"
	filterKeyMinPrice   = "minPrice"
	filterKeyMaxPrice   = "maxPrice"

	// optionFilterPrefix 属性值维度的前缀（与集合源契约的 `option.<属性key>` 一致，issue #25）。
	optionFilterPrefix = "option."
)

// 筛选栏维度与工具条项的白名单（写错即报错，不静默忽略）。
var filterBlockNames = map[string]bool{
	"categories": true, "brands": true, "tags": true, "attributes": true,
}

// toolbarItemNames 工具条项白名单（onSale = 参考站那个「只看在售」开关）。
var toolbarItemNames = map[string]bool{"sort": true, "pageSize": true, "columns": true, "onSale": true}

// PriceRange 一个预设价格档位（下限为 0 表示「不限下限」，上限为 nil 表示「不限上限」）。
type PriceRange struct {
	Min float64
	Max *float64
}

// ParsePriceRange 解析一个档位字面量：`200-399`（闭区间）/ `799+`（该值以上）。
//
// 负数与非法形状一律报错：价格档位写错会让整块筛选消失或筛出莫名其妙的结果，
// 而这类错误在页面上很难看出来（少一块筛选栏没人会注意）。
func ParsePriceRange(raw string) (out PriceRange, label string, err error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return out, "", fmt.Errorf("价格档位为空")
	}
	if strings.HasSuffix(token, "+") {
		min, perr := strconv.ParseFloat(strings.TrimSuffix(token, "+"), 64)
		if perr != nil || min < 0 {
			return out, "", fmt.Errorf("价格档位 %q 形状非法（正数+ 表示该值以上）", raw)
		}
		return PriceRange{Min: min}, raw, nil
	}
	low, high, ok := strings.Cut(token, "-")
	if !ok {
		return out, "", fmt.Errorf("价格档位 %q 形状非法（期望 下限-上限 或 下限+）", raw)
	}
	min, merr := strconv.ParseFloat(strings.TrimSpace(low), 64)
	max, xerr := strconv.ParseFloat(strings.TrimSpace(high), 64)
	if merr != nil || xerr != nil || min < 0 || max < 0 || min > max {
		return out, "", fmt.Errorf("价格档位 %q 形状非法（下限与上限必须是非负数且下限不大于上限）", raw)
	}
	return PriceRange{Min: min, Max: &max}, token, nil
}

// EffectivePriceBounds 滑块边界（默认 0~1000；非法即报错）。
//
// 边界是**作者配置的展示范围**，不是数据范围：滑块的可拖区间必须是个确定的数，
// 不能每次渲染去问数据库「最贵的商品多少钱」—— 那样同一个页面在不同时刻会长得不一样。
func EffectivePriceBounds(p *Props) (min, max float64, err error) {
	min, max = 0, 1000
	if p == nil || strings.TrimSpace(p.PriceBounds) == "" {
		return min, max, nil
	}
	low, high, ok := strings.Cut(p.PriceBounds, ",")
	if !ok {
		return 0, 0, fmt.Errorf("滑块边界 %q 形状非法（期望 最小值,最大值）", p.PriceBounds)
	}
	parsedMin, merr := strconv.ParseFloat(strings.TrimSpace(low), 64)
	parsedMax, xerr := strconv.ParseFloat(strings.TrimSpace(high), 64)
	if merr != nil || xerr != nil || parsedMin < 0 || parsedMax <= parsedMin {
		return 0, 0, fmt.Errorf("滑块边界 %q 非法（最小值需为非负且小于最大值）", p.PriceBounds)
	}
	return parsedMin, parsedMax, nil
}

// PriceSliderEnabled 是否渲染价格滑块（空 = 开启）。
func PriceSliderEnabled(p *Props) bool {
	return p == nil || p.PriceSlider != "off"
}

// splitList 逗号分隔 → 去空去重列表（顺序保持首次出现）。
func splitList(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		v := strings.TrimSpace(part)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// optionKeyRe 属性 key / 值的形状（与商品域属性组 key 的字符集一致）。
//
// 组件侧只做形状校验（挡住空键、超长、带空格这类明显写错的配置）；
// 属性到底存不存在由集合源解析器与 SQL 决定 —— 组件不持有商品域的数据。
var optionKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// 布局与排序取值。
const (
	LayoutGrid = "grid"
	LayoutList = "list"

	OrderDefault = "default"
	OrderNewest  = "newest"
	OrderOldest  = "oldest"
	// 价格排序（issue #28）：按最低启用变体价升 / 降；键名与集合源白名单一致。
	// 评分排序（issue #29）：按评分降序；无评分的排最后。
	OrderRatingDesc = "ratingDesc"
	OrderPriceAsc   = "priceAsc"
	OrderPriceDesc  = "priceDesc"

	ColumnsAuto = "auto"
)

// 缺省值。
const (
	defaultLimit     = 12
	maxLimit         = 100
	defaultCurrency  = "¥"
	defaultTitleTag  = "h3"
	defaultEmptyText = "暂无商品"
)

// fieldPathRe 字段槽位形状（与 core.productCard 同口径：item./product. + 驼峰字段名）。
var fieldPathRe = regexp.MustCompile(`^(item|product)\.[a-z][a-zA-Z0-9_]*$`)

// Props core.productList 属性。
type Props struct {
	// —— 取数 ——
	// CollectionSource 集合源（当前只开放商品集合源；留空按商品处理）。
	// 空值 = 未选择数据源：此时组件渲染空态而**不查库**（工作台刚插入、还没挑源时就是这个状态）。
	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=未选择,content:product=商品列表,default=content:product,sec=collection,label=数据源"`
	// CollectionLimit 取前几条（1~100，缺省 12）。
	CollectionLimit int `json:"collectionLimit,omitempty" ct:"slider,min=1,max=100,step=1,sec=collection,label=取几条"`

	// —— 筛选（构建期下推到集合源，issue #21 的四个维度）——
	FilterStatus     string `json:"filterStatus,omitempty" ct:"select,=全部,draft=草稿,published=已发布,archived=已归档,default=,sec=collection,label=状态"`
	FilterCategoryID string `json:"filterCategoryId,omitempty" ct:"entityref,category,label=分类"`
	FilterBrandID    string `json:"filterBrandId,omitempty" ct:"entityref,brand,label=品牌"`
	FilterTagID      string `json:"filterTagId,omitempty" ct:"entityref,tag,label=标签"`
	// FilterTagIDs 多标签筛选（issue #27）：逗号分隔的标签 id 列表（「热卖」「新品」这类用标签表达）。
	FilterTagIDs string `json:"filterTagIds,omitempty" ct:"text,maxlen=500,sec=collection,label=标签 id 列表"`
	// FilterTagMode 多标签语义：any（默认，具备任一）/ all（同时具备全部）。
	FilterTagMode string `json:"filterTagMode,omitempty" ct:"select,=具备任一,any=具备任一,all=同时具备全部,default=,sec=collection,label=多标签语义"`
	// OnlyOnSale 只看在售（存在启用变体有划线价且高于售价，与 on_sale 自动标签同源）。
	OnlyOnSale string `json:"onlyOnSale,omitempty" ct:"select,=不限,on=只看在售,default=,sec=collection,label=在售"`
	// FilterOptionKey / FilterOptionValue 属性筛选（issue #25）：按「属性组 key = 属性值 key」
	// 固定筛一个属性值（如 color + red）。访客可交互的多属性筛选走 #27 的筛选条。
	//
	// 为什么拆成两个 props 而不是一个 "color:red"：控件层一句话写错就整块失效，
	// 拆开能分别给出「只填了 key 没填 value」这种明确的配置错误。
	FilterOptionKey   string `json:"filterOptionKey,omitempty" ct:"text,maxlen=64,sec=collection,label=属性 key"`
	FilterOptionValue string `json:"filterOptionValue,omitempty" ct:"text,maxlen=64,sec=collection,label=属性值 key"`
	// PageSize 每页条数（issue #27）：0 = 不分页（整块按 collectionLimit 截断）。
	//
	// 分页在**集合源单次上限（100 条）以内**生效：片段每次按「第几页」取对应切片，
	// 超过上限的部分需要集合源支持 offset（票里记为后续），当前口径在下方 BuildView 里写明。
	PageSize int `json:"pageSize,omitempty" ct:"slider,min=0,max=60,step=1,sec=collection,label=每页条数"`
	// Page 当前页（1 起；由片段参数或 URL 传入，构建期默认 1）。
	Page int `json:"page,omitempty" ct:"number,min=1,max=100,sec=collection,label=页码"`
	// Filters 筛选栏维度（issue #27）：逗号分隔，取 categories / brands / tags / attributes。
	// 空 = 不渲染筛选栏（纯列表）。真正渲染得出来还要集合源实现了筛选选项能力，
	// 否则该块自动不显示（契约缺失不阻断构建）。
	Filters string `json:"filters,omitempty" ct:"text,maxlen=120,sec=collection,label=筛选栏维度"`
	// PriceRanges 预设价格档位（issue #28）：`0-199,200-399,799+` 逗号分隔。
	// 空 = 不渲染价格块。每档渲染成一条筛选链接（与其它筛选同构：无 JS 可点、URL 干净）。
	PriceRanges string `json:"priceRanges,omitempty" ct:"text,maxlen=200,sec=collection,label=预设价格档位"`
	// PriceSlider 可拖动价格滑块：`off` = 不渲染；空 = 渲染（双端点 range）。
	//
	// 滑块是**渐进增强**：没有 JS 时它是原生 range + 表单提交（整页跳转到 `?minPrice=..&maxPrice=..`，
	// 冷启动补正会把它变成筛过的列表）；有 HTMX 时同一表单走片段局部刷新。
	PriceSlider string `json:"priceSlider,omitempty" ct:"select,=开启,off=关闭,default=,sec=collection,label=价格滑块"`
	// PriceBounds 滑块边界：`min,max`（如 `0,1000`），默认 0,1000。
	PriceBounds string `json:"priceBounds,omitempty" ct:"text,maxlen=32,sec=collection,label=滑块边界"`

	// Toolbar 工具条项：逗号分隔，取 sort / pageSize / columns。空 = 不渲染工具条。
	Toolbar string `json:"toolbar,omitempty" ct:"text,maxlen=120,sec=collection,label=工具条"`
	// PushQuery 当前语义参数（片段层渲染前灌入，如 `categoryId=x&page=2`）。
	//
	// 为什么是渲染期输入：交互控件要「切下一页 / 换排序时带上现有筛选」，而构建期不知道
	// 访客当前选了哪些维度 —— 只有片段层见过原始请求参数。故不作为可编辑字段。
	PushQuery string `json:"-" ct:"-"`

	// FilterMinPrice / FilterMaxPrice 价格区间（issue #28）：下推给集合源。
	// 各自由片段参数 `minPrice` / `maxPrice` 灌入（见 PushQuery 的说明）。
	FilterMinPrice string `json:"filterMinPrice,omitempty" ct:"text,maxlen=16,sec=collection,label=最低价"`
	FilterMaxPrice string `json:"filterMaxPrice,omitempty" ct:"text,maxlen=16,sec=collection,label=最高价"`

	// RatingOptions 评分档位（issue #29）：4.5,4,3 逗号分隔，每档表示「≥ N 星」。
	// 空 = 不渲染评分块。取值 0~5，形状在配置期校验。
	RatingOptions string `json:"ratingOptions,omitempty" ct:"text,maxlen=64,sec=collection,label=评分档位"`
	// FilterMinRating 最低评分（issue #29）：由片段参数 minRating 灌入，下推给集合源。
	FilterMinRating string `json:"filterMinRating,omitempty" ct:"text,maxlen=8,sec=collection,label=最低评分"`

	// FilterOptions 多属性筛选（issue #27）：`颜色key:值key,尺码key:值key` 逗号分隔，逐项 AND。
	// 访客交互的多属性筛选（筛选栏点选）在片段侧把选中值拼成这个参数；工作台也能固定写死。
	FilterOptions string `json:"filterOptions,omitempty" ct:"text,maxlen=500,sec=collection,label=多属性筛选"`

	// —— 排序 ——
	// OrderBy 排序口径：默认 = 集合源的确定性序（排序号 → 创建时间 → id）。
	// orderBy 的空值即「默认序」：选项里第一项必须是空 key —— `default=` 是 ct 的保留键
	//（表示控件默认值），把「默认」写成 `default=默认（排序号）` 会被解析成默认值指令而不是选项，
	// 白名单里就只剩后面两个值，工作台插入的默认配置会被自己的控件校验拒掉。
	OrderBy string `json:"orderBy,omitempty" ct:"select,=默认（排序号）,newest=最新创建,oldest=最早创建,priceAsc=价格从低到高,priceDesc=价格从高到低,ratingDesc=评分最高,default=,sec=collection,label=排序"`

	// —— 布局 ——
	Layout string `json:"layout,omitempty" ct:"select,grid=网格,list=列表,default=grid,sec=style,label=布局"`
	// Columns 网格列数（auto = 按容器宽度自适应）。窄屏一律单列，列数只在宽视口生效。
	Columns string `json:"columns,omitempty" ct:"select,auto=自适应,2=两列,3=三列,4=四列,default=auto,sec=style,label=列数"`
	// EmptyText 无商品时的提示文案（留空用默认）。
	EmptyText string `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`

	// —— 卡片字段（与 core.productCard 同一套槽位口径）——
	ImageField        string `json:"imageField,omitempty" ct:"string,maxlen=60,sec=content,label=主图字段"`
	ImageAltField     string `json:"imageAltField,omitempty" ct:"string,maxlen=60,sec=content,label=主图 alt 字段"`
	TitleField        string `json:"titleField,omitempty" ct:"string,maxlen=60,sec=content,label=标题字段"`
	PriceField        string `json:"priceField,omitempty" ct:"string,maxlen=60,sec=content,label=价格字段"`
	ComparePriceField string `json:"comparePriceField,omitempty" ct:"string,maxlen=60,sec=content,label=划线价字段"`
	TagsField         string `json:"tagsField,omitempty" ct:"string,maxlen=60,sec=content,label=标签字段"`
	LinkField         string `json:"linkField,omitempty" ct:"string,maxlen=60,sec=content,label=链接字段"`
	LinkPrefix        string `json:"linkPrefix,omitempty" ct:"text,maxlen=200,sec=content,label=链接前缀"`
	// ListPageLink 列表页链接文案（留空用「全部商品」）。
	//
	// 链接**目标不是作者填的**：它来自系统页面槽位 shop（BIZ-2）——「商品列表在哪一页」
	// 是站点级事实，由运维在槽位里绑一次，改 URL 时链接自动跟着走。
	// 作者在这里只能改文案，改不了目标：让作者手填路径就是允许产物里长出死链。
	ListPageLink string `json:"listPageLink,omitempty" ct:"text,maxlen=30,sec=content,label=列表页链接文案"`
	Currency     string `json:"currency,omitempty" ct:"text,maxlen=8,sec=content,label=货币符号"`
	TitleTag          string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 商品列表组件。
type Component struct {
	core.Atom[Props]
}

// Widget 组件实例。
var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{TypeName: Type, ValidateExtra: validateExtra},
}}

// slotFields 卡片槽位（顺序即渲染顺序）。
type slotField struct {
	Slot  string
	Field string
}

const (
	slotImage        = "image"
	slotImageAlt     = "imageAlt"
	slotTitle        = "title"
	slotPrice        = "price"
	slotComparePrice = "comparePrice"
	slotTags         = "tags"
	slotLink         = "link"
)

func (p *Props) slotFields() []slotField {
	if p == nil {
		return nil
	}
	return []slotField{
		{Slot: slotImage, Field: strings.TrimSpace(p.ImageField)},
		{Slot: slotImageAlt, Field: strings.TrimSpace(p.ImageAltField)},
		{Slot: slotTitle, Field: strings.TrimSpace(p.TitleField)},
		{Slot: slotPrice, Field: strings.TrimSpace(p.PriceField)},
		{Slot: slotComparePrice, Field: strings.TrimSpace(p.ComparePriceField)},
		{Slot: slotTags, Field: strings.TrimSpace(p.TagsField)},
		{Slot: slotLink, Field: strings.TrimSpace(p.LinkField)},
	}
}

// validateExtra 关系性校验：槽位路径形状、标题层级、筛选 id 形状、集合源白名单。
//
// 至少声明一个展示字段（空列表渲染出来是空白块）；筛选 id 只做**形状**校验（非空即可），
// 具体 uuid 合法性由集合源解析器判定 —— 组件不持有商品域的校验规则。
func validateExtra(p *Props, _ string) (err error) {
	switch effectiveSource(p) {
	case "", collectionSourceProduct:
	default:
		return fmt.Errorf("无效的集合源 %q（当前只支持 %s）", p.CollectionSource, collectionSourceProduct)
	}
	declared := 0
	for _, s := range p.slotFields() {
		if s.Field == "" {
			continue
		}
		declared++
		if !fieldPathRe.MatchString(s.Field) {
			return fmt.Errorf("无效的字段路径 %q（期望 item.字段名 或 product.字段名，如 item.name）", s.Field)
		}
	}
	if declared == 0 {
		return fmt.Errorf("至少需要声明一个商品字段（主图 / 标题 / 价格 / 划线价 / 标签 / 链接）")
	}
	switch p.TitleTag {
	case "", "h2", "h3", "h4":
	default:
		return fmt.Errorf("无效的标题层级 %q", p.TitleTag)
	}
	// 校验必须看**原始输入**：effective* 系列会把非法值归一成默认值，
	// 拿归一化后的值做 switch 永远通过（这类错误会让非法配置静默生效）。
	switch p.Layout {
	case "", LayoutGrid, LayoutList:
	default:
		return fmt.Errorf("无效的布局 %q", p.Layout)
	}
	switch p.OrderBy {
	case "", OrderNewest, OrderOldest:
	default:
		return fmt.Errorf("无效的排序 %q", p.OrderBy)
	}
	if p.CollectionLimit < 0 || p.CollectionLimit > maxLimit {
		return fmt.Errorf("取几条必须在 0~%d 之间（0 = 用默认值 %d）", maxLimit, defaultLimit)
	}
	switch p.FilterTagMode {
	case "", "any", "all":
	default:
		return fmt.Errorf("无效的多标签语义 %q（any = 具备任一 / all = 同时具备全部）", p.FilterTagMode)
	}
	switch p.OnlyOnSale {
	case "", "on":
	default:
		return fmt.Errorf("无效的在售开关 %q（空 = 不限 / on = 只看在售）", p.OnlyOnSale)
	}
	// 属性筛选必须成对：只填一半是配置错误，早点报比「筛出空列表」好排查。
	keySet := strings.TrimSpace(p.FilterOptionKey) != ""
	valueSet := strings.TrimSpace(p.FilterOptionValue) != ""
	if keySet != valueSet {
		return fmt.Errorf("属性筛选需要同时填写属性 key 与属性值 key（当前 key=%q value=%q）", p.FilterOptionKey, p.FilterOptionValue)
	}
	if p.PageSize < 0 || p.PageSize > 60 {
		return fmt.Errorf("每页条数必须在 0~60 之间（0 = 不分页）")
	}
	if p.Page < 0 || p.Page > 100 {
		return fmt.Errorf("页码必须在 0~100 之间（0 = 第 1 页）")
	}
	// 筛选栏与工具条：只接受列出的维度 / 项，写错即报错（静默忽略会让作者以为配上了）。
	for _, name := range splitList(p.Filters) {
		if !filterBlockNames[name] {
			return fmt.Errorf("未知的筛选栏维度 %q（可选：categories / brands / tags / attributes）", name)
		}
	}
	for _, name := range splitList(p.Toolbar) {
		if !toolbarItemNames[name] {
			return fmt.Errorf("未知的工具条项 %q（可选：sort / pageSize / columns / onSale）", name)
		}
	}
	switch p.PriceSlider {
	case "", "off":
	default:
		return fmt.Errorf("无效的价格滑块开关 %q（空 = 开启 / off = 关闭）", p.PriceSlider)
	}
	// 预设档位形状：`下限-上限` 或 `下限+`（正数），写错即报错（静默忽略会让作者以为配上了）。
	for _, raw := range splitList(p.PriceRanges) {
		if _, _, err := ParsePriceRange(raw); err != nil {
			return err
		}
	}
	// 评分档位：0~5 的数值，写错即报错。
	for _, raw := range splitList(p.RatingOptions) {
		score, rerr := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if rerr != nil || score < 0 || score > 5 {
			return fmt.Errorf("评分档位 %q 非法（应为 0~5 的数值）", raw)
		}
	}
	if _, _, err := EffectivePriceBounds(p); err != nil {
		return err
	}
	if keySet && (!optionKeyRe.MatchString(strings.TrimSpace(p.FilterOptionKey)) || !optionKeyRe.MatchString(strings.TrimSpace(p.FilterOptionValue))) {
		return fmt.Errorf("属性筛选的 key / 值形状非法（只允许字母数字下划线与连字符）")
	}
	// 多属性：每对必须是 `key:value` 且两半形状合法（缺冒号 / 空半都是配置错误）。
	for _, pair := range strings.Split(p.FilterOptions, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, ":")
		if !ok || !optionKeyRe.MatchString(strings.TrimSpace(key)) || !optionKeyRe.MatchString(strings.TrimSpace(value)) {
			return fmt.Errorf("多属性筛选项 %q 形状非法（期望 属性key:属性值key）", pair)
		}
	}
	return nil
}

// FieldBindings 实现 core.FieldBindingProvider：卡片槽位声明的商品字段绑定。
//
// 与 core.productCard 同一翻译规则：item.<字段> 与 product.<字段> 都报成 product.<字段>，
// 保管存期按实体类型注册表校验（白名单只有 product contract 一份）。
func (c *Component) FieldBindings(node *core.Node) (refs []core.FieldRef, err error) {
	if node == nil {
		return nil, nil
	}
	var p Props
	if len(node.Props) > 0 {
		if uerr := json.Unmarshal(node.Props, &p); uerr != nil {
			return nil, fmt.Errorf("props 反序列化失败: %w", uerr)
		}
	}
	for _, s := range p.slotFields() {
		if s.Field == "" {
			continue
		}
		prefix, field, ok := strings.Cut(s.Field, ".")
		if !ok || field == "" {
			return nil, fmt.Errorf("无效的字段路径 %q", s.Field)
		}
		if prefix != "item" && prefix != "product" {
			return nil, fmt.Errorf("无效的字段前缀 %q（只接受 item. 或 product.）", s.Field)
		}
		refs = append(refs, core.FieldRef{EntityType: "product", Field: field})
	}
	return refs, nil
}

// —— 取值归一 ——

// effectiveSource 有效集合源：**空串表示未选择数据源**（不是「默认商品列表」）。
//
// 默认值语义留给控件（`default=content:product`，工作台表单的初始选中项）；
// 组件把空源当「没有数据源」处理 —— 刚拖出来还没挑源时渲染空态、不查库。
func effectiveSource(p *Props) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.CollectionSource)
}

func effectiveLimit(p *Props) int {
	if p == nil || p.CollectionLimit <= 0 {
		return defaultLimit
	}
	if p.CollectionLimit > maxLimit {
		return maxLimit
	}
	return p.CollectionLimit
}

func effectiveLayout(p *Props) string {
	if p != nil && p.Layout == LayoutList {
		return LayoutList
	}
	return LayoutGrid
}

func effectiveColumns(p *Props) string {
	switch {
	case p == nil:
		return ColumnsAuto
	case p.Columns == "2", p.Columns == "3", p.Columns == "4":
		return p.Columns
	default:
		return ColumnsAuto
	}
}

func effectiveOrder(p *Props) string {
	if p == nil {
		return OrderDefault
	}
	switch p.OrderBy {
	// 白名单必须与控件选项、集合源排序键**三处一致** ——
	// 少列一个的后果是「排序选项看得到、点了没反应」（静默回落默认序），
	// 这种缺陷在只看配置的测试里发现不了，只有断言渲染顺序才抓得住。
	case OrderNewest, OrderOldest, OrderPriceAsc, OrderPriceDesc, OrderRatingDesc:
		return p.OrderBy
	default:
		return OrderDefault
	}
}

func effectiveCurrency(p *Props) string {
	if p == nil || strings.TrimSpace(p.Currency) == "" {
		return defaultCurrency
	}
	return p.Currency
}

func effectiveTitleTag(p *Props) string {
	if p == nil {
		return defaultTitleTag
	}
	switch p.TitleTag {
	case "h2", "h3", "h4":
		return p.TitleTag
	default:
		return defaultTitleTag
	}
}

// EffectivePageSize 每页条数（0 = 不分页）。
func EffectivePageSize(p *Props) int {
	if p == nil || p.PageSize <= 0 {
		return 0
	}
	if p.PageSize > 60 {
		return 60
	}
	return p.PageSize
}

// EffectivePage 当前页（1 起；非法值归一到 1）。
func EffectivePage(p *Props) int {
	if p == nil || p.Page <= 1 {
		return 1
	}
	return p.Page
}

// defaultListPageLinkText 列表页链接的缺省文案。
const defaultListPageLinkText = "全部商品"

// effectiveListPageLinkText 列表页链接文案（留空用缺省）。
func effectiveListPageLinkText(p *Props) string {
	if p == nil || strings.TrimSpace(p.ListPageLink) == "" {
		return defaultListPageLinkText
	}
	return strings.TrimSpace(p.ListPageLink)
}

func effectiveEmptyText(p *Props) string {
	if p == nil || strings.TrimSpace(p.EmptyText) == "" {
		return defaultEmptyText
	}
	return strings.TrimSpace(p.EmptyText)
}

// collectionFilter props 声明的筛选维度 → 集合源的过滤参数（只放有值的维度）。
func collectionFilter(p *Props) map[string]string {
	if p == nil {
		return nil
	}
	f := map[string]string{}
	// 属性维度（issue #25）：键是 `option.<属性key>`——前缀维度，服务端校验子键形状。
	// 多属性（issue #27）：`属性key:属性值key` 对，逐项 AND。
	for _, pair := range strings.Split(p.FilterOptions, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, ":")
		if !ok {
			continue // 形状由 validateExtra 拦（这里静默跳过，不制造半个维度）
		}
		f[optionFilterPrefix+strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if key, value := strings.TrimSpace(p.FilterOptionKey), strings.TrimSpace(p.FilterOptionValue); key != "" && value != "" {
		f[optionFilterPrefix+key] = value
	}
	// 最低评分（issue #29）：与集合源维度同名下推；形状由集合源解析期校验。
	if v := strings.TrimSpace(p.FilterMinRating); v != "" {
		f[filterKeyMinRating] = v
	}
	// 价格区间（issue #28）：与集合源维度同名下推（minPrice / maxPrice）。
	//
	// 数值形状由集合源解析期校验（非数字 / 负数直接报错），组件这里只做非空传递 ——
	// 白名单与形状校验只有一份，不在这里再判一次（两处判断迟早会不一致）。
	if v := strings.TrimSpace(p.FilterMinPrice); v != "" {
		f[filterKeyMinPrice] = v
	}
	if v := strings.TrimSpace(p.FilterMaxPrice); v != "" {
		f[filterKeyMaxPrice] = v
	}
	// 多标签（issue #27）：值与语义分两个键下推（语义只在有多标签时有意义）。
	if ids := strings.TrimSpace(p.FilterTagIDs); ids != "" {
		f[filterKeyTagIDs] = ids
		if mode := strings.TrimSpace(p.FilterTagMode); mode == "all" {
			f[filterKeyTagMode] = mode
		}
	}
	if strings.TrimSpace(p.OnlyOnSale) == "on" {
		f[filterKeyOnSale] = "true"
	}
	for _, kv := range [][2]string{
		{filterKeyStatus, p.FilterStatus},
		{filterKeyCategoryID, p.FilterCategoryID},
		{filterKeyBrandID, p.FilterBrandID},
		{filterKeyTagID, p.FilterTagID},
	} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			f[kv[0]] = v
		}
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

// productListCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed productlist.css
var productListCSS string

// compileCSS 商品列表样式：网格 / 列表两种布局 + 窄屏恒单列。
//
// 多端硬规则：列数与间距都在宽视口生效，窄视口一律单列（列数再多也不能横向溢出）；
// 宽度一律 min(100%, ...)，不写死像素。
//
// Go 侧只保留业务判定：布局归一与列声明计算。网格列数是「值变量」{{cols}}，
// 列表布局独有的两条规则走规则级 @if isList（整块随布局存废）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	layout := effectiveLayout(p)
	vars := map[string]string{
		"isList": core.BoolVar(layout == LayoutList),
		"cols":   columnsDecl(layout, effectiveColumns(p)),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, productListCSS, vars); err != nil {
		panic(fmt.Sprintf("productlist 组件样式解析失败: %v", err))
	}
}

// columnsDecl 网格列声明（窄视口由 BreakpointMobile 覆盖为单列）。
func columnsDecl(layout, cols string) string {
	if layout == LayoutList {
		return "1fr"
	}
	switch cols {
	case "2":
		return "repeat(2, minmax(0, 1fr))"
	case "3":
		return "repeat(3, minmax(0, 1fr))"
	case "4":
		return "repeat(4, minmax(0, 1fr))"
	default:
		// 自适应：按容器宽度尽量多列，单列最小宽度也用 min(100%, …) 封顶，
		// 大卡片 + 窄屏的极端组合下不会溢出。
		return "repeat(auto-fill, minmax(min(100%, 15rem), 1fr))"
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("product_list", productlistTemplate)
}

// productlistTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed product_list.jet
var productlistTemplate string
