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
	// staleImpactSampleLimit 写侧日志 / 回执里最多列出的页面数（前 K 个）。
	staleImpactSampleLimit = 5
	// staleImpactIDChunkSize 影响面反查时单条 IN 查询最多带多少个 id。
	//
	// 整站标记（主题 / 块 / 词条）一次就能返回成千上万个 id：500 远低于 PostgreSQL 的
	// 参数上限 65535，同时把单条 SQL 的 IN 元素规模压在一个 planner 处理起来很便宜的
	// 量级（分块的正确性与取舍见 StaleImpactOfIDs 的注释）。
	staleImpactIDChunkSize = 500
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
	// 分块 IN 查询（整站标记接影响面回执时新增的规模处理）：
	//
	// 整站标记一次就能返回成千上万个 id，而这里是一条 `WHERE id IN (…)` —— PG 的参数
	// 上限是 65535，且单个 IN 的元素越多，planner 为它构造的表达式与内存占用增长越快。
	// 每块最多 staleImpactIDChunkSize 个，块内取前 limit 条，最后与其它块一起全局归并。
	//
	// 为什么选「分块」而不是「超过阈值就降级成只记条数 + 按标记时间取样本」：
	//   · 分块后的样本仍然**精确属于本次 id 集合**；降级法给出的样本是按时间近似的，
	//     读者无法区分「这次改动影响的页」与「同一时刻被别的改动标记的页」——
	//     而整站标记本身正是「一次改动标记全站」，样本里混进别处的概率不低；
	//   · 降级法要新增一条「按标记时间取样本」的取数口径，那等于给「样本从哪来」
	//     留下两个答案（本包刻意只保留一份：id 集合反查 + 同一份排序 / 截断）。
	// 代价：查询条数从 1 条变成 ceil(N/500) 条（每块都是主键索引 + LIMIT K）。整站标记
	// 本身就要逐工程 UPDATE 一次全站，这点只读开销属于同一量级，且只发生在罕见的
	// 文案 / 主题变更路径上；失败语义不变（任一块读失败即整体放弃，见下）。
	//
	// 归并的正确性：全局前 K 条一定落在「各块前 K 条」的并集里（标准 top-K 归并性质），
	// 所以分块不会让样本变样。
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil
		}
		for _, chunk := range chunkIDs(uniq, staleImpactIDChunkSize) {
			rows, lerr := s.model.ListBriefsByIDs(ctx, projectID, chunk, limit, pagemodel.StaleOrderUpdateTime, true)
			if lerr != nil {
				// 单个工程（或其中一块）读失败即整体放弃：给出一份少了某个工程、
				// 或某一块的摘要，比不给摘要更容易误导（读者会以为那就是全部）。
				return nil
			}
			parts = append(parts, rows)
		}
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

// chunkIDs 把 id 集合切成每块最多 size 个（纯函数，便于单测）。
//
// 分块本身不会报错：切错了只会让样本少几行 —— 而「影响面样本少了几行」正是这一批
// 要消灭的那种静默偏差，所以它单独被测。
//
// 保持原顺序（块内 = 输入次序，块 = 输入次序）：样本的最终次序由 mergeStaleRows 按
// 标记时间重排，与分块次序无关；保序只是为了「同一个 id 集合每次都得到同一组 SQL」。
//
// size <= 0（调用方给了不合法的值）时退回单块，**不静默丢 id**：丢 id 在本函数的语义里
// 等于「这些页面不存在」，那是最不该出现的失败形态。
func chunkIDs(ids []string, size int) [][]string {
	if len(ids) == 0 {
		return nil
	}
	if size <= 0 || len(ids) <= size {
		return [][]string{ids}
	}
	out := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}

// staleImpactLogPlan 一次影响面日志的「写什么」（纯数据，便于单测）。
type staleImpactLogPlan struct {
	// Total 本次受影响的页面数（去重后的 id 条数）。
	Total int
	// NoSample 摘要不可用：这一次只能记条数。
	NoSample bool
	// Sample 人类可读样本（标题优先、回落路径）；NoSample 时为空。
	Sample string
	// SampleShown 样本条数。
	SampleShown int
	// Truncated 样本被截断（总数大于样本条数）。
	Truncated bool
	// Message 日志正文（唯一一份文案）。
	Message string
}

// planStaleImpactLog 决定这次记什么（纯函数，便于单测）。
//
// 总数以调用方给的 affected 为准，而不是 impact.Total：affected 是**已经发生的事实**
// （这一次扇出真正返回了多少个不同页面），而 summary 是一次只读反查的产物 ——
// 反查少了几行不该把日志里的总数也改小，那会让「日志说 3、实际标了 8」这种偏差
// 永远查不出来。两者的口径由调用方保证一致（同一个归一化后的 id 集合）。
func planStaleImpactLog(affected int, impact *pagedto.StaleImpactSummary) staleImpactLogPlan {
	plan := staleImpactLogPlan{Total: affected}
	if impact == nil {
		plan.NoSample = true
		plan.Message = "依赖失效：本次影响 " + strconv.Itoa(affected) + " 个页面（影响面摘要不可用，仅记条数）"
		return plan
	}
	plan.Sample = formatStaleImpactSample(impact.Pages)
	plan.SampleShown = len(impact.Pages)
	plan.Truncated = impact.Truncated
	plan.Message = "依赖失效：本次影响 " + strconv.Itoa(affected) +
		" 个页面（前 " + strconv.Itoa(plan.SampleShown) + " 个：标题 / 路径）"
	return plan
}

// logStaleImpact 记一条「本次影响 N 个页面（最多列前 K 个：标题 / 路径）」的结构化日志。
//
// 通道复用现有的 logger（scene=dependency），不新造回执通道；文案只有这一份
// （planStaleImpactLog）。摘要取不到时降级为「只记条数」；写侧主流程（返回的 ids / error）
// 完全不受影响 —— 这是观测，不是业务结果。
//
// 边界（写清是为了让后来者知道这条日志能当什么证据、不能当什么）：
//   - 空集合（或全是空白 id）**不记**：那不是「影响面为 0 个页面」，是「这次什么都没被
//     标记」；每次整站标记都留一行「影响 0 个页面」只会把 dependency 场景淹掉；
//   - 调用方因 ctx 取消提前中断扇出时，这里的 affected 是**已经标记掉的那部分**，
//     不是「本应标记的全部」—— 日志只陈述已发生的事实，不替调用方承诺完整范围；
//   - 样本是按**本次 id 集合**反查出来的（不是按「谁刚变成 stale」猜的），所以它不会
//     混进别的改动标记的页面；整站规模下的取数代价与取舍见 StaleImpactOfIDs 的注释。
func (s *Service) logStaleImpact(ctx context.Context, reason string, ids []string) {
	if s == nil {
		return
	}
	// 归一化复用逐工程聚合那一份实现（staleIDCollector）：口径分叉的表现是
	// 「日志里的总数」与「摘要里的总数」对不上，而两处都只在日志里看得见。
	c := &staleIDCollector{}
	c.add(ids)
	uniq := c.list()
	if len(uniq) == 0 {
		return
	}
	plan := planStaleImpactLog(len(uniq), s.StaleImpactOfIDs(ctx, uniq))
	entry := logger.Scene("dependency").
		With("reason", reason).
		With("affected", plan.Total).
		With("sample_limit", staleImpactSampleLimit)
	if plan.NoSample {
		// 摘要不可用：至少留下条数与场景，不让「谁受影响了」整体消失。
		entry.Info(plan.Message)
		return
	}
	entry.With("truncated", plan.Truncated).With("sample", plan.Sample).Info(plan.Message)
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
