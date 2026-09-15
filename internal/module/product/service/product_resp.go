package productservice

// product_resp.go — 商品响应组装（明细/列表、变体、属性、库存投影）。

import (
	"context"
	"strings"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// attributeRespByProduct 批量取各商品引用的属性组（列表页专用，零 N+1）。
//
// products.attribute_ids 只存 id：多个商品可引用同一个属性组，定义只存一份。
// 这里按 id 去重后一次性取回，再按商品拆分；引用已失效（组被删）时跳过，
// 不让列表因为一条悬空引用整体失败。
func (s *Service) attributeRespByProduct(ctx context.Context, products []*productmodel.ProductEntity) (out map[string][]*productdto.AttributeResp, err error) {
	out = map[string][]*productdto.AttributeResp{}
	idSet := map[string]bool{}
	for _, p := range products {
		for _, id := range decodeStrings(p.AttributeIDs) {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	rows, lerr := s.m.ListAttributesByIDs(ctx, ids)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productdto.AttributeResp, len(rows))
	for _, r := range rows {
		byID[r.ID] = toAttributeResp(r)
	}
	for _, p := range products {
		refs := decodeStrings(p.AttributeIDs)
		if len(refs) == 0 {
			continue
		}
		items := make([]*productdto.AttributeResp, 0, len(refs))
		for _, id := range refs {
			if a, ok := byID[id]; ok {
				items = append(items, a)
			}
		}
		if len(items) > 0 {
			out[p.ID] = items
		}
	}
	return out, nil
}

// toResp 组装商品详情（含变体与价格区间）。
func (s *Service) toResp(ctx context.Context, e *productmodel.ProductEntity) (res *productdto.ProductResp, err error) {
	variants, err := s.m.ListVariants(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	resp := s.toListResp(e)
	resp.ImageAlts = decodeStrings(e.ImageAlts)
	resp.Description = orJSON(e.Description, "{}")
	resp.Subtitle = e.Subtitle
	resp.Unit = e.Unit
	resp.Weight = e.Weight
	resp.SEOTitle = e.SEOTitle
	resp.SEODescription = e.SEODescription
	resp.AttributeIDs = decodeStrings(e.AttributeIDs)
	resp.CategoryIDs = decodeStrings(e.CategoryIDs)
	if e.PrimaryCategoryID != nil {
		resp.PrimaryCategoryID = *e.PrimaryCategoryID
	}
	resp.TagIDs = decodeStrings(e.TagIDs)
	if e.PublishedAt != nil {
		resp.PublishedAt = e.PublishedAt.Format(time.RFC3339)
	}
	resp.RelatedIDs = decodeStrings(e.RelatedIDs)
	resp.BundleItems = orJSON(e.BundleItems, string(defaultBundleItemsJSON))
	resp.DefaultPrice = e.DefaultPrice
	resp.DefaultImage = e.DefaultImage
	resp.Metadata = orJSON(e.Metadata, "{}")
	if e.BrandID != nil {
		resp.BrandID = *e.BrandID
	}
	for _, v := range variants {
		resp.Variants = append(resp.Variants, toVariantResp(v))
	}
	// 库存展示值查询期投影（issue #32）：一次批量取真源汇总。
	s.fillVariantStock(ctx, resp.ProjectID, resp.Variants)
	// 引用到的属性组（组 + 值），供后台与详情页直接渲染规格选择器。
	if groups, aerr := s.attributeRespByProduct(ctx, []*productmodel.ProductEntity{e}); aerr != nil {
		return nil, aerr
	} else {
		resp.Attributes = groups[e.ID]
	}
	applyPriceRange(resp, variants)
	// 挂载的分类 / 品牌 / 标签（issue #12）：翻译工作台要按分类名、品牌名、标签名
	// 展示与取词，详情接口一并返回实体（后台翻译页不为每个 id 再打一次接口）。
	if rerr := s.fillRelated(ctx, resp, e); rerr != nil {
		return nil, rerr
	}
	return resp, nil
}

// fillRelated 把商品引用的分类 / 品牌 / 标签填入详情响应（失败不阻断详情读取）。
func (s *Service) fillRelated(ctx context.Context, resp *productdto.ProductResp, e *productmodel.ProductEntity) (err error) {
	ids := decodeStrings(e.CategoryIDs)
	if len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids)
		if cerr != nil {
			return cerr
		}
		byID := make(map[string]*productmodel.ProductCategoryEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range ids {
			if row, ok := byID[id]; ok {
				resp.Categories = append(resp.Categories, toCategoryResp(row))
			}
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID); berr == nil {
			resp.Brand = toBrandResp(row)
		}
	}
	tagIDs := decodeStrings(e.TagIDs)
	if len(tagIDs) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, tagIDs)
		if terr != nil {
			return terr
		}
		byID := make(map[string]*productmodel.ProductTagEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range tagIDs {
			if row, ok := byID[id]; ok {
				resp.Tags = append(resp.Tags, toTagResp(row))
			}
		}
	}
	return nil
}

// toListResp 组装列表项（不含 metadata / description）。
func (s *Service) toListResp(e *productmodel.ProductEntity) *productdto.ProductResp {
	resp := &productdto.ProductResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Slug: e.Slug,
		Status: e.Status, Sort: e.Sort, Images: decodeStrings(e.Images),
		ImageAlts: decodeStrings(e.ImageAlts),
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if len(resp.Images) == 0 {
		resp.Images = []string{}
	}
	return resp
}

// applyPriceRange 用变体算出商品的价格区间（商品主体不存价格，区间是只读派生）。
func applyPriceRange(resp *productdto.ProductResp, variants []*productmodel.VariantEntity) {
	resp.VariantCount = len(variants)
	if len(variants) == 0 {
		return
	}
	minV, maxV := variants[0].Price, variants[0].Price
	for _, v := range variants[1:] {
		if v.Price < minV {
			minV = v.Price
		}
		if v.Price > maxV {
			maxV = v.Price
		}
	}
	resp.PriceMin, resp.PriceMax = minV, maxV
}

// fillVariantStock 用库存真源汇总填充变体响应的库存投影值（issue #32）。
//
// 库存不再有商品侧缓存列，展示值只能**查询期投影**：
//
//	· 一次批量取真源按变体汇总（不是逐条查）；
//	· 未注入库存 model（纯商品单测路径）时保持 0 —— 调用方**不可**据此判断可用量；
//	· 投影失败不影响商品本身的读取（库存是展示值，不是商品的组成部分）。
//
// 死线不变：可用量判断一律走库存真源的带行锁路径，永远不看这个投影值。
func (s *Service) fillVariantStock(ctx context.Context, projectID string, variants []*productdto.VariantResp) {
	if s.inv == nil || len(variants) == 0 {
		return
	}
	ids := make([]string, 0, len(variants))
	for _, v := range variants {
		if v != nil && v.ID != "" {
			ids = append(ids, v.ID)
		}
	}
	rows, err := s.inv.StockTotals(ctx, projectID, ids)
	if err != nil {
		return
	}
	totals := make(map[string]int, len(rows))
	for _, row := range rows {
		if row != nil {
			totals[row.VariantID] = row.Total
		}
	}
	for _, v := range variants {
		if v != nil {
			v.StockTotal = totals[v.ID]
		}
	}
}

// toVariantResp 变体实体 → 响应。
func toVariantResp(v *productmodel.VariantEntity) *productdto.VariantResp {
	return &productdto.VariantResp{
		ID: v.ID, ProductID: v.ProductID, SKUCode: v.SKUCode, Barcode: v.Barcode,
		Price: v.Price, ComparePrice: v.ComparePrice, CostPrice: v.CostPrice,
		Image: v.Image, OptionValues: orJSON(v.OptionValues, "{}"),
		Enabled: v.Enabled, Sort: v.Sort,
		CreatedAt: v.CreatedAt.Format(time.RFC3339), UpdatedAt: v.UpdatedAt.Format(time.RFC3339),
	}
}

// formatTimePtr 可空时间 → RFC3339（nil → 空串）。
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
