package pageservice

// page_stale_overview.go — 待重建页面的**只读反查面**（service 层）。
//
// 与 model.ListStale 的分工：model 管「一个工程内按参数取一段」，
// 本文件管「跨工程合并 + 全局排序 + 截断 + 计数」。跨工程只能逐工程各设一次
// 作用域（pages 在迁移 215 里带 FORCE 策略，作用域是单值会话变量，合并多工程
// 到一个查询只能靠放宽谓词，那等于取消隔离 —— 见 page_scope.go 的论证）。
//
// 两个消费者：
//   1. 后台只读展示（/admin/pages 的待重建区块）：ListStalePages —— 全站清单，
//      按标记时间倒序，「最近这次改动影响的」排在最前面；
//   2. 写侧的回执与结构化日志：StaleImpactOfIDs —— 把扇出返回的 id 集合
//      （跨工程）翻译成「N 个页面 + 最多 K 条标题 / 路径」的人类可读摘要。
//
// 一致性：两个消费者共用同一份取数口径（同一组 model 方法、同一份可读标识规则、
// 同一组 limit 归一化常量），不在各自的调用点上另抄一遍。

import (
	"context"
	"sort"
	"strconv"
	"strings"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// staleOverviewLimit 列表页只读区块一次列出的页面数。
	//
	// 只读区块的作用是「让人看见影响面」，不是完整清单：给多了会把列表页淹掉，
	// 给少了又不说明被截断。被截断的条数由 Total 给出并在页面上显式说明。
	staleOverviewLimit = 8
	// staleImpactSampleLimit 写侧日志 / 回执里最多列出的页面数（前 K 个）。
	staleImpactSampleLimit = 5
)

// ListStalePages 只读反查：列出待重建页面（ProjectID 为空 = 全部工程）。
//
// 失败即失败（不降级成空清单）：「影响面 0」与「读不到影响面」是两件事，
// 混在一起会让运营把一次读取失败当成「没有待重建页面」然后放心地不看。
// 调用方（页面 handler）自己决定怎么显示这个失败 —— 它有不降级的权限，
// 只读面没有替它撒谎的权限。
func (s *Service) ListStalePages(ctx context.Context, req *pagedto.StalePageListReq) (res *pagedto.StalePageListResp, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	if req == nil {
		req = &pagedto.StalePageListReq{}
	}
	orderBy, oerr := pagemodel.NormalizeStaleOrder(req.OrderBy)
	if oerr != nil {
		return nil, ErrInvalidParam
	}
	limit := pagemodel.NormalizeStaleListLimit(req.Limit)

	projectIDs, names, err := s.staleProjectScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	parts := make([][]pagemodel.StalePageRow, 0, len(projectIDs))
	total := 0
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// 每工程取满 limit 条：全局前 limit 条一定落在「各工程前 limit 条」的并集里，
		// 所以逐工程截断后再全局排序不会丢行 —— 而一次读全站会把整张表拉进内存。
		rows, lerr := s.model.ListStale(ctx, projectID, limit, orderBy, req.Descending)
		if lerr != nil {
			return nil, lerr
		}
		n, cerr := s.model.CountStale(ctx, projectID)
		if cerr != nil {
			return nil, cerr
		}
		total += int(n)
		parts = append(parts, rows)
	}

	merged := mergeStaleRows(parts, limit, orderBy, req.Descending)
	pages := make([]pagedto.StalePageResp, 0, len(merged))
	for i := range merged {
		pages = append(pages, staleRowToDTO(merged[i], names))
	}
	return &pagedto.StalePageListResp{
		Pages:     pages,
		Total:     total,
		Limit:     limit,
		Truncated: total > len(pages),
	}, nil
}

