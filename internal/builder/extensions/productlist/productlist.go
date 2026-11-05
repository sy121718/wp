package productlist

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.productList"

const collectionSourceProduct = "content:product"

const (
	filterKeyStatus       = "status"
	filterKeyCategoryID   = "categoryId"
	filterKeyCategoryIDs  = "categoryIds"
	filterKeyCategoryMode = "categoryMode"
	filterKeyBrandID      = "brandId"
	filterKeyBrandIDs     = "brandIds"
	filterKeyBrandMode    = "brandMode"
	filterKeyTagID        = "tagId"
	filterKeyTagIDs       = "tagIds"
	filterKeyTagMode      = "tagMode"
	filterKeyOnSale       = "onSale"
	filterKeyMinRating    = "minRating"
	filterKeyMinPrice     = "minPrice"
	filterKeyMaxPrice     = "maxPrice"

	optionFilterPrefix = "option."
)

var filterBlockNames = map[string]bool{
	"categories": true, "brands": true, "tags": true, "attributes": true,
}

var toolbarItemNames = map[string]bool{"sort": true, "pageSize": true, "columns": true, "onSale": true}

type PriceRange struct {
	Min float64
	Max *float64
}

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

func PriceSliderEnabled(p *Props) bool {
	return p == nil || p.PriceSlider != "off"
}

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

var optionKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const (
	LayoutGrid = "grid"
	LayoutList = "list"

	OrderDefault = "default"
	OrderNewest  = "newest"
	OrderOldest  = "oldest"

	OrderRatingDesc = "ratingDesc"
	OrderPriceAsc   = "priceAsc"
	OrderPriceDesc  = "priceDesc"

	ColumnsAuto = "auto"
)

const (
	defaultLimit     = 12
	maxLimit         = 100
	defaultCurrency  = "¥"
	defaultTitleTag  = "h3"
	defaultEmptyText = "暂无商品"

	MaxPageSize = 60
	MaxPage     = 100
)

var fieldPathRe = regexp.MustCompile(`^(item|product)\.[a-z][a-zA-Z0-9_]*$`)

