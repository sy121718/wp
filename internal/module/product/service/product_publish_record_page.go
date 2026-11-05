package productservice

import (
	"context"
	"sort"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
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
		Records: []productdto.PublishRecordRowResp{},
		Page:    1, Limit: ProductPublishRecordPageSize,
	}
	scope, err := s.ResolveCatalogScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	page, err := s.m.QueryPublishRecordPage(ctx, scope.ProjectID,
		strings.TrimSpace(req.Keyword), normalizePublishStatus(req.Status), result.Limit, req.Page)
	if err != nil {
		return nil, err
	}
	result.Total = page.Total
	result.Page = page.Page
	result.Records = publishRowsOf(page.Rows)
	return result, nil
}

// publishRowsOf 把读模型的行还原成发布记录行：明细在 SQL 里已经按
// 「实例新→旧、语言升序」排好，这里逐语言再稳一遍，与页面显示的旧口径一致。
func publishRowsOf(rows []productmodel.PublishRecordPageRow) []productdto.PublishRecordRowResp {
	out := make([]productdto.PublishRecordRowResp, 0, len(rows))
	for _, r := range rows {
		row := productdto.PublishRecordRowResp{
			ProductID:   r.ProductID,
			ProductName: r.ProductName,
			ProductSlug: r.ProductSlug,
			HasInstance: len(r.Facts) > 0,
			Langs:       make([]productdto.PublishRecordLangResp, 0, len(r.Facts)),
		}
		for _, f := range r.Facts {
			row.Langs = append(row.Langs, productdto.PublishRecordLangResp{
				Lang:            f.Lang,
				OnlinePath:      f.ActivePath,
				Online:          f.Active,
				Stale:           f.Stale,
				PublishedAt:     f.PublishedAt,
				ArtifactVersion: f.ArtifactVersion,
				ArtifactHash:    f.ArtifactHash,
			})
		}
		row.Langs = sortPublishLangs(row.Langs)
		for _, l := range row.Langs {
			row.Online = row.Online || l.Online
			row.Stale = row.Stale || l.Stale
			if l.PublishedAt.After(row.PublishedAt) {
				row.PublishedAt = l.PublishedAt
			}
			if l.ArtifactVersion > row.ArtifactVersion {
				row.ArtifactVersion = l.ArtifactVersion
				row.ArtifactHash = l.ArtifactHash
			}
		}
		out = append(out, row)
	}
	return out
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
