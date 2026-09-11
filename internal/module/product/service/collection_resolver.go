package productservice

// collection_resolver.go — 商品集合源（issue #9）。
//
// 把商品注册成集合源后，集合类组件（core.cardstack 等）即可直接绑定商品字段：
//   集合源元数据（CollectionSchemas）给出字段白名单 / 过滤维度 / 排序键，
//   构建期按白名单解析出集合项（ResolveCollection）静态填入产物。
//
// 两条与实体解析器（entity_source.go）一致的铁律：
//  1. 白名单是唯一来源（productcontract），解析只返回白名单内字段；
//  2. 发布产物零查库（不变量 1）—— 查库全部发生在构建期，这里就是那一处。
//
// 与实体解析器的差异：集合项是「列表卡」的数据（map[string]any），数组字段给
// 真数组（图片取首元素是集合卡的既有渲染约定），而实体绑定给的是 JSON 文本。

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// collectionItemLimit 单次集合解析返回的条目上限。
//
// 与内容集合（content 模块同样取 100）保持同一口径：集合卡是页面区块，不是分页列表；
// 真正需要「翻页的商品列表」属于后续的数据驱动页面能力，不在集合源职责内。
const collectionItemLimit = 100

// ResolveCollection 实现 core.CollectionResolver：解析 "content:product" 集合源。
//
// filter 为白名单等值过滤（当前只有 status）：白名单外的维度直接报错，
// 不接受任意过滤表达式（不变量 4）。
// 工程范围取自构建上下文（core.BuildProjectID，由 builder.Compile 注入）：
// 商品是分工程的数据，缺工程 ID 时不限工程（后台预览等无站点上下文的场景）。
func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	if source != productcontract.CollectionSourceProduct {
		return nil, fmt.Errorf("%s: %q", productenums.ErrCollectionSourceInvalid, source)
	}
	f, err := parseCollectionFilter(filter)
	if err != nil {
		return nil, err
	}
	// 工程范围取自构建上下文（后台预览等无站点上下文的场景为空 = 不限工程）。
	f.ProjectID = core.BuildProjectID(ctx)
	rows, err := s.m.ListForCollection(ctx, f, collectionItemLimit, 0)
	if err != nil {
		return nil, err
	}
	items = make([]map[string]any, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	lang := core.BuildLang(ctx)
	ids := make([]string, 0, len(rows))
	attrIDs := make([]string, 0, len(rows))
	seenAttr := map[string]bool{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		for _, id := range decodeStrings(r.AttributeIDs) {
			if !seenAttr[id] {
				seenAttr[id] = true
				attrIDs = append(attrIDs, id)
			}
		}
	}
	// 变体与属性组各批量取一次（列表页专用，零 N+1）：价格区间的派生需要变体。
	variants, err := s.m.ListVariantsByProducts(ctx, ids)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	attrs, err := s.m.ListAttributesByIDs(ctx, attrIDs)
	if err != nil {
		return nil, err
	}
	// 分类 / 品牌 / 标签一次取好（issue #12：展示名要取译文，逐个商品查会变成 N 次）。
	categoryIndex, brandIndex, tagIndex, ierr := s.taxonomyIndex(ctx, rows)
	if ierr != nil {
		return nil, ierr
	}
	for _, r := range rows {
		// 与实体绑定同源：同一份派生值 + 同一份译文替换（语境 实体类型.<字段名>）。
		loc, lerr := s.localizeRelatedFrom(ctx, lang, r, categoryIndex, brandIndex, tagIndex, attrs)
		if lerr != nil {
			return nil, lerr
		}
		values := productFieldValues(r, byProduct[r.ID], attrs, loc)
		s.translateFields(ctx, lang, productcontract.EntityTypeProduct, values)
		items = append(items, collectionItem(r, values))
	}
	return items, nil
}

