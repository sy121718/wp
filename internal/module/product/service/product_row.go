// product_row.go — 商品行的装配（列表 / 详情 / 编辑三页共用同一份）。
//
// 成品行原先在控制器（inbound/http 的 productRow）里拼：它既要取数（评分明细）又要取词
// （标签数量后缀）、还要把分类/品牌/标签的勾选态算好，而「一屏的取数编排与视图装配属于
// service」是本模块的既定分工（见 internal/module/CLAUDE.md §inbound/http）。现在行在这里
// 产出，控制器只做四件事：绑定校验 → 调读模型 → 装配模板 data(事实) → 渲染。
//
// 两条形状约定（改行形状前先读 dto.ProductRowResp 的文件头）：
//   - 行元素一律 dto 结构体（模板全用 .Field 读，结构体换来编译期保护）；只有**带可选键**的
//     下拉选项仍是 map —— 它的语义是「LabelKey 这个键在不在」，而 Jet 对 struct 标量字段的
//     isset 恒真，换成结构体就分不出「空项」与「数据项」；
//   - 只在部分路径出现的整块数据用**可 nil 的指针**（Bundle / Stock：nil 才让模板整块跳过）；
//   - 取词走 ctx（translateFrom），控制器不再往里传取词函数。
package productservice

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"slices"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// productRow 组装单个商品的页面视图数据（列表页的一行 / 详情页 / 编辑页共用）。
//
// 键分两段：汇总段（VariantCount / PriceMin / PriceMax / HasRating / RatingAvg /
// RatingCount / CategoryCell / CategoryOthers / BrandName / TagNames）只回答「有哪些商品」，
// 列表页只用这一段；明细段（Variants / Ratings / CategoryChecks / TagChecks / AutoTags /
// VariationAttributes …）是某个商品的子资源，详情页与编辑页用。
//
// 两页共用同一份组装：同一个值如果在两处各算一遍，分叉时没有任何东西会报错
// （与「同一概念两处口径」是同一类问题）。
//
// selected 是**当前工程**：评分表有 FORCE RLS 策略，明细查询要工程作用域；兜底解析
// （取唯一工程）在多工程下会直接判参数错，所以把页面上正在用的那个工程显式传下来。
//
// error 只表示**取数失败**（评分读不到，见 ratingsOf）：调用方按各自既有的失败通道处理
// （详情页 / 编辑页都走 fail(...) ⇒ LoadFailed 失败页），与列表路径同一档。
// 入参 detail 为 nil 是畸形调用，返回 (nil, nil) —— 调用方本来就有「详情非 nil 才装配」的守卫。
func (s *Service) productRow(ctx context.Context, selected string, flat []*productdto.CategoryResp,
	brands []*productdto.BrandResp, tags []*productdto.TagResp,
	warehouses []productdto.WarehouseOptionResp, detail *productdto.ProductResp) (*productdto.ProductRowResp, error) {
	if detail == nil {
		return nil, nil
	}
	// 评分读一次：明细 + 投影值（issue #33）。**读不到 = 读取失败**（error 向上传播，
	// 页面判 500 失败页）；只有「读到了、这条商品确实没有评分」才渲染零评分。
	// 两者混作一档就是 dd.txt 3.5 验收表最后一行点名的缺陷：数据库故障被渲染成「暂无评分」。
	rating, err := s.ratingsOf(ctx, selected, detail.ID)
	if err != nil {
		return nil, err
	}
	return s.productRowWithRating(ctx, flat, brands, tags, warehouses, detail, rating), nil
}

