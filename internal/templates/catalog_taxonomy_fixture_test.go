package templates

import (
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

func taxonomyData(result any) map[string]any {
	return groupDData(map[string]any{
		"req_path": "/admin/product-tags",
		"Scope":    &productcontract.CatalogScope{ProjectID: "pr1", Projects: []projectdto.ProjectResp{{ID: "pr1", Name: "站点"}}},
		"Query":    &productdto.CatalogTaxonomyReq{}, "Result": result,
		"Categories": []*productdto.CategoryResp{}, "RuleTypes": []*productdto.TagRuleTypeResp{{Type: "new_arrival", ParamKeys: []string{"days"}}},
	})
}
func taxonomyBrands(rows ...*productdto.BrandResp) map[string]any {
	return taxonomyData(&productdto.TaxonomyListResp[*productdto.BrandResp]{Items: rows, Total: int64(len(rows)), Page: 1, Size: 20})
}
func taxonomyAttributes(rows ...*productdto.AttributeResp) map[string]any {
	return taxonomyData(&productdto.TaxonomyListResp[*productdto.AttributeResp]{Items: rows, Total: int64(len(rows)), Page: 1, Size: 20})
}
func taxonomyTags(rows ...*productdto.TagResp) map[string]any {
	return taxonomyData(&productdto.TaxonomyListResp[*productdto.TagResp]{Items: rows, Total: int64(len(rows)), Page: 1, Size: 20})
}
func taxonomyCategories(rows ...*productdto.CategoryResp) map[string]any {
	data := taxonomyData(&productdto.CategorySearchResp{Items: rows, Total: int64(len(rows)), Page: 1, Size: 20})
	data["Categories"] = rows
	return data
}
func tagHitsData(total, page, size int, project string, rows []*productdto.TagProductResp) map[string]any {
	return groupDData(map[string]any{
		"Query":    &productdto.ListTagProductsPageReq{ProjectID: project, TagID: "t1"},
		"Result":   &productdto.TagProductsListResp{TagID: "t1", TagName: "清仓", Total: total, Page: page, PageSize: size, Items: rows},
		"req_path": "/admin/product-tags/hits?id=t1&project=" + project,
	})
}
