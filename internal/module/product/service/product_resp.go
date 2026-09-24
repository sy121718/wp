package productservice

// product_resp.go — 商品响应组装（明细/列表、变体、属性、库存投影）。

import (
	"context"
	"strings"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorydto "go_wp/internal/module/inventory/dto"
	productmodel "go_wp/internal/module/product/model"
)

// attributeRespByProduct 批量取各商品引用的属性组（列表页专用，零 N+1）。
//
// products.attribute_ids 只存 id：多个商品可引用同一个属性组，定义只存一份。
// 这里按 id 去重后一次性取回，再按商品拆分；引用已失效（组被删）时跳过，
// 不让列表因为一条悬空引用整体失败。
// projectID 由调用方给出：products 是某一个工程下取回来的行（读路径 / 列表装配），
// 属性组同样走工程作用域（product_attributes 在迁移 215 名单里）。
func (s *Service) attributeRespByProduct(ctx context.Context, projectID string, products []*productmodel.ProductEntity) (out map[string][]*productdto.AttributeResp, err error) {
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
	rows, lerr := s.m.ListAttributesByIDs(ctx, ids, projectID)
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
	resp.Type = e.Type
	// 主体 SKU（products.sku_code，迁移 246）：详情页只读展示它的唯一性范围（本工程内唯一），
	// 运营据此核对「新建时填过的编码」有没有落库。
	resp.SKUCode = e.SKUCode
	resp.DefaultPrice = e.DefaultPrice
	resp.DefaultImage = mediaURL(e.DefaultImage)
	resp.Metadata = orJSON(e.Metadata, "{}")
	if e.BrandID != nil {
		resp.BrandID = *e.BrandID
	}
	for _, v := range variants {
		resp.Variants = append(resp.Variants, toVariantResp(v))
	}
	// 库存展示值查询期投影（issue #32）：一次批量取真源汇总。
	s.fillVariantStock(ctx, resp.ProjectID, resp.Variants)
	// 商品级库存聚合（列表「库存」列的三态，docs/14 §1.4）：详情接口同样带上，
	// 后台列表页逐商品取详情时就不必再单独打一次库存接口。
	s.fillProductStock(ctx, resp.ProjectID, []*productdto.ProductResp{resp})
	// 引用到的属性组（组 + 值），供后台与详情页直接渲染规格选择器。
	if groups, aerr := s.attributeRespByProduct(ctx, e.ProjectID, []*productmodel.ProductEntity{e}); aerr != nil {
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

// fillProductStock 查询期聚合商品列表的「库存」列（docs/14 §1.4，2026-09-19 口径）。
//
// 真源是 inventory_stocks（商品侧不留任何副本，issue #32）——这里一次批量读回若干商品在
// 各仓的库存行，按**三态**归并：
//
//	· 没有任何行 → none（未入库）；
//	· 任一行不跟踪 → infinite（∞）——**混合状态绝不求和**：求和等于把无限当 0，
//	               页面会显示成「有货」，而实际是「卖不完」；多规格商品里任一规格不跟踪同理；
//	· 全部跟踪   → tracked，StockTotal = 各行数量之和（**0 就显示 0**，与「未入库」不同）。
//
// 失败不阻断商品读取：库存列是展示投影，读不到就保持零值（页面按未入库显示）；
// 未装库存用例（纯商品单测路径）时空转。可用量判断一律走库存真源的带行锁路径，绝不看这个投影。
func (s *Service) fillProductStock(ctx context.Context, projectID string, resps []*productdto.ProductResp) {
	if s.invSvc == nil || len(resps) == 0 || strings.TrimSpace(projectID) == "" {
		return
	}
	ids := make([]string, 0, len(resps))
	for _, r := range resps {
		if r != nil && r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := s.invSvc.WarehouseStocksByProducts(ctx, projectID, ids)
	if err != nil {
		return
	}
	byProduct := make(map[string][]inventorydto.ProductWarehouseStock, len(ids))
	for _, row := range rows {
		byProduct[row.ProductID] = append(byProduct[row.ProductID], row)
	}
	for _, resp := range resps {
		if resp == nil {
			continue
		}
		items := byProduct[resp.ID]
		if len(items) == 0 {
			resp.StockState = productenums.StockStateNone
			continue
		}
		state, total := aggregateStockState(items)
		resp.StockState = state
		if state == productenums.StockStateTracked {
			resp.StockTotal = total
		}
		resp.StockWarehouses = warehouseStockRows(items)
	}
}

// aggregateStockState 一组库存行的三态结论：任一行不跟踪即「无限」，否则求和。
//
// 抽成函数而不是内联：商品级与「分仓」两处用的是**同一条**口径，
// 各写一遍必然分叉（一处求和、一处显示 ∞，页面上两行数字对不上）。
func aggregateStockState(rows []inventorydto.ProductWarehouseStock) (state string, total int) {
	if len(rows) == 0 {
		return productenums.StockStateNone, 0
	}
	for _, r := range rows {
		if !r.TrackQuantity {
			return productenums.StockStateInfinite, 0
		}
		total += r.Quantity
	}
	return productenums.StockStateTracked, total
}

// warehouseStockRows 按仓归并分仓明细（保持服务端返回的顺序，不额外排序）。
//
// 一行一个仓：同一仓里多规格多行时，该仓的 State / Quantity 是**归并后的结论**
// （同一条 aggregateStockState 口径）；SKUCode 与 CostPrice 取该仓第一条有值的
// （SKU 串只用于展示与对账，身份恒为 variantId；成本是 (仓库, SKU) 维度的事实）。
func warehouseStockRows(rows []inventorydto.ProductWarehouseStock) []*productdto.ProductWarehouseStockResp {
	order := make([]string, 0, len(rows))
	grouped := make(map[string][]inventorydto.ProductWarehouseStock, len(rows))
	for _, r := range rows {
		if _, ok := grouped[r.WarehouseID]; !ok {
			order = append(order, r.WarehouseID)
		}
		grouped[r.WarehouseID] = append(grouped[r.WarehouseID], r)
	}
	out := make([]*productdto.ProductWarehouseStockResp, 0, len(order))
	for _, id := range order {
		items := grouped[id]
		state, total := aggregateStockState(items)
		row := &productdto.ProductWarehouseStockResp{WarehouseID: id, State: state}
		for _, it := range items {
			if row.WarehouseCode == "" {
				row.WarehouseCode, row.WarehouseName = it.WarehouseCode, it.WarehouseName
			}
			if row.SKUCode == "" {
				row.SKUCode = it.SKUCode
			}
			if row.CostPrice == nil {
				row.CostPrice = it.CostPrice
			}
		}
		if state == productenums.StockStateTracked {
			row.TrackQuantity = true
			row.Quantity = total
		}
		out = append(out, row)
	}
	return out
}

// fillRelated 把商品引用的分类 / 品牌 / 标签填入详情响应（失败不阻断详情读取）。
func (s *Service) fillRelated(ctx context.Context, resp *productdto.ProductResp, e *productmodel.ProductEntity) (err error) {
	ids := decodeStrings(e.CategoryIDs)
	if len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids, e.ProjectID)
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
		if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {
			resp.Brand = toBrandResp(row)
		}
	}
	tagIDs := decodeStrings(e.TagIDs)
	if len(tagIDs) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, tagIDs, e.ProjectID)
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
		Status: e.Status, Sort: e.Sort, Images: mediaURLs(decodeStrings(e.Images)),
		ImageAlts: decodeStrings(e.ImageAlts),
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if len(resp.Images) == 0 {
		resp.Images = []string{}
	}
	return resp
}

// applyPriceRange 算出商品的价格区间（只读派生）。
//
// 两种商品的来源不同，别混：
//
//	· variant 商品：区间由**变体**派生（主体不存价，区间是展示用的汇总）；
//	· bundle 容器：只有一个对外价格 = 容器价（products.default_price）。它没有自己的 SKU，
//	  若还从变体派生，就会把自动生成的价 0 变体当成售价（列表显示 0.00 的成因）。
func applyPriceRange(resp *productdto.ProductResp, variants []*productmodel.VariantEntity) {
	resp.VariantCount = len(variants)
	if resp.Type == productmodel.TypeBundle {
		if resp.DefaultPrice != nil {
			resp.PriceMin, resp.PriceMax = *resp.DefaultPrice, *resp.DefaultPrice
		}
		return
	}
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
	totals, err := s.inv.VariantStockTotals(ctx, projectID, ids)
	if err != nil {
		return
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