// StaleImpactOfIDs 把**一次写操作**的受影响页面 id 集合翻译成影响面摘要
// （总数 + 最多 staleImpactSampleLimit 条标题 / 路径），供写侧回执与结构化日志共用。
//
// 为什么放在 service 而不是让每个写侧自己查：标题的取数口径（文档 SEO 段）、
// 可读标识的回落规则、跨工程的作用域拆解都只有一份，写侧复用同一份实现；
// 每个写侧各写一遍的结果，是这个页面在不同日志行里显示成三个名字。
//
// 返回 nil 表示「摘要不可用」（工程清单读不到 / 查询失败 / 空集合）——
// **不是错误**：这是观测，不是业务结果。内容已经写成功了，一次只读查询失败
// 绝不能反向影响写入结论；调用方按「没有摘要」继续即可。
func (s *Service) StaleImpactOfIDs(ctx context.Context, ids []string) *pagedto.StaleImpactSummary {
	if s == nil || s.model == nil || len(ids) == 0 {
		return nil
	}
	projectIDs, names, err := s.staleProjectScope(ctx, "")
	if err != nil {
		return nil
	}
	limit := staleImpactSampleLimit
	parts := make([][]pagemodel.StalePageRow, 0, len(projectIDs))
	seen := map[string]bool{}
	uniq := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil
	}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil
		}
		rows, lerr := s.model.ListBriefsByIDs(ctx, projectID, uniq, limit, pagemodel.StaleOrderUpdateTime, true)
		if lerr != nil {
			// 单个工程读失败即整体放弃：给出一份少了某个工程的摘要，
			// 比不给摘要更容易误导（读者会以为那就是全部）。
			return nil
		}
		parts = append(parts, rows)
	}
	merged := mergeStaleRows(parts, limit, pagemodel.StaleOrderUpdateTime, true)
	pages := make([]pagedto.StalePageResp, 0, len(merged))
	for i := range merged {
		pages = append(pages, staleRowToDTO(merged[i], names))
	}
	return &pagedto.StaleImpactSummary{
		Total:     len(uniq),
		Pages:     pages,
		Limit:     limit,
		Truncated: len(uniq) > len(pages),
	}
}

// logStaleImpact 记一条「本次影响 N 个页面（最多列前 K 个：标题 / 路径）」的结构化日志。
//
// 通道复用现有的 logger（scene=dependency），不新造回执通道；文案只有这一份。
// 摘要取不到时降级为「只记条数」，写侧主流程（返回的 ids / error）完全不受影响。
func (s *Service) logStaleImpact(ctx context.Context, reason string, ids []string) {
	if s == nil || len(ids) == 0 {
		return
	}
	entry := logger.Scene("dependency").
		With("reason", reason).
		With("affected", len(ids))
	impact := s.StaleImpactOfIDs(ctx, ids)
	if impact == nil {
		// 摘要不可用：至少留下条数与场景，不让「谁受影响了」整体消失。
		entry.With("sample_limit", staleImpactSampleLimit).
			Info("依赖失效：本次影响 " + strconv.Itoa(len(ids)) + " 个页面（影响面摘要不可用，仅记条数）")
		return
	}
	entry.With("sample_limit", impact.Limit).
		With("truncated", impact.Truncated).
		With("sample", formatStaleImpactSample(impact.Pages)).
		Info("依赖失效：本次影响 " + strconv.Itoa(impact.Total) +
			" 个页面（前 " + strconv.Itoa(len(impact.Pages)) + " 个：标题 / 路径）")
}

// formatStaleImpactSample 把影响面样本压成一行（标题优先、回落路径）。
//
// 纯函数便于单测。标题缺失时**只给路径**，不编「（无标题）」这类占位 ——
// 日志里那句话不是给人看的界面文案，多一个常量不会让任何东西更清楚。
func formatStaleImpactSample(pages []pagedto.StalePageResp) string {
	parts := make([]string, 0, len(pages))
	for i := range pages {
		label := strings.TrimSpace(pages[i].Title)
		path := strings.TrimSpace(pages[i].Path)
		switch {
		case label == "":
			label = path
		case path != "" && path != label:
			label = label + " (" + path + ")"
		}
		if label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, "; ")
}

// staleProjectScope 返回本次反查要覆盖的工程清单与工程名映射。
//
// projectID 非空 = 只查该工程（调用方自带作用域，不再按工程表扇出）；
// 空 = 全部工程（逐工程各设一次作用域，见 page_scope.go）。
func (s *Service) staleProjectScope(ctx context.Context, projectID string) (ids []string, names map[string]string, err error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return []string{pid}, s.staleProjectNames(ctx), nil
	}
	ids, err = s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return ids, s.staleProjectNames(ctx), nil
}