// taxonomyIndex 一次取回该批商品引用的分类 / 品牌 / 标签（列表页专用，零 N+1）。
func (s *Service) taxonomyIndex(ctx context.Context, rows []*productmodel.ProductEntity) (categories map[string]*productmodel.ProductCategoryEntity, brands map[string]*productmodel.ProductBrandEntity, tags map[string]*productmodel.ProductTagEntity, err error) {
	categoryIDs, brandIDs, tagIDs := []string{}, []string{}, []string{}
	seenCategory, seenBrand, seenTag := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		for _, id := range decodeStrings(r.CategoryIDs) {
			if !seenCategory[id] {
				seenCategory[id] = true
				categoryIDs = append(categoryIDs, id)
			}
		}
		if r.BrandID != nil && !seenBrand[*r.BrandID] {
			seenBrand[*r.BrandID] = true
			brandIDs = append(brandIDs, *r.BrandID)
		}
		for _, id := range decodeStrings(r.TagIDs) {
			if !seenTag[id] {
				seenTag[id] = true
				tagIDs = append(tagIDs, id)
			}
		}
	}
	categories, brands, tags = map[string]*productmodel.ProductCategoryEntity{}, map[string]*productmodel.ProductBrandEntity{}, map[string]*productmodel.ProductTagEntity{}
	if len(categoryIDs) > 0 {
		categoryRows, cerr := s.m.ListCategoriesByIDs(ctx, categoryIDs)
		if cerr != nil {
			return nil, nil, nil, cerr
		}
		for _, row := range categoryRows {
			categories[row.ID] = row
		}
	}
	if len(brandIDs) > 0 {
		brandRows, berr := s.m.ListBrandsByIDs(ctx, brandIDs)
		if berr != nil {
			return nil, nil, nil, berr
		}
		for _, row := range brandRows {
			brands[row.ID] = row
		}
	}
	if len(tagIDs) > 0 {
		tagRows, terr := s.m.ListTagsByIDs(ctx, tagIDs)
		if terr != nil {
			return nil, nil, nil, terr
		}
		for _, row := range tagRows {
			tags[row.ID] = row
		}
	}
	return categories, brands, tags, nil
}

// CollectionSchemas 实现 core.CollectionSchemaProvider：商品集合源的元数据。
//
// 字段白名单直接取 contract 的唯一来源（与实体类型注册表同一份），
// 过滤维度与排序键同出 contract —— 集合源元数据不含第二份白名单。
func (s *Service) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{
		Source:  productcontract.CollectionSourceProduct,
		Label:   productcontract.CollectionLabel,
		Fields:  productcontract.FieldWhitelist(productcontract.EntityTypeProduct),
		Filters: productcontract.CollectionFilters(),
		OrderBy: productcontract.CollectionOrderBy(),
	}}, nil
}

