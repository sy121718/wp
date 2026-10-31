package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

func (s *Service) ListVariantOptions(ctx context.Context, req *productdto.ListVariantOptionReq) (list []*productdto.VariantOptionResp, err error) {
	if req == nil {
		req = &productdto.ListVariantOptionReq{}
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	products, lerr := s.m.List(ctx, projectID, strings.TrimSpace(req.Keyword), "", maxPageSize, 0)
	if lerr != nil {
		return nil, lerr
	}
	if len(products) == 0 {
		return []*productdto.VariantOptionResp{}, nil
	}
	ids := make([]string, 0, len(products))
	nameOf := make(map[string]string, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
		nameOf[p.ID] = p.Name
	}

	variants, verr := s.m.ListVariantOptionFacts(ctx, ids)
	if verr != nil {
		return nil, verr
	}
	list = make([]*productdto.VariantOptionResp, 0, len(variants))
	for _, v := range variants {
		if !v.Enabled {
			continue
		}
		list = append(list, &productdto.VariantOptionResp{
			VariantID: v.ID, SKUCode: v.SKUCode, ProductID: v.ProductID,
			ProductName: nameOf[v.ProductID], Price: v.Price, CostPrice: v.CostPrice, Enabled: v.Enabled,
		})
	}
	return list, nil
}

func (s *Service) ListAllVariantOptions(ctx context.Context, projectID string) ([]productcontract.VariantOption, error) {

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	list, err := s.ListVariantOptions(ctx, &productdto.ListVariantOptionReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make([]productcontract.VariantOption, 0, len(list))
	for _, o := range list {
		out = append(out, productcontract.VariantOption{
			VariantID: o.VariantID, ProductID: o.ProductID, SKUCode: o.SKUCode,
			ProductName: o.ProductName, CostPrice: o.CostPrice,
		})
	}
	return out, nil
}
