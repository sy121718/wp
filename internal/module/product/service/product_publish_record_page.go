package productservice

// product_publish_record_page.go — 商品发布记录整页（B9）。
//
// 一屏一个方法（同商品列表页 / 详情页模板页）：控制器只剩「绑参数 → 调这里 → 渲染」。
//
// 这一屏回答的问题是「我的商品现在发布成什么样了」—— 三个维度：发布到哪个站点路径、
// 什么时候、是不是最新的。**没有「历史」这一维**：库里没有记录每次发布尝试成功/失败的
// 明细表（回执只覆盖访问面切换那一段，完成后只留终态），所以这里不编造「上次失败」这类
// 行，只给当前状态。这一取舍写进了报告。
//
// 先分页后取发布登记：发布登记是**按实例批量读**（presentation 侧一次查里带 IN），
// 每页 20 个商品的实例数在几十的量级，一次批量读比逐商品查省掉 N 次往返。

import (
	"context"
	"sort"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	projectdto "go_wp/internal/module/project/dto"
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

// ProductPublishRecordPage 发布记录整页的读模型。
//
// 失败档位与商品列表页一致：工程清单读不到 500；?project=<不存在的 id> 是 400；
// 商品列表或发布登记读不到 500（这一屏的每一行都是发布事实，静默跳过会让人以为
// 「那个商品没发布」）。
func (s *Service) ProductPublishRecordPage(ctx context.Context, req *productdto.ProductPublishRecordPageReq) (res *productdto.ProductPublishRecordPageResp, err error) {
	view := &productdto.ProductPublishRecordPageResp{
		Projects:     []projectdto.ProjectResp{},
		Records:      []productdto.PublishRecordRowResp{},
		Limit:        ProductPublishRecordPageSize,
		FilterStatus: "",
	}
	var firstErr error
	fail := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}

	projects, perr := s.project.List(ctx)
	if perr != nil {
		fail(perr)
	} else {
		view.Projects = projects
	}
	selected := ""
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected
	view.SiteName = projectName(projects, selected)
	if firstErr == nil && selected != "" && !ProjectExists(projects, selected) {
		fail(productenums.Err(productenums.ErrInvalidParam))
	}

	keyword, status, page := "", "", 1
	if req != nil {
		keyword = strings.TrimSpace(req.Keyword)
		status = normalizePublishStatus(req.Status)
		if req.Page > 0 {
			page = req.Page
		}
	}
	view.FilterKeyword = keyword
	view.FilterStatus = status
	view.Page = page
	// 端口未装配 = 这一屏没有数据源（装配缺陷）。不置 err：页面照常 200，
	// 只是给一句「发布能力未装配」—— 与「读不到」是两档。
	view.Ready = s.pagePublishRecords != nil
	if firstErr != nil || !view.Ready {
		return view, firstErr
	}

	// 状态筛选里 active / draft 是**发布状态**，与商品的 status 列无关，因此不能下推到
	// 商品表的 WHERE（那边没有这一列）。做法：把过滤放在合并之后，并按「过滤后的总数」
	// 分页 —— 先分页再过滤会出现「本页 0 行、总数 40」这种自相矛盾的组合。
	products, perr := s.listProductsForPublishRecord(ctx, selected, keyword)
	if perr != nil {
		return view, perr
	}
	if len(products) == 0 {
		view.Pagination = pagination.Page{Total: 0, Page: 1, Limit: view.Limit}
		return view, nil
	}
	ids := make([]string, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	langs, lerr := s.pagePublishRecords.Langs(ctx, selected, ids)
	if lerr != nil {
		return view, lerr
	}
	rows := mergePublishRows(products, langs, status)
	// 「共 N 条」= 合并（并按状态筛过）之后的行数：分页单位是行，不是商品表的 COUNT ——
	// 状态筛选掉一半时，用 COUNT 会让「共 40 条」与「本页 0 行」同时出现。
	view.Total = int64(len(rows))
	view.Page = clampPublishPage(page, view.Total, view.Limit)
	view.Records = pagePublishRows(rows, view.Page, view.Limit)
	view.Pagination = pagination.Page{Total: view.Total, Page: view.Page, Limit: view.Limit}
	return view, nil
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
// **stale 不参与这里**：失待重建的已上线商品仍是 active（线上有内容，只是旧的），
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

// projectName 取选中工程的名字（「站点」那一列的值）。
//
// 由读模型给而不是模板按 id 去 Projects 里找：Jet 没有「按条件取数组元素」的写法，
// 模板里 range 一遍工程去比 id 就是业务判断（templates/CLAUDE.md 禁）。
func projectName(projects []projectdto.ProjectResp, id string) string {
	for i := range projects {
		if projects[i].ID == id {
			return projects[i].Name
		}
	}
	return ""
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