// productRowWithRating 行装配的本体：评分由调用方给 —— 详情页 / 编辑页给 ratingsOf 的
// 明细 + 投影值（明细行两者都渲染），列表页给批量聚合出来的投影值（见 productRowFromList）。
//
// **本函数不取任何数**：签名里没有 projectID，也没有 model 调用 —— 「这一行要什么原料」
// 由调用方批量备齐，装配只做纯计算。
func (s *Service) productRowWithRating(ctx context.Context, flat []*productdto.CategoryResp,
	brands []*productdto.BrandResp, tags []*productdto.TagResp,
	warehouses []productdto.WarehouseOptionResp, detail *productdto.ProductResp,
	rating *productdto.RatingResp) *productdto.ProductRowResp {
	if detail == nil {
		return nil
	}
	if rating == nil {
		rating = &productdto.RatingResp{}
	}
	ratingRows := make([]productdto.RatingRowResp, 0, len(rating.Items))
	for _, it := range rating.Items {
		ratingRows = append(ratingRows, productdto.RatingRowResp{
			ID: it.ID, Score: formatScore(it.Score),
			Source: it.Source, CreatedAt: it.CreatedAt.Time(),
		})
	}
	categoryCell, categoryOthers := categoryCellLabel(flat, detail.CategoryIDs, detail.PrimaryCategoryID)
	// 标签与自动标签各算一次（列表列的取值与详情页的勾选态共用同一份结果）。
	tagChecks := checkedTagOptions(tags, detail.TagIDs)
	autoTags := attachedAutoTags(tags, detail.TagIDs)
	return &productdto.ProductRowResp{
		ID:   detail.ID,
		Name: detail.Name,
		Slug: detail.Slug,
		// 主体 SKU（products.sku_code）：详情页基本信息区只读展示 + 一句话说明唯一性范围。
		SKUCode: detail.SKUCode,
		Status:  detail.Status,

		Subtitle: detail.Subtitle,
		Unit:     detail.Unit,
		// 富文本字段要的是裸 HTML，库里存的是 {"html": "..."}（jsonb 对象）。
		DescriptionHTML: productDescriptionHTML(detail.Description),
		// 主图（编辑页的单图媒体控件）：与详情响应同一个值（ProductResp.DefaultImage，
		// 已经过 upload.StorageURL 归一）。它与保存路径是一对：编辑页保存时基座把表单里的
		// 隐藏域整份发成 UpdateReq.DefaultImage，所以这里漏赋值 = 控件空着 = 下一次保存把主图静默清空。
		// 两处必须同时在位 —— 只有读没有回显，等于每次保存都擦图。
		DefaultImage: detail.DefaultImage,

		PriceMin: detail.PriceMin,
		PriceMax: detail.PriceMax,

		VariantCount: detail.VariantCount,
		// 变体**清单**行（docs/14 §8）：种子是库里已有的变体（不是重算笛卡尔积），
		// 行上带可编辑的 SKU 与规范化后的 option_values（保存时原样回传）。
		Variants: variantListRows(detail),
		// 引用的属性组（issue #7）：同一属性组可被多个商品共用，这里只展示引用与属性值，
		// 编辑入口在 /admin/product-attributes；组合生成面板只列参与变体的组（issue #8）。
		Attributes:          detail.Attributes,
		VariationAttributes: VariationAttributes(detail.Attributes),

		// 分类与品牌（issue #10）：勾选态 / 选中态都由服务端算好，模板只做展示。
		// CategoryCell / CategoryOthers 是**列表列**要的「一个值 + 其余数量」：
		// 一列一个概念，单元格放值，不把「挂载 N 个分类 · 主分类 X」拼成一句话。
		CategoryChecks: checkedCategoryOptions(flat, detail.CategoryIDs),
		CategoryCell:   categoryCell,
		CategoryOthers: categoryOthers,
		// 主分类 / 品牌下拉：数据项给名字，「（不指定…）」空项给词条 + 中文兜底（事实），
		// 取词在模板 —— 见 productPickOptions。
		PrimaryOptions: productPickOptions(detail.PrimaryCategoryID, CategoryPickOptions(flat)),
		BrandOptions:   productPickOptions(detail.BrandID, productBrandNameOptions(brands)),
		BrandName:      brandNameByID(brands, detail.BrandID),

		// 标签（issue #11）：手工标签勾选挂载（勾选态服务端算好）；自动标签只读展示 ——
		// 归属由规则重算维护，手工改会被下一次重算覆盖，故不提供勾选框。
		TagChecks: tagChecks,
		AutoTags:  autoTags,
		// TagNames 是**列表列**要的事实：这个商品挂了哪些标签（手工 + 自动合并的标签名）。
		// 拼接、超量退化与取词都在模板侧（一列回答一个问题，句子归前端）。
		TagNames: tagCellNames(tagChecks, autoTags),

		// 评分（issue #30 / #33）：明细 + 投影值。**没有评分时 HasRating=false** ——
		// 空态与「评分 0」是两回事，模板据它给出不同文案。
		Ratings:     ratingRows,
		HasRating:   rating.HasRating,
		RatingAvg:   formatScore(rating.Rating),
		RatingCount: rating.RatingCount,

		// 库存列（docs/14 §1.4）：三态 + 分仓明细。真源是 inventory_stocks，
		// 这里只是把服务端算好的聚合结果整理成页面要的形状。
		Stock: productStockCell(detail, warehouses),
	}
}