type Props struct {
	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=未选择,content:product=商品列表,default=content:product,sec=collection,label=数据源"`

	CollectionLimit int `json:"collectionLimit,omitempty" ct:"slider,min=1,max=100,step=1,sec=collection,label=取几条"`

	FilterStatus string `json:"filterStatus,omitempty" ct:"select,=全部,draft=草稿,published=已发布,archived=已归档,default=published,sec=collection,label=状态"`

	FilterFromArchive string `json:"filterFromArchive,omitempty" ct:"select,=关闭,on=按归档上下文,default=,sec=collection,label=筛选来源"`
	FilterCategoryID  string `json:"filterCategoryId,omitempty" ct:"entityref,category,label=分类"`

	FilterCategoryIDs  string `json:"filterCategoryIds,omitempty" ct:"multientityref,category,maxlen=500,sec=collection,label=分类 id 列表"`
	FilterCategoryMode string `json:"filterCategoryMode,omitempty" ct:"select,=具备任一,any=具备任一,all=同时具备全部,default=,sec=collection,label=多分类语义"`
	FilterBrandID      string `json:"filterBrandId,omitempty" ct:"entityref,brand,label=品牌"`

	FilterBrandIDs  string `json:"filterBrandIds,omitempty" ct:"multientityref,brand,maxlen=500,sec=collection,label=品牌 id 列表"`
	FilterBrandMode string `json:"filterBrandMode,omitempty" ct:"select,=具备任一,any=具备任一,all=同时具备全部,default=,sec=collection,label=多品牌语义"`
	FilterTagID     string `json:"filterTagId,omitempty" ct:"entityref,tag,label=标签"`

	FilterTagIDs string `json:"filterTagIds,omitempty" ct:"multientityref,tag,maxlen=500,sec=collection,label=标签 id 列表"`

	FilterTagMode string `json:"filterTagMode,omitempty" ct:"select,=具备任一,any=具备任一,all=同时具备全部,default=,sec=collection,label=多标签语义"`

	OnlyOnSale string `json:"onlyOnSale,omitempty" ct:"select,=不限,on=只看在售,default=,sec=collection,label=在售"`

	FilterOptionKey   string `json:"filterOptionKey,omitempty" ct:"text,maxlen=64,sec=collection,label=属性 key"`
	FilterOptionValue string `json:"filterOptionValue,omitempty" ct:"text,maxlen=64,sec=collection,label=属性值 key"`

	PageSize int `json:"pageSize,omitempty" ct:"slider,min=0,max=60,step=1,sec=collection,label=每页条数"`

	Page int `json:"page,omitempty" ct:"number,min=1,max=100,sec=collection,label=页码"`

	Filters string `json:"filters,omitempty" ct:"text,maxlen=120,sec=collection,label=筛选栏维度"`

	CategoryMulti string `json:"categoryMulti,omitempty" ct:"select,=多选,off=单选,default=,sec=collection,label=分类多选"`

	CaretIcon string `json:"caretIcon,omitempty" ct:"select,chevron-right=默认箭头,dot=圆点,square=方块,none=无图标,default=chevron-right,sec=collection,label=折叠图标"`

	PriceRanges string `json:"priceRanges,omitempty" ct:"rangelist,maxlen=200,sec=collection,label=预设价格档位"`

	PriceSlider string `json:"priceSlider,omitempty" ct:"select,=开启,off=关闭,default=,sec=collection,label=价格滑块"`

	PriceBounds string `json:"priceBounds,omitempty" ct:"text,maxlen=32,sec=collection,label=滑块边界"`

	Toolbar string `json:"toolbar,omitempty" ct:"text,maxlen=120,sec=collection,label=工具条"`

	PushQuery string `json:"pushQuery,omitempty" ct:"-"`

	FilterMinPrice string `json:"filterMinPrice,omitempty" ct:"text,maxlen=16,sec=collection,label=最低价"`
	FilterMaxPrice string `json:"filterMaxPrice,omitempty" ct:"text,maxlen=16,sec=collection,label=最高价"`

	RatingOptions string `json:"ratingOptions,omitempty" ct:"text,maxlen=64,sec=collection,label=评分档位"`

	FilterMinRating string `json:"filterMinRating,omitempty" ct:"text,maxlen=8,sec=collection,label=最低评分"`

	FilterOptions string `json:"filterOptions,omitempty" ct:"text,maxlen=500,sec=collection,label=多属性筛选"`

	OrderBy string `json:"orderBy,omitempty" ct:"select,=默认（排序号）,newest=最新创建,oldest=最早创建,priceAsc=价格从低到高,priceDesc=价格从高到低,ratingDesc=评分最高,default=,sec=collection,label=排序"`

	Layout string `json:"layout,omitempty" ct:"select,grid=网格,list=列表,default=grid,sec=style,label=布局"`

	Columns string `json:"columns,omitempty" ct:"select,auto=自适应,2=两列,3=三列,4=四列,default=auto,sec=style,label=列数"`

	EmptyText string `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`

	ImageField        string `json:"imageField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=主图字段"`
	ImageAltField     string `json:"imageAltField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=主图 alt 字段"`
	TitleField        string `json:"titleField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=标题字段"`
	PriceField        string `json:"priceField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=价格字段"`
	ComparePriceField string `json:"comparePriceField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=划线价字段"`
	TagsField         string `json:"tagsField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=标签字段"`
	LinkField         string `json:"linkField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=链接字段"`
	LinkPrefix        string `json:"linkPrefix,omitempty" ct:"text,maxlen=200,sec=content,label=链接前缀"`

	ListPageLink string `json:"listPageLink,omitempty" ct:"text,maxlen=30,sec=content,label=列表页链接文案"`
	Currency     string `json:"currency,omitempty" ct:"string,maxlen=8,sec=content,label=货币符号"`
	TitleTag     string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct {
	core.Atom[Props]
}

func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{{Key: "list", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionExternal}}}
}
func (*Component) RequiredActions() []string { return nil }

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,

		CollectionProp:  "collectionSource",
		PaletteCategory: core.PaletteCategoryBasic,
		DisplayName:     "商品列表",
		Hint:            "网格 / 列表铺开一批商品",
		DefaultProps: map[string]any{
			"collectionLimit":   8,
			"layout":            "grid",
			"columns":           "auto",
			"filterStatus":      "published",
			"imageField":        "item.images",
			"titleField":        "item.name",
			"priceField":        "item.priceRange",
			"comparePriceField": "item.comparePrice",
			"tagsField":         "item.tags",
			"linkField":         "item.url",
			"linkPrefix":        "",
			"currency":          "¥",
			"titleTag":          "h3",
			"emptyText":         "暂无商品",
		},
	},
}}

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

	for _, mode := range [][2]string{
		{p.FilterCategoryMode, "多分类"},
		{p.FilterBrandMode, "多品牌"},
		{p.FilterTagMode, "多标签"},
	} {
		switch mode[0] {
		case "", "any", "all":
		default:
			return fmt.Errorf("无效的%s语义 %q（any = 具备任一 / all = 同时具备全部）", mode[1], mode[0])
		}
	}
	switch p.OnlyOnSale {
	case "", "on":
	default:
		return fmt.Errorf("无效的在售开关 %q（空 = 不限 / on = 只看在售）", p.OnlyOnSale)
	}

	keySet := strings.TrimSpace(p.FilterOptionKey) != ""
	valueSet := strings.TrimSpace(p.FilterOptionValue) != ""
	if keySet != valueSet {
		return fmt.Errorf("属性筛选需要同时填写属性 key 与属性值 key（当前 key=%q value=%q）", p.FilterOptionKey, p.FilterOptionValue)
	}
	if p.PageSize < 0 || p.PageSize > MaxPageSize {
		return fmt.Errorf("每页条数必须在 0~%d 之间（0 = 不分页）", MaxPageSize)
	}
	if p.Page < 0 || p.Page > MaxPage {
		return fmt.Errorf("页码必须在 0~%d 之间（0 = 第 1 页）", MaxPage)
	}

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

	for _, raw := range splitList(p.PriceRanges) {
		if _, _, err := ParsePriceRange(raw); err != nil {
			return err
		}
	}

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

	for _, pair := range strings.Split(p.FilterOptions, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, values, ok := strings.Cut(pair, ":")
		key = strings.TrimSpace(key)
		if !ok || !optionKeyRe.MatchString(key) {
			return fmt.Errorf("多属性筛选项 %q 形状非法（期望 属性key:属性值key[,属性值key…]）", pair)
		}
		for _, value := range strings.Split(values, ",") {
			if value = strings.TrimSpace(value); !optionKeyRe.MatchString(value) {
				return fmt.Errorf("多属性筛选项 %q 形状非法（期望 属性key:属性值key[,属性值key…]）", pair)
			}
		}
	}
	return nil
}

