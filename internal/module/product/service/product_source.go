package productservice

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

// 筛选栏的选项来自**本模块的表**（分类 / 品牌 / 标签 / 属性组），构建期一次取回，
// 展示名按构建语言取译文（与集合项字段同一套 helper，不另写一份取词逻辑）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/internal/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/money"
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

// 编译期断言：集合源筛选选项能力（issue #27）。
var _ core.CollectionFilterOptionsProvider = (*Service)(nil)

// CollectionFilterOptions 实现 core.CollectionFilterOptionsProvider。
//
// source 由集合源注册表按「注册时各提供方自报的集合源」派发进来：本模块只拥有
// 商品集合源一个，其它源（哪怕也落在本模块的元数据聚合里）不是本实现在答 ——
// 不猜、不报错，返回空选项（与注册表「无该能力/无该源 → 空选项」的规则一致）。
func (s *Service) CollectionFilterOptions(ctx context.Context, source, projectID string) (out core.CollectionFilterOptions, err error) {
	if strings.TrimSpace(source) != productcontract.CollectionSourceProduct {
		return out, nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return out, fmt.Errorf("缺少工程 ID，无法给出筛选选项")
	}
	lang := core.BuildLang(ctx)

	categories, cerr := s.m.ListCategories(ctx, projectID, "")
	if cerr != nil {
		return out, cerr
	}
	for _, row := range categories {
		parent := ""
		if row.ParentID != nil {
			parent = *row.ParentID
		}
		out.Categories = append(out.Categories, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.categoryValues(ctx, lang, row), row.Name), ParentID: parent,
		})
	}

	// 全量取（0, 0）：筛选选项要列出工程里所有品牌 / 标签，不是第一页。
	brands, berr := s.m.ListBrands(ctx, projectID, "", 0, 0)
	if berr != nil {
		return out, berr
	}
	for _, row := range brands {
		out.Brands = append(out.Brands, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.brandValues(ctx, lang, row), row.Name),
		})
	}

	tags, terr := s.m.ListTags(ctx, projectID, "", "", 0, 0)
	if terr != nil {
		return out, terr
	}
	for _, row := range tags {
		out.Tags = append(out.Tags, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.tagValues(ctx, lang, row), row.Name),
		})
	}

	attrs, aerr := s.m.ListAttributesByProject(ctx, projectID)
	if aerr != nil {
		return out, aerr
	}
	for _, row := range attrs {
		// 只有**参与变体**的属性组可筛：不参与变体的属性在商品侧只是一个标记，
		// 变体的 option_values 里没有它，筛了必然恒空 —— 与其给一个永远筛不出东西的选项，
		// 不如不显示（参考站上「价格」这类维度也不是靠属性表达的）。
		if !row.IsVariation {
			continue
		}
		labels := s.attributeValueTranslations(ctx, lang, row)
		attr := core.CollectionFilterAttributeGroup{
			Key: row.Key, Name: filterOptionName(s.attributeValues(ctx, lang, row), row.Name),
		}
		for _, value := range decodeAttributeValueOptions(row.Values) {
			name := strings.TrimSpace(labels[value.Key])
			if name == "" {
				name = value.Label
			}
			attr.Values = append(attr.Values, core.CollectionFilterChoice{
				Key: value.Key, Name: name,
			})
		}
		if len(attr.Values) > 0 {
			out.Attributes = append(out.Attributes, attr)
		}
	}
	return out, nil
}

// attributeValueOption 属性值定义的最小结构（只要渲染筛选项需要的两个字段）。
type attributeValueOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// decodeAttributeValueOptions 属性值 JSONB → 选项列表（形状不对返回空，不 panic）。
func decodeAttributeValueOptions(raw json.RawMessage) []attributeValueOption {
	if len(raw) == 0 {
		return nil
	}
	var rows []attributeValueOption
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]attributeValueOption, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

// filterOptionName 取译文里的展示名，无译文逐字节回退原文（与集合项字段同一口径）。
func filterOptionName(values map[string]string, fallback string) string {
	if name := strings.TrimSpace(values["name"]); name != "" {
		return name
	}
	return fallback
}

