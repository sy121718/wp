package productservice

import (
	"context"
	"sort"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/pagination"
)

const ProductPublishRecordPageSize = 20

const (
	publishFilterActive = "active"
	publishFilterDraft  = "draft"
	publishFilterNone   = "none"
)

func (s *Service) QueryPublishRecords(ctx context.Context, req *productdto.ProductPublishRecordPageReq) (*productcontract.PublishRecordList, error) {
	if req == nil {
		req = &productdto.ProductPublishRecordPageReq{}
	}
	result := &productcontract.PublishRecordList{
		Available: s.pagePublishRecords != nil, Records: []productdto.PublishRecordRowResp{},
		Page: 1, Limit: ProductPublishRecordPageSize,
	}
	if !result.Available {
		return result, nil
	}
	scope, err := s.ResolveCatalogScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(req.Keyword)
	status := normalizePublishStatus(req.Status)
	if status == "" {
		return s.queryPublishRecordsPaged(ctx, scope.ProjectID, keyword, req.Page, result)
	}
	return s.queryPublishRecordsByStatus(ctx, scope.ProjectID, keyword, status, req.Page, result)
}

func (s *Service) queryPublishRecordsPaged(ctx context.Context, projectID, keyword string, page int,
	result *productcontract.PublishRecordList) (*productcontract.PublishRecordList, error) {
	total, err := s.m.CountForPublishRecords(ctx, projectID, keyword)
	if err != nil {
		return nil, err
	}
	result.Total = total
	if total == 0 {
		return result, nil
	}
	result.Page = pagination.ClampPage(page, result.Limit, total)
	products, err := s.m.ListForPublishRecordsPage(ctx, projectID, keyword, result.Limit, (result.Page-1)*result.Limit)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return result, nil
	}
	langs, err := s.readPublishLangs(ctx, projectID, products)
	if err != nil {
		return nil, err
	}

	result.Records = mergePublishRows(products, langs, "")
	return result, nil
}

func (s *Service) queryPublishRecordsByStatus(ctx context.Context, projectID, keyword, status string, page int,
	result *productcontract.PublishRecordList) (*productcontract.PublishRecordList, error) {
	products, err := s.m.ListForPublishRecords(ctx, projectID, keyword)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return result, nil
	}
	langs, err := s.readPublishLangs(ctx, projectID, products)
	if err != nil {
		return nil, err
	}
	rows := mergePublishRows(products, langs, status)
	result.Total = int64(len(rows))
	result.Records, result.Page = pagination.Slice(rows, page, result.Limit)
	return result, nil
}

func (s *Service) readPublishLangs(ctx context.Context, projectID string, products []*productmodel.ProductEntity) (map[string][]productdto.PublishRecordLangResp, error) {
	if len(products) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	return s.pagePublishRecords.Langs(ctx, projectID, ids)
}

func mergePublishRows(products []*productmodel.ProductEntity,
	langs map[string][]productdto.PublishRecordLangResp, status string) []productdto.PublishRecordRowResp {
	rows := make([]productdto.PublishRecordRowResp, 0, len(products))
	for _, p := range products {
		row := productdto.PublishRecordRowResp{
			ProductID:   p.ID,
			ProductName: p.Name,
			ProductSlug: p.Slug,
			Langs:       []productdto.PublishRecordLangResp{},
		}
		if detail := langs[p.ID]; len(detail) > 0 {
			row.HasInstance = true
			row.Langs = sortPublishLangs(detail)
			for _, l := range row.Langs {
				if l.Online {
					row.Online = true
				}
				row.Stale = row.Stale || l.Stale
				if l.PublishedAt.After(row.PublishedAt) {
					row.PublishedAt = l.PublishedAt
				}

				if l.ArtifactVersion > row.ArtifactVersion {
					row.ArtifactVersion = l.ArtifactVersion
					row.ArtifactHash = l.ArtifactHash
				}
			}
		}
		if !publishRowMatchesStatus(row, status) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func publishRowMatchesStatus(row productdto.PublishRecordRowResp, status string) bool {
	switch status {
	case publishFilterActive:
		return row.Online
	case publishFilterDraft:
		return row.HasInstance && !row.Online
	case publishFilterNone:
		return !row.HasInstance
	default:
		return true
	}
}

func sortPublishLangs(in []productdto.PublishRecordLangResp) []productdto.PublishRecordLangResp {
	out := make([]productdto.PublishRecordLangResp, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].Online && !out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].PublishedAt.After(out[j].PublishedAt)
		}
		return out[i].Lang < out[j].Lang
	})
	return out
}

func normalizePublishStatus(status string) string {
	switch strings.TrimSpace(status) {
	case publishFilterActive:
		return publishFilterActive
	case publishFilterDraft:
		return publishFilterDraft
	case publishFilterNone:
		return publishFilterNone
	default:
		return ""
	}
}