// productRowFromList 列表页的一行：原料是**列表项**（s.List 的分页结果）+ 批量补齐的
// 挂载事实 + 批量评分投影 —— 全程不取商品详情。
//
// 列表项已经带出汇总段要的全部原料：名称 / 状态 / 价格区间（从变体派生；容器价由
// applyRowFacts 按同一规则补）/ 变体数 / 库存三态（s.List 里 fillProductStock 批量填）；
// 分类·品牌·标签的**名字**来自本页一次读的分类树与品牌·标签清单，行里只需要挂载 id。
//
// 明细段（Subtitle / Unit / DescriptionHTML / Variants / Attributes / Ratings 明细 /
// PrimaryOptions / BrandOptions）在列表路径**刻意为空**：列表模板（products.html）只读
// 汇总段，为它加载完整详情对象正是这次要消掉的开销（见 dto.ProductRowResp 的两段划分）。
func (s *Service) productRowFromList(ctx context.Context, flat []*productdto.CategoryResp,
	brands []*productdto.BrandResp, tags []*productdto.TagResp,
	warehouses []productdto.WarehouseOptionResp, item *productdto.ProductResp,
	agg productmodel.RatingAgg) *productdto.ProductRowResp {
	if item == nil {
		return nil
	}
	return s.productRowWithRating(ctx, flat, brands, tags, warehouses, item, ratingRespOfAgg(agg))
}

// ratingRespOfAgg 把批量聚合结果投影成行装配要的评分形状（**只填投影值，明细为空**）。
//
// 不在 map 里（agg 是零值）= 一条评分都没有 ⇒ HasRating=false、RatingCount=0 ——
// 与详情路径「确实没有评分」时的零值渲染逐字一致（模板据此把评分列显示成 —）。
// **读不到不是这一档**：两条批量读失败会返回 error（见 productRowsFromList）。
func ratingRespOfAgg(agg productmodel.RatingAgg) *productdto.RatingResp {
	return &productdto.RatingResp{Rating: agg.Avg, RatingCount: agg.Count, HasRating: agg.Count > 0}
}

