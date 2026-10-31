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

const (
	publicCatalogMaxPage         = 100
	publicCatalogMaxPageSize     = 60
	publicCatalogDefaultPageSize = 12
)

func (s *Service) PublicCatalog(ctx context.Context, req *productdto.PublicCatalogReq) (*productdto.PublicCatalogResp, error) {
	if req == nil || uuid.Validate(req.ProjectID) != nil || req.Page < 0 || req.Page > publicCatalogMaxPage || req.PageSize < 0 || req.PageSize > publicCatalogMaxPageSize {
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
	order := strings.TrimSpace(req.Sort)
	if !publicCatalogSort(order) {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	page, size := req.Page, req.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = publicCatalogDefaultPageSize
	}
	out := &productdto.PublicCatalogResp{Items: []productdto.PublicCatalogItemResp{}, Page: page, PageSize: size}
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
	f.ProjectID, f.Order, f.Keyword = req.ProjectID, order, strings.TrimSpace(req.Query)
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

	offset := (page - 1) * size
	rows, total, err := s.m.ListForCollectionPageWithTotal(ctx, f, size, offset)
	if err != nil {
		return nil, err
	}
	out.Total = total
	if len(rows) == 0 {

		if page > 1 {
			if out.Total, err = s.m.CountForCollection(ctx, f); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	values := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		values = append(values, map[string]string{"name": row.Name, "subtitle": row.Subtitle})
	}
	s.translateFieldsBatch(ctx, req.Lang, productcontract.EntityTypeProduct, values)

	variants, err := s.m.ListVariantPriceFacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagIndex(ctx, req.ProjectID, rows)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	for i, row := range rows {

		if row.ProjectID != req.ProjectID || row.Status != productenums.StatusPublished || paths[row.ID] == "" {
			continue
		}
		item := productdto.PublicCatalogItemResp{ID: row.ID, Name: values[i]["name"], Subtitle: values[i]["subtitle"], URL: seo.AbsoluteSiteURL(paths[row.ID]), Images: imageURLs(row), Slug: row.Slug, Unit: row.Unit, Tags: []string{}}
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

		summary := summarizeVariantPrices(byProduct[row.ID], true)
		if summary.MinVariant != nil {
			price := yuanToCents(summary.MinPrice)
			item.Price = &price
			if summary.MinVariant.ComparePrice != nil {
				compare := yuanToCents(*summary.MinVariant.ComparePrice)
				item.ComparePrice = &compare
			}
			maxPrice := yuanToCents(summary.MaxPrice)
			item.MaxPrice = &maxPrice
		}
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
