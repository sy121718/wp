package productservice

// 聚合商品与当前发布登记；发布尝试历史由独立查询提供。
//
// 两条读法按**过滤能否下推到商品表**分档，不是两种风格：
//   - 无状态筛选：状态列不在 products 表里也不需要判断，先 count 夹页、再取本页投影，
//     发布端口只收本页 id —— 页内成本与工程内商品数无关（PRD-011 的无状态档）；
//   - 有状态筛选（active/draft/none）：发布状态是跨模块事实，过滤必须发生在与发布登记
//     合并**之后**。所以这一类仍要全量读商品再合并过滤，绝不能先切商品页再过滤 ——
//     那会让「第 2 页」只在前 20 个商品里找已上线的，直接漏行。
// 有状态档的跨模块有界查询（让状态进到正确分页路径）另行推进，本文件不声称已完成。

import (
	"context"
	"sort"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/pagination"
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
	keyword := strings.TrimSpace(req.Keyword)
	status := normalizePublishStatus(req.Status)
	if status == "" {
		return s.queryPublishRecordsPaged(ctx, scope.ProjectID, keyword, req.Page, result)
	}
	return s.queryPublishRecordsByStatus(ctx, scope.ProjectID, keyword, status, req.Page, result)
}

// queryPublishRecordsPaged 无状态筛选档：count → 夹页 → 只读本页，再把本页 id 交发布端口。
//
// 顺序是判据不是优化：夹页必须在取数之前（否则 page=999 会先去读第 19980 行再丢掉），
// 发布端口必须在拿到本页之后才调用，且空页**不调用** ——
// contract 里 「entityIDs 为空 = 本工程全部」，空页传空切片会把它变成全工程读。
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
	// 无状态档不过滤，行与商品一一对应；仍走同一合并函数，形状与有状态档不会分叉。
	result.Records = mergePublishRows(products, langs, "")
	return result, nil
}

// queryPublishRecordsByStatus 有状态筛选档：过滤依赖跨模块发布事实，只能先全量合并再切页。
//
// 这条路径的成本随工程内商品数增长（PRD-011 已记录）。它**不能**改写成「先切商品页再
// 过滤」：那样 total 与可见行数都会错，且越是已上线稀少的工程错得越离谱。
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

// readPublishLangs 按给定商品读各语言发布明细（空集合直接返回，不碰端口）。
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
