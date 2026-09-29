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

	"go_wp/internal/seo"

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

// 编译期断言：商品集合源支持按页取数（审计 PERF-019）。
// 没有这行的话，ResolveCollectionPage 被改名或改签名时不会有任何提示 ——
// 注册表只是「探测不到这个能力」然后静默退回取一批再截断。
var _ core.CollectionPager = (*Service)(nil)

// ResolveCollection 实现 core.CollectionResolver：解析 "content:product" 集合源。
//
// filter 为白名单等值过滤（当前只有 status）：白名单外的维度直接报错，
// 不接受任意过滤表达式（不变量 4）。
// 工程范围取自构建上下文（core.BuildProjectID，由 builder.Compile 注入）：
// 商品是分工程的数据，缺工程 ID 时不限工程（后台预览等无站点上下文的场景）。
// resolveCollection 两个入口的共用实现（审计 PERF-019）。
//
// total 的语义：满足过滤条件的**总数**，不是本页条数。取数不满一页时不必再 COUNT ——
// 后面已经没有了，总量就是 offset + 本页条数。
func (s *Service) resolveCollection(ctx context.Context, source string, filter map[string]string, offset, limit int) (items []map[string]any, total int, err error) {
	if source != productcontract.CollectionSourceProduct {
		return nil, 0, fmt.Errorf("%s: %q", productenums.ErrCollectionSourceInvalid, source)
	}
	f, err := parseCollectionFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	// 工程范围取自构建上下文（后台预览等无站点上下文的场景为空 = 不限工程）。
	f.ProjectID = core.BuildProjectID(ctx)
	rows, err := s.m.ListForCollection(ctx, f, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items = make([]map[string]any, 0, len(rows))
	if len(rows) == 0 {
		// 本页空：调用方不会凭空跳到第 N 页，所以 offset 之前必然已有内容，总量就是 offset ——
		// 与「不满一页即到底」同一个判断，省掉一次 COUNT。
		return items, offset, nil
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
		return nil, 0, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	attrs, err := s.m.ListAttributesByIDs(ctx, attrIDs, f.ProjectID)
	if err != nil {
		return nil, 0, err
	}
	// 分类 / 品牌 / 标签一次取好（issue #12：展示名要取译文，逐个商品查会变成 N 次）。
	categoryIndex, brandIndex, tagIndex, ierr := s.taxonomyIndex(ctx, f.ProjectID, rows)
	if ierr != nil {
		return nil, 0, ierr
	}
	valueBatches := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		// 与实体绑定同源：同一份派生值 + 同一份译文替换（语境 实体类型.<字段名>）。
		loc, lerr := s.localizeRelatedFrom(ctx, lang, r, categoryIndex, brandIndex, tagIndex, attrs)
		if lerr != nil {
			return nil, 0, lerr
		}
		valueBatches = append(valueBatches, productFieldValues(r, byProduct[r.ID], attrs, loc))
	}
	s.translateFieldsBatch(ctx, lang, productcontract.EntityTypeProduct, valueBatches)
	var publishedPaths map[string]string
	if s.publishedLocator != nil && f.ProjectID != "" && len(ids) > 0 {
		publishedPaths, err = s.publishedLocator.PublishedEntityPaths(ctx, f.ProjectID, productcontract.EntityTypeProduct, lang, ids)
		if err != nil {
			return nil, 0, err
		}
	}
	for i, r := range rows {
		item := collectionItem(r, valueBatches[i])
		if p := publishedPaths[r.ID]; p != "" {
			// 在**注入处**补成绝对地址：组件拿到 item.url 时就是可直接用的地址。
			// 不在渲染侧再拼 —— 那份数据要出集合给任意组件（列表卡、选择器、搜索），
			// 让每个消费方各自补一次必然漏（实测：商品列表卡的图链接与标题链接漏了）。
			item["url"] = seo.AbsoluteSiteURL(p)
		}
		items = append(items, item)
	}
	// 取数不满一页说明已经到底：总量就是 offset + 本页条数，不必再 COUNT 一次。
	// 满页时才真去数 —— 列表页每翻一页都走这里，省下的是每次翻页一次聚合查询。
	if limit > 0 && len(rows) < limit {
		return items, offset + len(items), nil
	}
	n, cerr := s.m.CountForCollection(ctx, f)
	if cerr != nil {
		return nil, 0, cerr
	}
	return items, int(n), nil
}

// ResolveCollection 实现 core.CollectionResolver：按集合源查商品列表（构建期取第一屏）。
func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	items, _, err = s.resolveCollection(ctx, source, filter, 0, collectionItemLimit)
	return items, err
}

// ResolveCollectionPage 实现可选能力 core.CollectionPager（审计 PERF-019）：按页取数并给出总量。
//
// 构建期不需要它（只取第一屏），所以是可选能力而不是往 CollectionResolver 上加参数 ——
// 那样全部实现与所有测试 fake 都要跟着改（见 source.CollectionPager 的注释）。
func (s *Service) ResolveCollectionPage(ctx context.Context, source string, q core.CollectionQuery) (core.CollectionPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = collectionItemLimit
	}
	items, total, err := s.resolveCollection(ctx, source, q.Filter, q.Offset, limit)
	if err != nil {
		return core.CollectionPage{}, err
	}
	return core.CollectionPage{Items: items, Total: total}, nil
}

