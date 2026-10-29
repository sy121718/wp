package productservice

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/seo"
	json "go_wp/pkg/jsonx"
)

func (s *Service) SetPublicCatalogIndex(index presentationcontract.PublishedEntityIndex) {
	s.publicCatalogIndex = index
}

// PublicCatalog 先约束真实上线实体，再用同一过滤条件取数与计数，隐藏项不占分页位置。
func (s *Service) PublicCatalog(ctx context.Context, req *productdto.PublicCatalogReq) (*productdto.PublicCatalogResp, error) {
	if req == nil || uuid.Validate(req.ProjectID) != nil || req.Page < 0 || req.Page > 100 || req.PageSize < 0 || req.PageSize > 60 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	filter := map[string]string{}
	if req.Filter != "" {
		if err := json.Unmarshal([]byte(req.Filter), &filter); err != nil {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
	}
	if filter == nil {
		filter = map[string]string{}
	}
	filter[productcontract.CollectionFilterStatus] = productenums.StatusPublished
	f, err := parseCollectionFilter(filter)
	if err != nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if !publicCatalogSort(req.Sort) {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	page, size := req.Page, req.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 12
	}
	out := &productdto.PublicCatalogResp{Items: []productdto.PublicCatalogItem{}, Page: page, PageSize: size}
	if s.publicCatalogIndex == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	paths, err := s.publicCatalogIndex.PublishedEntityIndex(ctx, req.ProjectID, productcontract.EntityTypeProduct, req.Lang)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return out, nil
	}
	f.ProjectID, f.Order, f.Keyword = req.ProjectID, req.Sort, strings.TrimSpace(req.Query)
	f.IDs = make([]string, 0, len(paths))
	for id := range paths {
		if uuid.Validate(id) == nil {
			f.IDs = append(f.IDs, id)
		}
	}
	if len(f.IDs) == 0 {
		return out, nil
	}
	sort.Strings(f.IDs)
	out.Total, err = s.m.CountForCollection(ctx, f)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListForCollection(ctx, f, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	values := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		values = append(values, map[string]string{"name": row.Name, "subtitle": row.Subtitle})
	}
	s.translateFieldsBatch(ctx, req.Lang, productcontract.EntityTypeProduct, values)
	variants, err := s.m.ListVariantsByProducts(ctx, ids)
	if err != nil {
		return nil, err
	}
	_, _, tags, err := s.taxonomyIndex(ctx, req.ProjectID, rows)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	for i, row := range rows {
		// 显式工程与状态判定不依赖数据库连接角色是否绕过RLS。
		if row.ProjectID != req.ProjectID || row.Status != productenums.StatusPublished || paths[row.ID] == "" {
			continue
		}
		item := productdto.PublicCatalogItem{ID: row.ID, Name: values[i]["name"], Subtitle: values[i]["subtitle"], URL: seo.AbsoluteSiteURL(paths[row.ID]), Images: imageURLs(row), Slug: row.Slug, Unit: row.Unit, Tags: []string{}}
		var alts []string
		_ = json.Unmarshal(row.ImageAlts, &alts)
		if len(alts) > 0 {
			item.ImageAlt = alts[0]
		}
		for _, tagID := range decodeStrings(row.TagIDs) {
			if tag := tags[tagID]; tag != nil {
				item.Tags = append(item.Tags, tag.Name)
			}
		}
		var cheapest *productmodel.VariantEntity
		var maxPrice *int64
		for _, v := range byProduct[row.ID] {
			if v.Enabled {
				price := yuanToCents(v.Price)
				if maxPrice == nil || price > *maxPrice {
					maxPrice = &price
				}
			}
			if v.Enabled && (cheapest == nil || v.Price < cheapest.Price || (v.Price == cheapest.Price && v.ID < cheapest.ID)) {
				cheapest = v
			}
		}
		if cheapest != nil {
			price := yuanToCents(cheapest.Price)
			item.Price = &price
			if cheapest.ComparePrice != nil {
				compare := yuanToCents(*cheapest.ComparePrice)
				item.ComparePrice = &compare
			}
		}
		item.MaxPrice = maxPrice
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func publicCatalogSort(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "default", "newest", "oldest", "priceAsc", "priceDesc", "ratingDesc":
		return true
	}
	return false
}