// parseCollectionFilter 校验并取出集合源的下推条件（issue #21）。
//
// 两条拒绝：白名单外的维度（不变量 4）、id 维度形状非法。后者刻意也报错而不是
// 「让它匹配不到任何行」—— 非法 id 下推到 SQL 得到的是空集合，那是把配置错误
// 伪装成「这个分类下没有商品」，构建期必须显式失败。空值 = 该维度不参与过滤。
func parseCollectionFilter(filter map[string]string) (f productmodel.CollectionFilter, err error) {
	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 报错文案里的维度顺序稳定（同输入同报错）
	for _, k := range keys {
		if !productcontract.IsCollectionFilterKey(k) {
			return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
		}
		v := strings.TrimSpace(filter[k])
		if v == "" {
			continue
		}
		// 前缀维度（属性值，issue #25）：`option.<属性key>=<属性值key>`。
		//
		// 这里只校验**形状**（键与值的字符集 / 长度）；某个属性值到底存不存在由 SQL 决定 ——
		// 与 categoryId / tagId 的口径一致：形状错是配置错误该报错，值匹配不到只是空集合。
		if attrKey, ok := strings.CutPrefix(k, productcontract.CollectionFilterOptionPrefix); ok {
			if !optionKeyRe.MatchString(attrKey) || !optionKeyRe.MatchString(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			if f.Options == nil {
				f.Options = map[string]string{}
			}
			f.Options[attrKey] = v
			continue
		}
		switch k {
		case productcontract.CollectionFilterStatus:
			f.Status = v
		case productcontract.CollectionFilterCategoryID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.CategoryID = v
		case productcontract.CollectionFilterBrandID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.BrandID = v
		case productcontract.CollectionFilterTagID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.TagID = v
		case productcontract.CollectionFilterTagIDs:
			// 多标签（issue #27）：逗号分隔的 uuid 列表；任何一个形状不对就报错，
			// 不静默丢弃（丢一个 id 会让筛选结果莫名变多，比报错难查得多）。
			ids := splitCSV(v)
			for _, id := range ids {
				if !isUUID(id) {
					return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
				}
			}
			if len(ids) > 0 {
				f.TagIDs = ids
			}
		case productcontract.CollectionFilterTagMode:
			switch v {
			case productcontract.CollectionTagModeAll:
				f.TagAll = true
			case productcontract.CollectionTagModeAny:
				f.TagAll = false
			default:
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
		case productcontract.CollectionFilterMinPrice, productcontract.CollectionFilterMaxPrice:
			// 价格区间（issue #28）：形状非法 / 负数 / 下限大于上限一律报错，
			// 不伪装成空集合（配置错误与「确实没这个价位的商品」是两件事）。
			amount, perr := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if perr != nil || amount < 0 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			if k == productcontract.CollectionFilterMinPrice {
				f.MinPrice = &amount
			} else {
				f.MaxPrice = &amount
			}
		case productcontract.CollectionFilterOnSale:
			switch v {
			case "true", "1":
				f.OnSale = true
			case "false", "0":
				f.OnSale = false
			default:
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
		}
	}
	// 区间上下限的相互关系在两维都解析完之后统一校验（它们可能以任意顺序出现）。
	return f, validatePriceRange(f)
}

// validatePriceRange 下限不得大于上限（解析完成后统一校验：两维可能任意顺序出现）。
func validatePriceRange(f productmodel.CollectionFilter) error {
	if f.MinPrice != nil && f.MaxPrice != nil && *f.MinPrice > *f.MaxPrice {
		return fmt.Errorf("%s: minPrice 大于 maxPrice", productenums.ErrCollectionFilterInvalid)
	}
	return nil
}

// splitCSV 逗号分隔值 → 去空、去重的列表（顺序保持首次出现）。
//
// 空串返回空列表 = 「该维度不参与筛选」，与单值维度的空值语义一致。
func splitCSV(raw string) []string {
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

// optionKeyRe 属性维度键 / 值的形状（属性组 key 与属性值 key 都走这个字符集）。
//
// 宽松是刻意的：属性 key 由用户建属性组时填，规则不该在筛选这一层重新发明；
// 这里的作用是挡住空键、超长键与明显不是键的输入，SQL 注入由参数化绑定兜住。
var optionKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// isUUID 形状校验（只关心「是不是 uuid」，不关心版本）。
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// collectionItem 集合项：白名单字段值 + 系统字段（id / slug）。
//
// 图片用真数组而不是 JSON 文本（与实体绑定的 images 形态不同）：集合卡的既有约定是
// 「数组字段取首元素」（core.ItemFieldText / cardstack.fieldText），给 JSON 文本会让
// 卡片渲染出 ["/storage/x.jpg"] 这样的字面量。
func collectionItem(p *productmodel.ProductEntity, values map[string]string) map[string]any {
	item := make(map[string]any, len(values)+2)
	for k, v := range values {
		item[k] = v
	}
	item["id"] = p.ID
	item["slug"] = p.Slug
	item["images"] = imageURLsAny(p)
	// createdAt 给 RFC3339（UTC）：组件要按时间排序，格式必须可解析且与时区无关。
	item["createdAt"] = p.CreatedAt.UTC().Format(time.RFC3339)
	// minPrice 给**数值**（issue #28）：priceRange 是给人看的字符串（"99 ~ 199"），
	// 拿它排序会得到字典序（"199" < "99"）。没有启用变体的商品给 nil，
	// 组件据此把它排到最后，而不是当成 0 元。
	if p.MinPrice != nil {
		item["minPrice"] = *p.MinPrice
	}
	return item
}

// imageURLsAny 图集 URL 数组（[]any 形态）；未配图集但有主图时退化为单元素数组
// （与详情页同口径），避免列表卡出现空图。
func imageURLsAny(p *productmodel.ProductEntity) []any {
	urls := imageURLs(p)
	out := make([]any, 0, len(urls))
	for _, u := range urls {
		out = append(out, u)
	}
	return out
}

// 编译期断言：本模块同时提供商品集合源的解析与元数据两个契约。
var (
	_ core.CollectionResolver       = (*Service)(nil)
	_ core.CollectionSchemaProvider = (*Service)(nil)
	_ core.CollectionSourceProvider = (*Service)(nil)
)