// taxonomyIndex 一次取回该批商品引用的分类 / 品牌 / 标签（列表页专用，零 N+1）。
//
// projectID 由调用方给出（集合源的过滤条件）：三张表都在迁移 215 名单里，
// 缺作用域时换非超级角色后分类 / 品牌 / 标签整批读空 —— 列表卡的展示名会全部消失
// （审计 db-03 §2.5）。这里不自己从 rows 推断工程：作用域必须是**取数时用的那个**，
// rows 是它的结果，从结果反推会让「读错了工程」这件事看起来仍然成立。
func (s *Service) taxonomyIndex(ctx context.Context, projectID string, rows []*productmodel.ProductEntity) (categories map[string]*productmodel.ProductCategoryEntity, brands map[string]*productmodel.ProductBrandEntity, tags map[string]*productmodel.ProductTagEntity, err error) {
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
		categoryRows, cerr := s.m.ListCategoriesByIDs(ctx, categoryIDs, projectID)
		if cerr != nil {
			return nil, nil, nil, cerr
		}
		for _, row := range categoryRows {
			categories[row.ID] = row
		}
	}
	if len(brandIDs) > 0 {
		brandRows, berr := s.m.ListBrandsByIDs(ctx, brandIDs, projectID)
		if berr != nil {
			return nil, nil, nil, berr
		}
		for _, row := range brandRows {
			brands[row.ID] = row
		}
	}
	if len(tagIDs) > 0 {
		tagRows, terr := s.m.ListTagsByIDs(ctx, tagIDs, projectID)
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
		Source:   productcontract.CollectionSourceProduct,
		Label:    productcontract.CollectionLabel,
		LabelKey: productcontract.CollectionLabelKey,
		Fields:   productcontract.FieldWhitelist(productcontract.EntityTypeProduct),
		Filters:  productcontract.CollectionFilters(),
		OrderBy:  productcontract.CollectionOrderBy(),
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
			// 属性维度的值也是**逗号多值**（组内 OR）：一个属性组勾多个值取并集，
			// 组与组之间仍是 AND —— 与分类 / 品牌 / 标签的多值同一套语义。
			values := splitCSV(v)
			for _, value := range values {
				if !optionKeyRe.MatchString(value) {
					return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
				}
			}
			if !optionKeyRe.MatchString(attrKey) || len(values) == 0 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			if f.Options == nil {
				f.Options = map[string][]string{}
			}
			f.Options[attrKey] = values
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
		case productcontract.CollectionFilterCategoryIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.CategoryIDs = ids
		case productcontract.CollectionFilterBrandIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.BrandIDs = ids
		case productcontract.CollectionFilterTagIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.TagIDs = ids
		case productcontract.CollectionFilterCategoryMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.CategoryAll = all
		case productcontract.CollectionFilterBrandMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.BrandAll = all
		case productcontract.CollectionFilterTagMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.TagAll = all
		case productcontract.CollectionFilterMinRating:
			// 最低评分（issue #29）：0~5 的数值，越界 / 非数字一律报错。
			rating, rerr := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if rerr != nil || rating < 0 || rating > 5 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.MinRating = &rating
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
	// 多值优先于单值（**在所有维度都解析完之后**统一裁决，而不是在 case 里就地清空：
	// 参数的遍历顺序是排序后的字典序，就地清空会依赖「categoryId 排在 categoryIds 前」
	// 这种与业务无关的巧合）。
	//
	// 单值维度是「只勾了一个」的退化写法：两者并存时按多值算。叠加成 AND 的话，
	// 用户把单选的配置改成多选之后，旧的那个值会继续把结果卡住 —— 表现为
	// 「我明明只勾了 B，出来的还是 A 的商品」。
	if len(f.CategoryIDs) > 0 {
		f.CategoryID = ""
	}
	if len(f.BrandIDs) > 0 {
		f.BrandID = ""
	}
	if len(f.TagIDs) > 0 {
		f.TagID = ""
	}
	// 区间上下限的相互关系在两维都解析完之后统一校验（它们可能以任意顺序出现）。
	return f, validatePriceRange(f)
}

// parseIDList 解析逗号分隔的 uuid 列表维度。
//
// 任何一个形状不对就报错，不静默丢弃（丢一个 id 会让筛选结果莫名变多，比报错难查得多）。
// 空串由调用方提前 continue 掉了，这里拿到的一定是非空值：全空值（`a,,b` 中间空）已在
// splitCSV 里去空，于是「只有逗号」的输入得到空列表 = 该维度不参与过滤。
func parseIDList(key, raw string) ([]string, error) {
	ids := splitCSV(raw)
	for _, id := range ids {
		if !isUUID(id) {
			return nil, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, key)
		}
	}
	return ids, nil
}

// parseMultiMode 解析多值维度的匹配语义（any / all）。
func parseMultiMode(key, raw string) (all bool, err error) {
	switch strings.TrimSpace(raw) {
	case productcontract.CollectionTagModeAll:
		return true, nil
	case productcontract.CollectionTagModeAny:
		return false, nil
	default:
		return false, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, key)
	}
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
	// ratingValue 给**数值**评分（issue #30）：由评分明细投影算出（明细已 Preload）。
	// 白名单里的 rating 是展示用的字符串，组件按评分排序要数值。
	// 一条评分都没有时不给这个键 —— 组件据它把无评分的排最后（不是「0 分」）。
	if avg, _, ok := p.RatingSummaryOf(); ok {
		item["ratingValue"] = avg
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