// staleProjectNames 工程 id → 名称。
//
// 读不到时返回空 map（不是错误）：工程名只是展示加成，缺了它清单仍然可用
// （路径本身就是页面唯一稳定的标识）；但反过来，让一次只读反查因为
// 「拿不到工程名」而整体失败，是把装饰当成了数据。
func (s *Service) staleProjectNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s == nil || s.project == nil {
		return out
	}
	list, err := s.project.List(ctx)
	if err != nil {
		logger.Scene("page").Error(err, "读取工程列表失败，待重建清单不显示工程名")
		return out
	}
	for i := range list {
		out[list[i].ID] = list[i].Name
	}
	return out
}

// mergeStaleRows 把各工程已排序的行合并为全局有序的前 limit 条（纯函数，便于单测）。
//
// 各工程片段内部已有序，但全局次序需要重排：A 工程第 3 条可能比 B 工程第 1 条更近。
// 比较口径与 model 的 ORDER BY 相同（staleRowLess 的注释里写了为什么必须一致）。
func mergeStaleRows(parts [][]pagemodel.StalePageRow, limit int, orderBy string, descending bool) []pagemodel.StalePageRow {
	total := 0
	for i := range parts {
		total += len(parts[i])
	}
	all := make([]pagemodel.StalePageRow, 0, total)
	for i := range parts {
		all = append(all, parts[i]...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return staleRowLess(all[i], all[j], orderBy, descending)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// staleRowLess 行的全局排序比较：与 model 的 ORDER BY 同一口径（列 + 方向 + id 兜底）。
//
// 为什么必须与 SQL 一致：逐工程查询按 SQL 排序取前 limit 条，全局重排按这个函数 ——
// 两者不一致时「每工程的前 limit 条」与「全局前 limit 条」对不上，清单会漏行，
// 而且漏得没有规律（取决于哪个工程先被遍历）。已知的一处偏差：PG 的默认 collation
// 与 Go 的字节序在非 ASCII 文本上可能给出不同次序，页面路径以 ASCII 为主，影响限于
// 同长度前缀的边界情形 —— 记录在此，不假装没有。
func staleRowLess(a, b pagemodel.StalePageRow, orderBy string, descending bool) bool {
	var less, equal bool
	switch orderBy {
	case pagemodel.StaleOrderDraftPath:
		less, equal = a.DraftPath < b.DraftPath, a.DraftPath == b.DraftPath
	case pagemodel.StaleOrderTitle:
		less, equal = a.Title < b.Title, a.Title == b.Title
	case pagemodel.StaleOrderID:
		less, equal = a.ID < b.ID, a.ID == b.ID
	default: // pagemodel.StaleOrderUpdateTime
		less, equal = a.UpdatedAt.Before(b.UpdatedAt), a.UpdatedAt.Equal(b.UpdatedAt)
	}
	if equal {
		// 与 SQL 的 ", id ASC" 对齐：同值行的次序由主键兜底，避免两次读取次序不同。
		return a.ID < b.ID
	}
	if descending {
		return !less
	}
	return less
}

// staleRowToDTO 行 → 只读投影（填入工程名与可读标识）。
func staleRowToDTO(row pagemodel.StalePageRow, names map[string]string) pagedto.StalePageResp {
	return pagedto.StalePageResp{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		ProjectName: names[strings.TrimSpace(row.ProjectID)],
		Title:       strings.TrimSpace(row.Title),
		Path:        staleDisplayPath(row),
		Published:   stalePublished(row),
		Stale:       row.Stale,
		UpdatedAt:   utils.NewJSONTime(row.UpdatedAt),
	}
}

// staleDisplayPath 页面的可读标识：已上线路径优先，其次草稿路径，都没有时退回 id。
//
// 与 content 的 stalePagePath / block 的 blockStalePagePath 是同一口径（三处显示同一
// 个页面时必须同名）；那两处在各自的 handler 里、不在本次改动范围内，因此这里是
// page 侧的那一份 —— 三份口径的一致性由注释与 hand-off 记录，不静默分叉。
//
// 不编造路径：一个不存在的 URL 比一串 id 更误导人。
func staleDisplayPath(row pagemodel.StalePageRow) string {
	if row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != "" {
		return strings.TrimSpace(*row.ActivePath)
	}
	if p := strings.TrimSpace(row.DraftPath); p != "" {
		return p
	}
	return row.ID
}

// stalePublished 是否已上线（有活跃路径）。
func stalePublished(row pagemodel.StalePageRow) bool {
	return row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != ""
}
