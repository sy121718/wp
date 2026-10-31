package templates

import (
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	json "go_wp/pkg/jsonx"
)

func groupDProductRow() *productdto.ProductResp {
	price, weight := 10.0, 0.5
	return &productdto.ProductResp{
		ID: "p1", ProjectID: "pr1", Name: "商品一", Slug: "p-one", Status: "draft",
		Subtitle: "副标题", Unit: "件", DefaultPrice: &price, Weight: &weight,
		Images: []string{"https://cdn.example.com/1.jpg"}, ImageAlts: []string{"图一"},
		Description:  json.RawMessage(`{"html":"<p>商品描述</p>"}`),
		VariantCount: 1, PriceMin: 10, PriceMax: 20, AttributeIDs: []string{"a1"},
		Attributes:  []*productdto.AttributeResp{{ID: "a1", Key: "color", Name: "颜色", IsVariation: true, Values: []productdto.AttributeValueResp{{ID: "av1", Key: "red", Label: "红", Enabled: true}, {ID: "av2", Key: "blue", Label: "蓝", Enabled: true}}}},
		Variants:    []*productdto.VariantResp{{ID: "v1", SKUCode: "SZ_TEE_001", Price: 19, Enabled: true, StockTotal: 5, StocksKnown: true, OptionValues: json.RawMessage(`{"color":"red"}`)}},
		Rating:      &productdto.RatingResp{Rating: 4.5, RatingCount: 2, HasRating: true, Items: []productdto.RatingItemResp{{ID: "r1", Score: 5, Source: "manual"}}},
		CategoryIDs: []string{"c1"}, PrimaryCategoryID: "c1", Categories: []*productdto.CategoryResp{{ID: "c1", Name: "男装"}},
		BrandID: "b1", Brand: &productdto.BrandResp{ID: "b1", Name: "山野"}, TagIDs: []string{"t1", "t2"},
		Tags: []*productdto.TagResp{{ID: "t1", Name: "新品", Kind: "manual"}, {ID: "t2", Name: "热销", Kind: "rule"}},
	}
}

func productCatalogData(extra map[string]any) map[string]any {
	p := groupDProductRow()
	data := groupDData(map[string]any{
		"Scope": &productcontract.CatalogScope{ProjectID: "pr1", Projects: []projectdto.ProjectResp{{ID: "pr1", Name: "站点"}}},
		"Query": &productdto.ProductListPageReq{}, "req_path": "/admin/products?project=pr1",
		"Result":  &productdto.ProductListResp{Items: []*productdto.ProductResp{p}, Total: 1, Page: 1, Size: 20},
		"Product": p, "Categories": p.Categories, "Brands": []*productdto.BrandResp{p.Brand}, "Tags": p.Tags, "Attributes": p.Attributes,
		"WarehouseOptions":    []productdto.WarehouseOptionResp{{ID: "w1", Name: "苏州仓"}},
		"WarehouseSKUOptions": []productdto.WarehouseSKUGroupResp{}, "TemplateCatalog": &productdto.EntityTemplateCatalogResp{},
		"Rules": []*productdto.PricingRuleTypeResp{}, "Roundings": []*productdto.PricingRoundingOptionResp{},
		"TplPanel": productcontract.DetailTemplatePanelFacts{},
	})
	for k, v := range extra {
		data[k] = v
	}

	if rows, ok := extra["Products"].([]*productdto.ProductResp); ok {
		data["Result"] = &productdto.ProductListResp{Items: rows, Total: int64(len(rows)), Page: 1, Size: 20}
	}
	return data
}
