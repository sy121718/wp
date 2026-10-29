package productservice

// 聚合商品与当前发布登记；发布尝试历史由独立查询提供。
// 状态筛选依赖完整发布事实，因此先批量读取并合并，再按筛选结果分页。

import (
	"context"
	"sort"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// ProductPublishRecordPageSize 发布记录每页条数（与商品列表同量级）。
const ProductPublishRecordPageSize = 20

// 发布记录页的状态筛选取值（空 = 全部）。
const (
	publishFilterActive = "active"
	publishFilterDraft  = "draft"
	publishFilterNone   = "none"
)

// QueryPublishRecords 聚合商品与各语言发布登记，按发布状态过滤后分页。
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
	products, err := s.listProductsForPublishRecord(ctx, scope.ProjectID, strings.TrimSpace(req.Keyword))
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	langs, err := s.pagePublishRecords.Langs(ctx, scope.ProjectID, ids)
	if err != nil {
		return nil, err
	}
	rows := mergePublishRows(products, langs, normalizePublishStatus(req.Status))
	result.Total = int64(len(rows))
	result.Page = clampPublishPage(req.Page, result.Total, result.Limit)
	result.Records = pagePublishRows(rows, result.Page, result.Limit)
	return result, nil
}

// listProductsForPublishRecord 取本工程（按关键词过滤）的全部商品。
//
// 为什么不用分页读：状态筛选在合并之后（见上），分页也必须在那之后；
// 而商品表的行数是这个后台里可控的量级（一个工程的商品数），全量取三列足够。
//
// **不能**用 s.m.List(ctx, projectID, keyword, "", 0, 0) 表达「全量」：那里的 (limit, offset)
// 直传 gorm，Limit(0) 生成的是 LIMIT 0 —— 零行，一页空白且不报错。model 侧为此另开了
// ListForPublishRecords（同一筛选口径、不分页）。
func (s *Service) listProductsForPublishRecord(ctx context.Context, projectID, keyword string) ([]*productmodel.ProductEntity, error) {
	return s.m.ListForPublishRecords(ctx, projectID, keyword)
}

// mergePublishRows 商品 × 发布登记 → 一行一个商品（多语言折叠进 Langs）。
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
				// 版本号取最大的那条已上线语言：同批多语言的产物版本号相同，
				// 跨批次时较新语言才代表「线上是哪一版」。
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

// publishRowMatchesStatus 状态筛选：active（已上线）/ draft（有实例未上线）/ none（无实例）。
//
// **stale 不参与这里**：待重建的已上线商品仍是 active（线上有内容，只是旧的），
// 那一列单独显示。把它并进筛选会让「想看线上有没有」的人看不到线上旧内容。
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

// sortPublishLangs 已上线在前（按发布时间倒序），其余按语言名 —— 「最新的在最上面」
// 是这一列被看的主要方式。
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

// normalizePublishStatus 未知取值退回「全部」：查询串不是可信边界，
// 把 ?status=xxx 当过滤条件会让页面莫名其妙地空掉。
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

// clampPublishPage 页码收敛：越界退回末页（与其它列表页同口径）。
func clampPublishPage(page int, total int64, limit int) int {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = ProductPublishRecordPageSize
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages > 0 && page > pages {
		return pages
	}
	return page
}

// pagePublishRows 按页码切本页行（切片边界自身归一，负 offset 在 Go 里是 panic）。
func pagePublishRows(rows []productdto.PublishRecordRowResp, page, limit int) []productdto.PublishRecordRowResp {
	if limit < 1 {
		limit = ProductPublishRecordPageSize
	}
	start := (page - 1) * limit
	if start < 0 {
		start = 0
	}
	if start >= len(rows) {
		return []productdto.PublishRecordRowResp{}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}