var singleToMultiKey = map[string]string{
	filterKeyCategoryID: filterKeyCategoryIDs,
	filterKeyBrandID:    filterKeyBrandIDs,
	filterKeyTagID:      filterKeyTagIDs,
}

func mergeOptionValues(existing, extra string) string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, raw := range []string{existing, extra} {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return strings.Join(out, ",")
}

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

func effectiveFilterStatus(p *Props) string {
	if p != nil {
		if v := strings.TrimSpace(p.FilterStatus); v != "" {
			return v
		}
	}

	return "published"
}

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

func EffectivePageSize(p *Props) int {
	if p == nil || p.PageSize <= 0 {
		return 0
	}
	if p.PageSize > 60 {
		return 60
	}
	return p.PageSize
}

func EffectivePage(p *Props) int {
	if p == nil || p.Page <= 1 {
		return 1
	}
	return p.Page
}

const defaultListPageLinkText = "全部商品"

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

func collectionFilter(p *Props) map[string]string {
	if p == nil {
		return nil
	}
	f := map[string]string{}

	for _, pair := range strings.Split(p.FilterOptions, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, values, ok := strings.Cut(pair, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if merged := mergeOptionValues(f[optionFilterPrefix+key], strings.TrimSpace(values)); merged != "" {
			f[optionFilterPrefix+key] = merged
		}
	}
	if key, value := strings.TrimSpace(p.FilterOptionKey), strings.TrimSpace(p.FilterOptionValue); key != "" && value != "" {
		f[optionFilterPrefix+key] = value
	}

	if v := strings.TrimSpace(p.FilterMinRating); v != "" {
		f[filterKeyMinRating] = v
	}

	if v := strings.TrimSpace(p.FilterMinPrice); v != "" {
		f[filterKeyMinPrice] = v
	}
	if v := strings.TrimSpace(p.FilterMaxPrice); v != "" {
		f[filterKeyMaxPrice] = v
	}

	for _, multi := range [][4]string{
		{p.FilterCategoryIDs, filterKeyCategoryIDs, filterKeyCategoryMode, p.FilterCategoryMode},
		{p.FilterBrandIDs, filterKeyBrandIDs, filterKeyBrandMode, p.FilterBrandMode},
		{p.FilterTagIDs, filterKeyTagIDs, filterKeyTagMode, p.FilterTagMode},
	} {
		ids := strings.TrimSpace(multi[0])
		if ids == "" {
			continue
		}
		f[multi[1]] = ids
		if mode := strings.TrimSpace(multi[3]); mode == "all" {
			f[multi[2]] = mode
		}
	}
	if strings.TrimSpace(p.OnlyOnSale) == "on" {
		f[filterKeyOnSale] = "true"
	}
	for _, kv := range [][2]string{
		{filterKeyStatus, effectiveFilterStatus(p)},
		{filterKeyCategoryID, p.FilterCategoryID},
		{filterKeyBrandID, p.FilterBrandID},
		{filterKeyTagID, p.FilterTagID},
	} {
		v := strings.TrimSpace(kv[1])
		if v == "" {
			continue
		}

		if multiKey, ok := singleToMultiKey[kv[0]]; ok && f[multiKey] != "" {
			continue
		}
		f[kv[0]] = v
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

//go:embed productlist.css
var productListCSS string

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

		return "repeat(auto-fill, minmax(min(100%, 15rem), 1fr))"
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("product_list", productlistTemplate)
}

//go:embed product_list.jet
var productlistTemplate string