// productFieldValues 计算商品白名单字段的展示值（纯函数，便于单测）。
//
// 价格全部落在变体上（商品主体不存价格，issue #5 已定语义），这里给出的是
// 由变体派生的只读值：price 取最低变体价，priceRange 在多价时输出 "最低 ~ 最高"。
//
// options / variants 同理是派生值（issue #8）：属性组 → 规格维度，变体行 → 规格组合；
// 维度显示名与值展示文本取 product_attribute 的译文（验收 5：只翻展示文本，key 原样保留）。
// 商品自身可翻译字段的取词由调用方（ResolverFor）用 translateFields 完成 ——
// 本函数保持无副作用的纯计算，便于单测与集合源复用。
func productFieldValues(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) map[string]string {
	out := map[string]string{
		"name":        p.Name,
		"subtitle":    p.Subtitle,
		"description": descriptionHTML(p.Description),
		// SEO 字段原样透出（不套 descriptionHTML）：它们是给 <title> 与 meta description
		// 用的纯文本，套上富文本包装反而要在消费侧再去标签
		//（presentation 侧仍会做一次归一，兜住历史数据里混进的标记）。
		// SEO 标题与商品名**就是同一个东西**（2026-09-30 合并）：商品名即网页标题。
		// 原来分开两个字段，是从 WordPress 那套抄来的形状 —— 它分开是因为原生没有独立标题概念，
		// 而这里「商品叫什么」本来就是我们要的 <title>，多一个框只会逼编辑者抄一遍。
		// seoTitle 这个**键保留**：模板里绑 {{product.seoTitle}} 的地方不用改，值就是商品名。
		// 副标题同理充当 meta description（按他说的「网页悬浮显示的那行」）。
		//
		// products.seo_title / seo_description 两列**保留但不再暴露**：里面可能有编辑者
		// 认真写过的历史值，删列会丢数据。它们不再是任何 UI 的来源。
		"seoTitle":       p.Name,
		"seoDescription": p.Subtitle,
		"imageAlts":      imageAltsJSON(p, loc),
		"slug":           p.Slug,
		"unit":           p.Unit,
		"images":         imagesJSON(p),
		"defaultImage":   mediaURL(p.DefaultImage),
		"options":        optionsJSON(p, variants, attrs, loc),
		"variants":       variantsJSON(variants),
		"related":        relatedJSON(p, loc),
		"tags":           tagsJSON(p, loc),
	}
	if len(variants) > 0 {
		out["sku"] = variants[0].SKUCode
		minPrice, maxPrice := variants[0].Price, variants[0].Price
		var compare float64
		hasCompare := false
		for _, v := range variants {
			if v.Price < minPrice {
				minPrice = v.Price
			}
			if v.Price > maxPrice {
				maxPrice = v.Price
			}
			if v.ComparePrice != nil && (!hasCompare || *v.ComparePrice > compare) {
				compare, hasCompare = *v.ComparePrice, true
			}
		}
		out["price"] = formatPrice(minPrice)
		out["minPrice"] = formatPrice(minPrice)
		out["maxPrice"] = formatPrice(maxPrice)
		if minPrice == maxPrice {
			out["priceRange"] = formatPrice(minPrice)
		} else {
			out["priceRange"] = formatPrice(minPrice) + " ~ " + formatPrice(maxPrice)
		}
		if hasCompare {
			out["comparePrice"] = formatPrice(compare)
		}
	}
	// 评分（issue #30）：由评分明细投影算出（明细已 Preload），商品表上没有评分列。
	//
	// **一条评分都没有时不写这两个键**：与「评分 0」严格区分 —— 集合组件据此把它排到最后，
	// 而不是当成 0 分。rating 给两位小数的字符串（展示用），数值另见集合项的 ratingValue。
	if avg, count, ok := p.RatingSummaryOf(); ok {
		out["rating"] = strconv.FormatFloat(avg, 'f', 2, 64)
		out["ratingCount"] = strconv.Itoa(count)
	}
	// imageAlt：图集首张的 alt 单值槽位（主图 alt 直接绑它；无 alt 时组件用商品名兜底）。
	if alts := decodeStrings(json.RawMessage(out["imageAlts"])); len(alts) > 0 {
		out["imageAlt"] = alts[0]
	}
	return out
}

