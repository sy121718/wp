package productservice

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/pagination"
)

func normalizedTaxonomyQuery(query *productdto.CatalogTaxonomyReq) productdto.CatalogTaxonomyReq {
	q := productdto.CatalogTaxonomyReq{}
	if query != nil {
		q = *query
	}
	_ = q.BindQuery(nil)
	return q
}

// FlattenCategoryTree 只展开实体顺序，不复制字段或生成展示行。
func FlattenCategoryTree(nodes []*productdto.CategoryResp) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(nodes))
	var walk func([]*productdto.CategoryResp)
	walk = func(items []*productdto.CategoryResp) {
		for _, n := range items {
			if n != nil {
				out = append(out, n)
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

// CategoriesInProject 按实体所属工程限制候选范围，不依赖数据库角色是否已启用 RLS。
func CategoriesInProject(nodes []*productdto.CategoryResp, projectID string) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ProjectID == projectID {
			out = append(out, n)
		}
	}
	return out
}

// CategoryNodes 返回工程内全部分类原对象，供父级关系查询与引用选择复用。
func (s *Service) CategoryNodes(ctx context.Context, projectID string) ([]*productdto.CategoryResp, error) {
	if projectID == "" {
		return []*productdto.CategoryResp{}, nil
	}
	nodes, err := s.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return CategoriesInProject(FlattenCategoryTree(nodes), projectID), nil
}

func (s *Service) QueryCategories(ctx context.Context, query *productdto.CatalogTaxonomyReq) (*productdto.CategorySearchResp, error) {
	q := normalizedTaxonomyQuery(query)
	projectID := q.ProjectID
	result := &productdto.CategorySearchResp{Items: []*productdto.CategoryResp{}, Page: 1, Size: q.Size}
	if projectID == "" {
		return result, nil
	}
	req := &productdto.ListCategoryPageReq{ProjectID: projectID, Keyword: q.Keyword, Page: q.Page, Size: q.Size}
	found, err := s.ListCategoryPage(ctx, req)
	if err != nil {
		return nil, err
	}
	page := pagination.ClampPage(q.Page, q.Size, found.Total)
	if page != req.Page {
		req.Page = page
		found, err = s.ListCategoryPage(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	result.Items, result.Total, result.MatchTotal, result.Page = FlattenCategoryTree(found.Items), found.Total, found.MatchTotal, page
	return result, nil
}

func (s *Service) SearchCategoryParents(ctx context.Context, query *productdto.CatalogTaxonomyReq) ([]*productdto.CategoryResp, error) {
	q := normalizedTaxonomyQuery(query)
	scope, err := s.ResolveCatalogScope(ctx, q.ProjectID)
	if err != nil {
		return nil, err
	}
	q.ProjectID = scope.ProjectID
	projectID := q.ProjectID
	out := []*productdto.CategoryResp{}
	if q.Keyword == "" {
		return out, nil
	}
	result, err := s.QueryCategories(ctx, &q)
	if err != nil {
		return nil, err
	}
	for _, n := range result.Items {
		if n.Matched && n.ProjectID == projectID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *Service) QueryBrands(ctx context.Context, query *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.BrandResp], error) {
	q := normalizedTaxonomyQuery(query)
	projectID := q.ProjectID
	result := &productdto.TaxonomyListResp[*productdto.BrandResp]{Items: []*productdto.BrandResp{}, Page: 1, Size: q.Size}
	if projectID == "" {
		return result, nil
	}
	req := &productdto.ListBrandReq{ProjectID: projectID, Keyword: q.Keyword}
	total, err := s.CountBrands(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Page, req.Size = pagination.ClampPage(q.Page, q.Size, total), q.Size
	result.Items, err = s.ListBrands(ctx, req)
	if err != nil {
		return nil, err
	}
	result.Total, result.Page = total, req.Page
	return result, nil
}

func (s *Service) QueryTags(ctx context.Context, query *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.TagResp], error) {
	q := normalizedTaxonomyQuery(query)
	projectID := q.ProjectID
	result := &productdto.TaxonomyListResp[*productdto.TagResp]{Items: []*productdto.TagResp{}, Page: 1, Size: q.Size}
	if projectID == "" {
		return result, nil
	}
	req := &productdto.ListTagReq{ProjectID: projectID, Keyword: q.Keyword}
	total, err := s.CountTags(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Page, req.Size = pagination.ClampPage(q.Page, q.Size, total), q.Size
	result.Items, err = s.ListTags(ctx, req)
	if err != nil {
		return nil, err
	}
	result.Total, result.Page = total, req.Page
	return result, nil
}
