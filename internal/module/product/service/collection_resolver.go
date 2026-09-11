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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	status, err := collectionStatusFilter(filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListForCollection(ctx, core.BuildProjectID(ctx), status, collectionItemLimit, 0)
	if err != nil {
		return nil, err
	}
	items = make([]map[string]any, 0, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
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
	for _, r := range rows {
		// 与实体绑定同源：同一份派生值 + 同一份译文替换（语境 product.<字段名>）。
		values := productFieldValues(r, byProduct[r.ID], attrs)
		s.translateFields(ctx, values)
		items = append(items, collectionItem(r, values))
	}
	return items, nil
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

// collectionStatusFilter 校验并取出 status 维度（其余维度一律拒绝）。
func collectionStatusFilter(filter map[string]string) (status string, err error) {
	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 报错文案里的维度顺序稳定（同输入同报错）
	for _, k := range keys {
		if !productcontract.IsCollectionFilterKey(k) {
			return "", fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
		}
		if k == "status" {
			status = strings.TrimSpace(filter[k])
		}
	}
	return status, nil
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
	item["images"] = imageURLs(p)
	return item
}

// imageURLs 图集 URL 数组；未配图集但有主图时退化为单元素数组（与详情页同口径），
// 避免列表卡出现空图。
func imageURLs(p *productmodel.ProductEntity) []any {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
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