// tagsJSON 商品标签的展示名数组（issue #22：商品卡要显示标签）。
//
// 形状是**字符串数组**而不是对象数组：卡片只需要展示名（slug 进筛选参数，
// 不进卡片；要链到标签页的完整结构走 product.related）。展示名取译文（标签名
// 可翻译，issue #12），无引用输出空数组 —— 组件据此不输出空壳节点。
func tagsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	names := make([]string, 0, 4)
	if p == nil {
		return "[]"
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		name := loc.name(productcontract.EntityTypeTag, id, "name", row.Name)
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// —— 关联实体（分类 / 品牌 / 标签 / 属性）的译文视图 ——

// relatedTexts 商品引用的分类 / 品牌 / 标签 / 属性组的译文视图。
//
// entities 存实体本身（slug / key 等稳定标识原样读取，永不进译文），
// fields 存译文（键 "实体类型" + NUL + id → 字段名 → 文本）。
type relatedTexts struct {
	categories map[string]*productmodel.ProductCategoryEntity
	brands     map[string]*productmodel.ProductBrandEntity
	tags       map[string]*productmodel.ProductTagEntity
	attributes map[string]*productmodel.ProductAttributeEntity
	fields     map[string]map[string]string
	// attrLabels 属性 id → 属性值 key → 展示文本译文。
	attrLabels map[string]map[string]string
	// imageAlts 商品图集 alt 的译文（原文 → 译文）。
	imageAlts map[string]string
}

// relatedFieldKey 译文映射键（实体类型 + NUL + id；NUL 不会出现在两者里，无歧义）。
func relatedFieldKey(entityType, id string) string {
	return entityType + "\x00" + id
}

// emptyRelated 空译文视图（无语言 / 无引用时使用，方法全部回退原文）。
func emptyRelated() *relatedTexts {
	return &relatedTexts{
		categories: map[string]*productmodel.ProductCategoryEntity{},
		brands:     map[string]*productmodel.ProductBrandEntity{},
		tags:       map[string]*productmodel.ProductTagEntity{},
		attributes: map[string]*productmodel.ProductAttributeEntity{},
		fields:     map[string]map[string]string{},
		attrLabels: map[string]map[string]string{},
		imageAlts:  map[string]string{},
	}
}

// name 取某实体某字段的译文（无译文 / 无该实体时回退 fallback）。
func (r *relatedTexts) name(entityType, id, field, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.fields[relatedFieldKey(entityType, id)][field]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// label 取属性值展示文本的译文（无译文回退原文；key 永远不变）。
func (r *relatedTexts) label(attrID, valueKey, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.attrLabels[attrID][valueKey]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// relatedJSON 商品挂载关系的展示文本（分类 / 品牌 / 标签）。
//
// 形状：{"categories":[{"slug":…,"name":…}],"brand":{…},"tags":[…]}
// slug 原样保留（它进 URL 与筛选参数），只有展示名取译文；空引用输出空数组。
func relatedJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	type named struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	out := struct {
		Categories []named `json:"categories"`
		Brand      *named  `json:"brand,omitempty"`
		Tags       []named `json:"tags"`
	}{Categories: []named{}, Tags: []named{}}
	if p == nil {
		return "{}"
	}
	for _, id := range decodeStrings(p.CategoryIDs) {
		row, ok := loc.categories[id]
		if !ok {
			continue
		}
		out.Categories = append(out.Categories, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeCategory, id, "name", row.Name),
		})
	}
	if p.BrandID != nil {
		if row, ok := loc.brands[*p.BrandID]; ok {
			out.Brand = &named{Slug: row.Slug, Name: loc.name(productcontract.EntityTypeBrand, row.ID, "name", row.Name)}
		}
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		out.Tags = append(out.Tags, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeTag, id, "name", row.Name),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// imageAltsJSON 图集 alt 文本数组（与 images 逐位对应，元素可为空串）。
//
// 原文来自 products.images_alt（issue #12，迁移 094）；命中译文用译文，
// 未命中逐字节回退原文（不改写入库）。
func imageAltsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	alts := decodeStrings(p.ImageAlts)
	out := make([]string, len(alts))
	for i, alt := range alts {
		out[i] = alt
		if loc == nil {
			continue
		}
		if target, ok := loc.imageAlts[alt]; ok && strings.TrimSpace(target) != "" {
			out[i] = target
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// optionsJSON 规格维度：商品引用且「参与变体」的属性组 × 其启用值。
//
// 顺序 = 商品 attribute_ids 的引用顺序（作者在后台勾选的顺序），保证同一份数据
// 每次构建输出同样的字节（不变量 5）。非参与变体 / 无启用值的组不进规格维度。
// 维度名取 product_attribute.name 的译文，值展示文本取 product_attribute.values
// 的译文；值 key 原样保留（验收 5：筛选参数与 URL 段保持不变）。
// variantOptionKeys 变体实际用到的规格维度 key 集合。
//
// 变体的 option_values 是 {"<属性 key>": "<值 key>"} 形状（空对象表示无规格）。
// 解析失败按「没用到」处理：宁可少出一个选择器，也不要因为一条脏数据让整页报错。
func variantOptionKeys(variants []*productmodel.VariantEntity) map[string]bool {
	out := map[string]bool{}
	for _, v := range variants {
		if v == nil || len(v.OptionValues) == 0 {
			continue
		}
		var values map[string]string
		if err := json.Unmarshal(v.OptionValues, &values); err != nil {
			continue
		}
		for key := range values {
			if key != "" {
				out[key] = true
			}
		}
	}
	return out
}

// optionsJSON 商品规格维度（选择器用）。
//
// 一个属性要成为**规格维度**，必须同时满足两条：
//  1. 属性自身标记为变化属性（is_variation）；
//  2. **至少一个变体真的用了它**（option_values 里有这个 key）。
//
// 第 2 条是后补的，它挡住的是一类真实故障：单 SKU 商品也带属性引用
// （那些属性是拿来做**筛选**的，不是拿来选规格的），只看 is_variation 会把它们
// 全当成规格维度 → 组件认为「这件商品有规格，但没有任何可买的组合」→
// 加购按钮渲染成「暂无可购买的规格」。而本系统没有「简单商品」这个概念，
// 单 SKU 就是只有一个变体、没有规格维度的可变商品，它的加购必须照常可用。
func optionsJSON(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) string {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	used := variantOptionKeys(variants)
	groups := []optionGroupJSON{}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		// 没有任何变体用到 → 它不是这个商品的规格维度（见函数注释第 2 条）。
		if !used[a.Key] {
			continue
		}
		values := []optionValueJSON{}
		for _, v := range normalizeValuesFromRaw(a.Values) {
			if !v.Enabled {
				continue
			}
			values = append(values, optionValueJSON{Key: v.Key, Label: loc.label(id, v.Key, v.Label)})
		}
		if len(values) == 0 {
			continue
		}
		groups = append(groups, optionGroupJSON{
			Key:    a.Key,
			Name:   loc.name(productcontract.EntityTypeAttribute, id, "name", a.Name),
			Values: values,
		})
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// variantsJSON 规格组合行：每个变体的编码 / 价格 / 组合（属性组 key → 属性值 key）。
//
// 价格在这里就带上货币无关的纯数字（货币符号由组件按 Props 前缀），
// 与 price / priceRange 的取值口径一致。组合里的 key 是稳定标识，永不进译文。
func variantsJSON(variants []*productmodel.VariantEntity) string {
	rows := []variantJSON{}
	for _, v := range variants {
		row := variantJSON{
			ID:  v.ID,
			SKU: v.SKUCode, Price: formatPrice(v.Price), Image: mediaURL(v.Image),
			Enabled: v.Enabled, Options: map[string]string{},
		}
		if v.ComparePrice != nil {
			row.ComparePrice = formatPrice(*v.ComparePrice)
		}
		if len(v.OptionValues) > 0 {
			_ = json.Unmarshal(v.OptionValues, &row.Options)
		}
		if row.Options == nil {
			row.Options = map[string]string{}
		}
		rows = append(rows, row)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// valueLabelsJSON 属性值展示文本数组（按组内定义顺序，含未启用值）。
func valueLabelsJSON(values []productdto.AttributeValueResp) string {
	labels := make([]string, 0, len(values))
	for _, v := range values {
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

// stringListJSON 字符串数组 → JSON（nil 当空数组；编码失败退化为空数组，不让构建失败）。
func stringListJSON(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// formatPrice 数值 → 展示字符串：整数不带小数尾巴，其余按最短表示。
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与 dashboard 的 formatAmount
// 逐字节重复 —— 两处都是展示口径）。保留本名字是因为在构建期字段投影里「价格」
// 比「金额格式化」更贴调用点语义，函数体只是转发。
func formatPrice(v float64) string {
	return money.FormatYuan(v)
}

// imagesJSON 图集 JSON 数组字符串（core.gallery 等组件按 JSON 数组解析绑定值）；
// 商品未配图集但有主图时退化为单元素数组，避免详情页出现空图区。
func imagesJSON(p *productmodel.ProductEntity) string {
	urls := imageURLs(p)
	b, err := json.Marshal(urls)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// imageURLs 图集 URL：未配图集但有主图时退化为单元素数组（与 core.product 同口径）。
//
// 出口归一成完整链接：产物里的图片地址直接给访客用，相对路径在「站点与 CMS 不同域」
// 的部署下会指回 CMS 自己（cdn / 独立域名场景）。存量行入库时是相对路径，所以这一层
// 必须归一，不能只靠写入口。
func imageURLs(p *productmodel.ProductEntity) []string {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
	return mediaURLs(urls)
}

// descriptionHTML 商品描述（jsonb）→ HTML 片段。
//
// 约定两种形态：{"html": "..."}（富文本）与 JSON 字符串（纯文本）；
// 其余（{} 或未知结构）返回空串，由组件侧决定是否输出占位。
func descriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.HTML != "" {
		return obj.HTML
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// 编译期断言：适配器实现 core.EntityFieldSource，解析器实现 core.ContentResolver。
var (
	_ core.EntityFieldSource = (*entityFieldSource)(nil)
	_ core.ContentResolver   = (*entityResolver)(nil)
)

// entityFieldSource 商品域实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

func (e *entityFieldSource) EntityType() string { return e.entityType }

// FieldWhitelist 实现 core.EntityFieldSource（白名单仍取自本模块契约的唯一来源）。
func (e *entityFieldSource) FieldWhitelist() []string {
	return productcontract.FieldWhitelist(e.entityType)
}

// ResolverFor 实现 core.EntityFieldSource。
func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

// RegisterEntityTypes 把本模块的实体类型注册进注册表（装配期调用）。
//
// 注册表为 nil 视为装配缺陷（fail-closed）：静默跳过会让构建层到运行期才发现
// 「类型非法」，比装配期直接报错更难排查（与 content 模块同一口径）。
func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range productcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

// ResolverFor 返回绑定单个实体的字段解析器（构建期注入）。
//
// 一次性把白名单字段值算好（含价格区间等派生值、分类/品牌/标签/属性值的译文）
// 并挂上译文，构建期只读内存；不存在的实体返回 ErrNotFound，类型不符返回 ErrInvalidType。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !productcontract.IsValidType(entityType) {
		return nil, errors.New(productenums.ErrInvalidType)
	}
	lang := core.BuildLang(ctx)
	// 工程作用域（审计 DB-009 第四批收口）：**必须**从构建上下文取，取不到就显式报错。
	//
	// 这条链曾经是「显式例外」：本方法在 builder.Compile **之前**被 presentation 的
	// renderHTML 调用，而 core.WithBuildProjectID 原先只在 Compile 内部补，于是那时 ctx 里
	// 没有工程 id，只能走无作用域读。presentation 侧现在已在调用本方法之前补上
	// （presentation/service/presentation_render.go 的 buildCtx），例外条件不再成立 ——
	// 换非超级角色后继续裸读会 fail closed（0 行）⇒ 实体字段源解析失败 ⇒ 产物里对应区块
	// 静默缺失，所以这里改成带作用域读。
	//
	// 缺工程时**报错而不是退回裸读**：退回等于把「调用链漏了注入」伪装成「实体不存在」
	// （mapNotFound 会把它翻成 ErrNotFound），排查成本高得多。
	projectID := strings.TrimSpace(core.BuildProjectID(ctx))
	if projectID == "" {
		return nil, fmt.Errorf("%s: 构建上下文缺少工程 id（core.WithBuildProjectID），无法按工程隔离读取实体字段源",
			productenums.ErrMissingProjectContext)
	}
	switch entityType {
	case productcontract.EntityTypeCategory:
		var row *productmodel.ProductCategoryEntity
		if row, err = s.m.GetCategory(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.categoryValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeBrand:
		var row *productmodel.ProductBrandEntity
		if row, err = s.m.GetBrand(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.brandValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeTag:
		var row *productmodel.ProductTagEntity
		if row, err = s.m.GetTag(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.tagValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeAttribute:
		var row *productmodel.ProductAttributeEntity
		if row, err = s.m.GetAttribute(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.attributeValues(ctx, lang, row)}, nil
	}

	e, gerr := s.m.Get(ctx, entityID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	variants, verr := s.m.ListVariants(ctx, e.ID)
	if verr != nil {
		return nil, verr
	}
	// 读的是 e 自己引用的属性组：作用域用行自己的工程（与入参一致，这里显式用 e.ProjectID
	// 表达「读的是这一行的引用面」）。
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	loc, lerr := s.localizeRelated(ctx, lang, e)
	if lerr != nil {
		return nil, lerr
	}
	values := productFieldValues(e, variants, attrs, loc)
	return &entityResolver{entityType: entityType, values: s.translateFields(ctx, lang, productcontract.EntityTypeProduct, values)}, nil
}

// mapNotFound gorm 未命中 → 模块统一的「不存在」错误（其余原样上抛）。
func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(productenums.ErrNotFound)
	}
	return err
}

// —— 各实体类型的取值 ——

// ResolveString 按字段白名单解析实体字段值。
//
// field 形如 "product.name"（entityType.field，与 heading 等组件的绑定格式一致）；
// 前缀类型不符或字段不在白名单内一律报错 —— 白名单是唯一来源，不做静默回退。
func (r *entityResolver) ResolveString(field string) (string, error) {
	entityType, name, ok := splitEntityField(field)
	if !ok {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", productenums.ErrInvalidField, field)
	}
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !productcontract.IsValidField(entityType, name) {
		return "", fmt.Errorf("%s: %q", productenums.ErrInvalidField, name)
	}
	return r.values[name], nil
}

// splitEntityField 拆 "entity.field"（两段，字段名不允许再带点）。
func splitEntityField(field string) (entityType, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(field), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// —— 商品字段值 ——

// seoAliasFields 字段合并后的别名关系：别名 → 源字段（2026-09-30）。
//
// 商品 SEO 标题 / 描述与商品名 / 副标题、分类与品牌的 SEO 字段与名称 / 描述就是同一个东西，
// 它们已从可翻译字段集合（contract 的 translatableFields）里去掉，值是**源字段翻译后的值** ——
// 所以同步必须发生在取词之后（见 translateFieldsBatch 末尾的 applySEOAliases）：
// 先同步再取词的话，英文站点上 meta 标题会拿着中文原文去查 seoTitle 语境。
var seoAliasFields = map[string]map[string]string{
	productcontract.EntityTypeProduct:  {"seoTitle": "name", "seoDescription": "subtitle"},
	productcontract.EntityTypeCategory: {"seoTitle": "name", "seoDescription": "description"},
	productcontract.EntityTypeBrand:    {"seoTitle": "name", "seoDescription": "description"},
}

// categoryValues 分类的可绑定字段值（作者文本按 lang 取译文）。
func (s *Service) categoryValues(ctx context.Context, lang string, e *productmodel.ProductCategoryEntity) map[string]string {
	values := map[string]string{
		"name":        e.Name,
		"slug":        e.Slug,
		"description": e.Description,
		"image":       e.Image,
		// SEO 标题 / 描述与「分类名 / 分类描述」**就是同一个东西**（2026-09-30 合并）：
		// 分类名即 <title>、分类描述即 meta description（富文本由 presentation 的
		// seoDescriptionText 去标签，不在这里另做一份）。键保留 —— 模板里绑
		// {{category.seoTitle}} 的地方不用改，值就是分类名。
		// product_categories.seo_title / seo_description 两列保留但不再暴露：
		// 里面可能有编辑者认真写过的历史值，删列会丢数据。
		"seoTitle":       e.Name,
		"seoDescription": e.Description,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeCategory, values)
}

// brandValues 品牌的可绑定字段值。
func (s *Service) brandValues(ctx context.Context, lang string, e *productmodel.ProductBrandEntity) map[string]string {
	values := map[string]string{
		"name":        e.Name,
		"slug":        e.Slug,
		"logo":        e.Logo,
		"description": e.Description,
		// 同分类：品牌名即 <title>、品牌描述即 meta description（2026-09-30 合并）。
		// 键保留 —— 模板里绑 {{brand.seoTitle}} 的地方不用改，值就是品牌名。
		// product_brands.seo_title / seo_description 两列保留但不再暴露。
		"seoTitle":       e.Name,
		"seoDescription": e.Description,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeBrand, values)
}

// tagValues 标签的可绑定字段值。
func (s *Service) tagValues(ctx context.Context, lang string, e *productmodel.ProductTagEntity) map[string]string {
	values := map[string]string{"name": e.Name, "slug": e.Slug}
	return s.translateFields(ctx, lang, productcontract.EntityTypeTag, values)
}

// attributeValues 属性组的可绑定字段值。
//
// values 字段是属性值展示文本的 JSON 数组（按组内定义顺序，含未启用值）；
// 逐元素取 product_attribute.values 的译文（与商品规格选择器同源），
// 值 key 不进数组 —— 它进筛选参数与规格组合，永不翻译（验收 5）。
func (s *Service) attributeValues(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	values := map[string]string{
		"name":   e.Name,
		"key":    e.Key,
		"values": valueLabelsJSON(normalizeValuesFromRaw(e.Values)),
	}
	values = s.translateFields(ctx, lang, productcontract.EntityTypeAttribute, values)
	values["values"] = s.translatedValueLabelsJSON(ctx, lang, e)
	return values
}

// translatedValueLabelsJSON 属性值展示文本数组（逐元素取译文，无译文回退原文）。
func (s *Service) translatedValueLabelsJSON(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) string {
	if e == nil {
		return "[]"
	}
	items := normalizeValuesFromRaw(e.Values)
	translated := s.attributeValueTranslations(ctx, lang, e)
	labels := make([]string, 0, len(items))
	for _, v := range items {
		if target, ok := translated[v.Key]; ok {
			labels = append(labels, target)
			continue
		}
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

// translateFields 把某实体类型的标量可翻译字段整体替换为译文（就地改写 values）。
//
// 语境按 productcontract.FieldContext 一处拼装；跳过规则（纯数字/纯符号/空白）
// 与「无译文回退原文」由 pkg/i18n 负责，这里不做第二套判断。
// 数组字段（product_attribute.values）不在此处翻 —— 它逐元素取词，见
// attributeValueTranslations。
func (s *Service) translateFields(ctx context.Context, lang string, entityType string, values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	s.translateFieldsBatch(ctx, lang, entityType, []map[string]string{values})
	return values
}

// translateFieldsBatch 对多组字段值一次性批量取词（PERF-009：集合解析不再逐商品查库）。
func (s *Service) translateFieldsBatch(ctx context.Context, lang, entityType string, batches []map[string]string) {
	if lang == "" || s.contentStore == nil || len(batches) == 0 {
		return
	}
	type workItem struct {
		batchIdx int
		field    string
		context  string
		source   string
	}
	work := make([]workItem, 0, len(batches)*4)
	hashes := make([]string, 0, len(batches)*4)
	seenHash := map[string]bool{}
	for i, values := range batches {
		if len(values) == 0 {
			continue
		}
		for _, f := range productcontract.TranslatableFields(entityType) {
			if f == "values" {
				continue
			}
			v, ok := values[f]
			if !ok || !i18n.ShouldTranslateContent(v) {
				continue
			}
			h := i18n.ContentHash(v)
			if !seenHash[h] {
				seenHash[h] = true
				hashes = append(hashes, h)
			}
			work = append(work, workItem{
				batchIdx: i,
				field:    f,
				context:  productcontract.FieldContext(entityType, f),
				source:   v,
			})
		}
	}
	if len(hashes) == 0 {
		// 没有可翻译文本（值全是数字 / 符号）：源字段没被改写，但别名仍同步一次 ——
		// 「别名 = 源字段」这条在任何路径上都得成立，别把不变量寄托在「构造时已经设好」。
		applySEOAliases(entityType, batches)
		return
	}
	// 工程作用域（审计 I18N-009）：工程 id 取自构建上下文（core.WithBuildProjectID
	// 由 builder.Compile 注入，与 BuildLang 同一约定；非构建调用退化为全局视图）。
	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, item := range work {
		batches[item.batchIdx][item.field] = tr.TranslateContent(item.source, item.context)
	}
	applySEOAliases(entityType, batches)
}

// applySEOAliases 把 SEO 别名字段同步为源字段的当前值（已按 lang 取过译文）。
//
// 幂等：源字段不存在时保持原值（旧模板可能只绑了别名，不该在这里被清成空串）。
func applySEOAliases(entityType string, batches []map[string]string) {
	aliases := seoAliasFields[entityType]
	if len(aliases) == 0 {
		return
	}
	for _, values := range batches {
		for alias, source := range aliases {
			if v, ok := values[source]; ok {
				values[alias] = v
			}
		}
	}
}

// translateTexts 批量取一组文本在指定语境下的译文（同一语境，逐元素）。
//
// 返回 map[原文]译文；无译文 / 跳过规则命中的原文不在返回值里（调用方回退原文）。
// 一次批量 SQL（取词器的唯一查询形态），不逐条查库。
func (s *Service) translateTexts(ctx context.Context, lang, contextName string, sources []string) map[string]string {
	out := map[string]string{}
	if lang == "" || s.contentStore == nil || contextName == "" || len(sources) == 0 {
		return out
	}
	hashes := make([]string, 0, len(sources))
	uniq := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, src := range sources {
		if !i18n.ShouldTranslateContent(src) || seen[src] {
			continue
		}
		seen[src] = true
		uniq = append(uniq, src)
		hashes = append(hashes, i18n.ContentHash(src))
	}
	if len(hashes) == 0 {
		return out
	}
	// 工程作用域（审计 I18N-009）：工程 id 取自构建上下文（core.WithBuildProjectID
	// 由 builder.Compile 注入，与 BuildLang 同一约定；非构建调用退化为全局视图）。
	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, src := range uniq {
		if target := tr.TranslateContent(src, contextName); target != src {
			out[src] = target
		}
	}
	return out
}

// attributeValueTranslations 属性组内每个属性值展示文本的译文（值 key → 译文）。
//
// 语境固定 product_attribute.values：同一属性组内「同一段文本 → 同一行译文」，
// 值 key 永不进译文（验收 5：筛选参数与 URL 段保持不变）。
func (s *Service) attributeValueTranslations(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	out := map[string]string{}
	if e == nil {
		return out
	}
	values := normalizeValuesFromRaw(e.Values)
	sources := make([]string, 0, len(values))
	for _, v := range values {
		sources = append(sources, v.Label)
	}
	translated := s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeAttribute, "values"), sources)
	if len(translated) == 0 {
		return out
	}
	for _, v := range values {
		if target, ok := translated[v.Label]; ok {
			out[v.Key] = target
		}
	}
	return out
}

// imageAltTranslations 商品图集 alt 文本的译文（原文 → 译文）。
//
// 语境固定 product.imageAlts（与工作台写入一致），逐元素取词；URL 永不翻译。
func (s *Service) imageAltTranslations(ctx context.Context, lang string, p *productmodel.ProductEntity) map[string]string {
	if p == nil {
		return map[string]string{}
	}
	alts := decodeStrings(p.ImageAlts)
	if len(alts) == 0 {
		return map[string]string{}
	}
	return s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeProduct, "imageAlts"), alts)
}

// entityResolver 绑定单个实体的字段解析器（值已预计算并翻译）。
type entityResolver struct {
	entityType string
	values     map[string]string
}

// localizeRelated 加载商品引用的分类 / 品牌 / 标签 / 属性组并按构建语言取译文。
//
// 每个实体类型一次批量取词（不逐条查库）；语言为空时直接返回空视图（零查库）。
func (s *Service) localizeRelated(ctx context.Context, lang string, e *productmodel.ProductEntity) (loc *relatedTexts, err error) {
	loc = emptyRelated()
	if e == nil {
		return loc, nil
	}
	if lang == "" || s.contentStore == nil {
		return loc, nil
	}

	if ids := decodeStrings(e.CategoryIDs); len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids, e.ProjectID)
		if cerr != nil {
			return nil, cerr
		}
		for _, row := range rows {
			loc.categories[row.ID] = row
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {
			loc.brands[row.ID] = row
		}
	}
	if ids := decodeStrings(e.TagIDs); len(ids) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, ids, e.ProjectID)
		if terr != nil {
			return nil, terr
		}
		for _, row := range rows {
			loc.tags[row.ID] = row
		}
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	return s.fillRelatedTexts(ctx, lang, e, loc, attrs), nil
}

// localizeRelatedFrom 用已预载的分类 / 品牌 / 标签索引取译文（集合源列表用，零 N+1）。
func (s *Service) localizeRelatedFrom(ctx context.Context, lang string, e *productmodel.ProductEntity,
	categories map[string]*productmodel.ProductCategoryEntity,
	brands map[string]*productmodel.ProductBrandEntity,
	tags map[string]*productmodel.ProductTagEntity,
	attrs []*productmodel.ProductAttributeEntity) (loc *relatedTexts, err error) {
	loc = emptyRelated()
	if e == nil {
		return loc, nil
	}
	for id, row := range categories {
		loc.categories[id] = row
	}
	for id, row := range brands {
		loc.brands[id] = row
	}
	for id, row := range tags {
		loc.tags[id] = row
	}
	return s.fillRelatedTexts(ctx, lang, e, loc, attrs), nil
}

// fillRelatedTexts 按 lang 为已加载的关联实体取译文（每个实体类型一次批量取词）。
func (s *Service) fillRelatedTexts(ctx context.Context, lang string, e *productmodel.ProductEntity, loc *relatedTexts, attrs []*productmodel.ProductAttributeEntity) *relatedTexts {
	if lang == "" || s.contentStore == nil {
		return loc
	}
	for id, row := range loc.categories {
		loc.fields[relatedFieldKey(productcontract.EntityTypeCategory, id)] = s.categoryValues(ctx, lang, row)
	}
	for id, row := range loc.brands {
		loc.fields[relatedFieldKey(productcontract.EntityTypeBrand, id)] = s.brandValues(ctx, lang, row)
	}
	for id, row := range loc.tags {
		loc.fields[relatedFieldKey(productcontract.EntityTypeTag, id)] = s.tagValues(ctx, lang, row)
	}
	for _, row := range attrs {
		loc.attributes[row.ID] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeAttribute, row.ID)] = s.attributeValues(ctx, lang, row)
		loc.attrLabels[row.ID] = s.attributeValueTranslations(ctx, lang, row)
	}
	if e != nil {
		loc.imageAlts = s.imageAltTranslations(ctx, lang, e)
	}
	return loc
}

// —— JSON 派生值 ——

// optionGroupJSON 规格维度（构建期输出的 JSON 结构，与 core.product 的解析约定一致）。
type optionGroupJSON struct {
	Key    string            `json:"key"`
	Name   string            `json:"name"`
	Values []optionValueJSON `json:"values"`
}

// optionValueJSON 规格维度下的一个可选值（key 是稳定标识，不进译文）。
type optionValueJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// variantJSON 规格组合行（构建期输出的 JSON 结构）。
//
// ID 是变体 id（issue #24）：产物里要用它构造「实时可用量片段」的请求参数。
type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice,omitempty"`
	Image        string            `json:"image,omitempty"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}