// productRowsFromList 把一页列表项装配成成品行（列表页唯一的行入口）。
//
// 两条批量读都在这里、各一次：挂载事实（ListRowFacts）与评分投影
// （RatingSummariesByProducts）。二者任一失败**返回 error，不吞** —— 页面据此判 500，
// 而不是把「读不到」渲染成「暂无评分」（dd.txt 3.5 验收表最后一行）。
//
// 挂载事实缺行时同样判失败：列表项刚读过而事实读不到，与「逐行取详情失败」是同一档
// 静默失效（少一行却仍写着「共 N 条」，用户据此以为商品被删了）。
func (s *Service) productRowsFromList(ctx context.Context, projectID string, flat []*productdto.CategoryResp,
	brands []*productdto.BrandResp, tags []*productdto.TagResp,
	warehouses []productdto.WarehouseOptionResp, items []*productdto.ProductResp) ([]productdto.ProductRowResp, error) {
	rows := make([]productdto.ProductRowResp, 0, len(items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil && item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return rows, nil
	}
	facts, err := s.m.ListRowFacts(ctx, projectID, ids)
	if err != nil {
		return nil, fmt.Errorf("读取商品列表行的挂载事实失败：%w", err)
	}
	ratings, err := s.m.RatingSummariesByProducts(ctx, projectID, ids)
	if err != nil {
		return nil, fmt.Errorf("读取商品列表行的评分失败：%w", err)
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		if _, ok := facts[item.ID]; !ok {
			return nil, fmt.Errorf("商品 %s 的列表行事实为空", item.ID)
		}
		applyRowFacts(item, facts[item.ID])
		if row := s.productRowFromList(ctx, flat, brands, tags, warehouses, item, ratings[item.ID]); row != nil {
			rows = append(rows, *row)
		}
	}
	return rows, nil
}

// applyRowFacts 把批量事实补到列表项上（行装配的原料）。
//
// 只补**列表行真正缺的那几列**：s.List 的列表项已经带出价格区间（从变体派生）与库存三态。
// 容器价（bundle）是唯一的例外：商品侧只有一个对外价格 = products.default_price，
// 而列表项的 Select 里没有这一列、applyPriceRange 的 bundle 分支又只认它 —— 这里按
// **同一规则**补一次。两处口径是否一致由 product_list_row_parity_test.go 对账
// applyPriceRange 钉住：改了一处、另一处不改就变红。
func applyRowFacts(item *productdto.ProductResp, facts productmodel.ProductRowFacts) {
	if item == nil {
		return
	}
	item.CategoryIDs = decodeStrings(facts.CategoryIDs)
	item.TagIDs = decodeStrings(facts.TagIDs)
	item.PrimaryCategoryID = facts.PrimaryCategoryID
	item.BrandID = facts.BrandID
	if item.Type == productmodel.TypeBundle && facts.DefaultPrice != nil {
		item.DefaultPrice = facts.DefaultPrice
		item.PriceMin, item.PriceMax = *facts.DefaultPrice, *facts.DefaultPrice
	}
}

// ratingsOf 读某商品的评分明细与投影值，**两档语义分开**（dd.txt 3.5 验收表最后一行）：
//
//   - 读取失败（数据库 / 连接 / 超时 / 被 RLS 挡掉的作用域错）→ 返回 error。调用方据此判
//     500 失败页，绝不把「没读到」渲染成「暂无评分」——用户会以为这个商品真的没被打过分，
//     而真相是这一层没读到（重试可能就好了）。
//   - 读到、但这条商品确实没有评分（ListRatings 成功返回空结果）→ 零评分：HasRating=false、
//     RatingCount=0，模板把评分列显示成 —。这条**不是**失败。
//
// 与列表路径同一口径（productRowsFromList 对 RatingSummariesByProducts 的错误同样不吞）。
// 成功却拿到 nil 结果属于畸形返回，与详情为 nil 同一档，判失败。
func (s *Service) ratingsOf(ctx context.Context, projectID, productID string) (*productdto.RatingResp, error) {
	res, err := s.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: productID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("读取商品 %s 的评分失败：%w", productID, err)
	}
	if res == nil {
		return nil, fmt.Errorf("商品 %s 的评分返回为空", productID)
	}
	return res, nil
}

// formatScore 评分统一两位小数（4.5 → 4.50）。
//
// 与集合源给前台的 rating 字段同一口径：评分是 0~5 的小数，用最短表示会得到 4.5 / 4.25
// 混排，两位小数更符合评分展示习惯。
func formatScore(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// variantListRows 变体「清单」表的初始行 —— 直接来自**库里已有的变体**。
//
// 关键语义（docs/14 §8「预览—保存」）：清单的种子是既有变体，**不是重算笛卡尔积** ——
// 重算会让上次删掉的组合自己冒出来。OptionValues 是**规范化后的 JSON 文本**
// （逐行原样回传服务端做组合去重与跳过提示），规格列的可读文本由服务端拼（SpecLabel）。
func variantListRows(detail *productdto.ProductResp) []productdto.VariantListRowResp {
	rows := make([]productdto.VariantListRowResp, 0, len(detail.Variants))
	for _, v := range detail.Variants {
		if v == nil {
			continue
		}
		rows = append(rows, productdto.VariantListRowResp{
			ID: v.ID, SKUCode: v.SKUCode, Spec: SpecLabel(v.OptionValues, detail.Attributes),
			OptionValues: optionValuesJSON(v.OptionValues),
			Price:        v.Price,
			ComparePrice: v.ComparePrice,
			CostPrice:    v.CostPrice,
			Enabled:      v.Enabled, StockTotal: v.StockTotal,
		})
	}
	return rows
}

// optionValuesJSON 变体组合的规范化 JSON 文本（空 / null 归一到 {}）。
//
// 它同时是清单行的 data 属性值与预览去重的入参：组合为空的对象表示「无规格变体」
// （商品创建时的首个变体），归一成 {} 而不是空串，前端 JSON.parse 才不会炸。
func optionValuesJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "{}"
	}
	return s
}

// productDescriptionHTML 从商品描述的 JSON 里取正文（富文本字段要裸 HTML）。
//
// 库里存的是 {"html": "..."}（jsonb 对象），也兼容直接存字符串的历史值。
func productDescriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return s
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return ""
}

// productStockCell 商品行「库存」列的数据（三态 + 分仓明细）。
//
// 三态口径（docs/14 §1.4）：任一仓不跟踪 → ∞（**绝不求和**，求和等于把无限当 0）；
// 全部跟踪 → 各仓数量之和（0 就显示 0）；一个仓都没有这一行 → 未入库。
//
// 分仓明细按**工程仓库清单**逐仓列出：服务端返回的 StockWarehouses 只含有库存行的仓，
// 「某仓没有行」同样是一条信息（该仓未入库）—— 只列有行的仓会让运营以为这个商品只在那几个仓。
//
// 文案（∞ / 未入库）留在模板侧用 tr() 取词，这里只给出状态与数字：
// 一种语言一个值，别把中文拼进数据层（接口调用方拿到的会是中文）。
func productStockCell(detail *productdto.ProductResp, warehouses []productdto.WarehouseOptionResp) *productdto.StockCellResp {
	cell := &productdto.StockCellResp{
		State: productenums.StockStateNone,
		Total: 0,
		Rows:  make([]productdto.StockRowResp, 0, len(warehouses)),
	}
	if detail == nil {
		return cell
	}
	if detail.StockState != "" {
		cell.State = detail.StockState
	}
	cell.Total = detail.StockTotal
	byWarehouse := make(map[string]*productdto.ProductWarehouseStockResp, len(detail.StockWarehouses))
	for _, row := range detail.StockWarehouses {
		if row != nil {
			byWarehouse[row.WarehouseID] = row
		}
	}
	for _, w := range warehouses {
		item := productdto.StockRowResp{WarehouseID: w.ID, WarehouseLabel: w.Label,
			State: productenums.StockStateNone, Quantity: 0}
		if row, ok := byWarehouse[w.ID]; ok {
			item.State = row.State
			item.Quantity = row.Quantity
		}
		cell.Rows = append(cell.Rows, item)
	}
	return cell
}

// checkedCategoryOptions 商品页的分类勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedCategoryOptions(flat []*productdto.CategoryResp, checked []string) []productdto.CategoryCheckRowResp {
	picked := make(map[string]bool, len(checked))
	for _, id := range checked {
		picked[id] = true
	}
	out := make([]productdto.CategoryCheckRowResp, 0, len(flat))
	for _, node := range flat {
		if node == nil {
			continue
		}
		out = append(out, productdto.CategoryCheckRowResp{
			ID: node.ID, Label: CategoryLabel(node), Checked: picked[node.ID],
		})
	}
	return out
}

// categoryNameByID 按 id 取分类名（找不到返回空串，不让悬空引用把页面打崩）。
func categoryNameByID(flat []*productdto.CategoryResp, id string) string {
	for _, node := range flat {
		if node.ID == id {
			return node.Name
		}
	}
	return ""
}

// brandNameByID 按 id 取品牌名（找不到返回空串）。
func brandNameByID(brands []*productdto.BrandResp, id string) string {
	for _, b := range brands {
		if b != nil && b.ID == id {
			return b.Name
		}
	}
	return ""
}

// checkedTagOptions 商品行的手工标签勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedTagOptions(tags []*productdto.TagResp, attached []string) []productdto.TagCheckRowResp {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]productdto.TagCheckRowResp, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindManual {
			continue
		}
		out = append(out, productdto.TagCheckRowResp{ID: t.ID, Name: t.Name, Checked: have[t.ID]})
	}
	return out
}

// attachedAutoTags 商品已归属的自动标签（只读展示：归属由规则重算维护）。
func attachedAutoTags(tags []*productdto.TagResp, attached []string) []productdto.AutoTagRowResp {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]productdto.AutoTagRowResp, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindRule || !have[t.ID] {
			continue
		}
		out = append(out, productdto.AutoTagRowResp{
			ID: t.ID, Name: t.Name, RuleType: t.RuleType, RuleValues: t.RuleValues,
		})
	}
	return out
}

// categoryCellLabel 商品行「分类」列要显示的单个值（一列一个概念：单元格是值，不是句子）。
//
// 取值优先级：主分类 > 挂载分类里的第一个 —— 挂了分类却没设主分类时显示空值是误导
// （用户会以为这个商品没有分类）。都没有才返回空串，由模板渲染成 —。
// others 是「其余分类数」，模板用 +N 徽章跟在值后面，不把三个信号拼成一句话。
func categoryCellLabel(flat []*productdto.CategoryResp, attached []string, primaryID string) (label string, others int) {
	if name := categoryNameByID(flat, primaryID); name != "" {
		return name, max(len(attached)-1, 0)
	}
	for _, node := range flat {
		if node == nil || !slices.Contains(attached, node.ID) {
			continue
		}
		return node.Name, max(len(attached)-1, 0)
	}
	return "", 0
}

// tagCellNames 商品行「标签」列的事实：这个商品挂了哪些标签（手工挂载 + 命中规则的自动标签）。
//
// 只给名字列表 —— 来源分解（手工 N / 自动 M）不属于这一列要回答的问题，拼接成一句话、
// 超量时退化成数量、以及按请求语言取词，全部归模板（见 products.html 的标签列）。
func tagCellNames(tagChecks []productdto.TagCheckRowResp, autoTags []productdto.AutoTagRowResp) []string {
	names := make([]string, 0, len(tagChecks)+len(autoTags))
	for _, t := range tagChecks {
		if !t.Checked || t.Name == "" {
			continue
		}
		names = append(names, t.Name)
	}
	for _, t := range autoTags {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names
}

// productPickOptions 「含空项的下拉」的选项：「—」那一行只有 IsNone，文案由模板按用途取词
// （主分类 / 品牌 / 捆绑来源各是各的句子）；其余行给名字（分类名 / 品牌名 / 商品名）。
//
// data 只放**数据项**：分类侧用 CategoryPickOptions（分类名带层级缩进的真源只有一处），
// 品牌侧由 productBrandNameOptions 给出同一个形状。
func productPickOptions(selected string, data []productdto.PickOptionResp) []productdto.PickOptionResp {
	out := make([]productdto.PickOptionResp, 0, len(data)+1)
	out = append(out, productdto.PickOptionResp{Selected: selected == "", IsNone: true})
	for _, item := range data {
		item.Selected = item.Value == selected
		out = append(out, item)
	}
	return out
}

// productBrandNameOptions 品牌下拉的数据项（id + 品牌名）。
//
// 与分类侧用 CategoryPickOptions 同一取舍：本函数只交**数据**，
// 「（不指定品牌）」空项的词条由 productPickOptions 补。
func productBrandNameOptions(brands []*productdto.BrandResp) []productdto.PickOptionResp {
	out := make([]productdto.PickOptionResp, 0, len(brands))
	for _, b := range brands {
		if b != nil {
			out = append(out, productdto.PickOptionResp{Value: b.ID, Label: b.Name})
		}
	}
	return out
}
