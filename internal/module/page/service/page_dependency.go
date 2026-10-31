package pageservice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) SourceType() string { return pipeline.SourceTypePage }

func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()

	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, herr := s.model.MarkStaleByDependency(ctx, projectID, kind, key, at)
		if herr != nil {
			return nil, herr
		}
		hit.add(ids)
	}
	affected := hit.list()

	s.logStaleImpact(ctx, "dependency:"+kind+":"+key, affected)
	return affected, nil
}

func (s *Service) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (int64, error) {
	if s == nil || s.model == nil {

		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	return s.model.MarkMenuStaleTx(ctx, tx, projectID, kind, at)
}

func (s *Service) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error) {
	if s == nil || s.model == nil {
		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	return s.model.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	_, err := s.rebuildStale(ctx, ids, false)
	if errors.Is(err, ErrRebuildOverloaded) {

		return nil
	}
	return err
}

func (s *Service) RebuildStaleStrict(ctx context.Context, ids []string) error {
	_, err := s.rebuildStale(ctx, ids, true)
	return err
}

func (s *Service) ListStalePageIDs(ctx context.Context, projectID string) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	return s.model.ListStaleIDs(ctx, strings.TrimSpace(projectID))
}

func (s *Service) rebuildStale(ctx context.Context, ids []string, strict bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	overloaded := false
	var overflowErr error
	if len(ids) > maxAutoRebuildPages {

		overflow := append([]string(nil), ids[maxAutoRebuildPages:]...)
		ids = ids[:maxAutoRebuildPages]
		overloaded = true
		if eerr := s.enqueueOverflowBuildJobs(ctx, overflow); eerr != nil {

			logger.Scene("dependency").With("count", len(overflow)).
				Error(eerr, "超限重建页面入队失败")
			overflowErr = fmt.Errorf("超限重建入队: %w", eerr)
		}
	}
	rebuilt, published := 0, 0
	for _, id := range ids {
		if ctx.Err() != nil {
			if strict {
				return rebuilt, ctx.Err()
			}
			return rebuilt, nil
		}

		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			logger.Scene("dependency").With("page_id", id).Warn("自动重建跳过：页面不存在或已删除")

			if strict {
				return rebuilt, fmt.Errorf("重建页面 %s: %w", id, err)
			}
			continue
		}

		plan, perr := s.planPageRebuild(ctx, page, nil, pagecontract.BuildIntentDependency)
		if perr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(perr, "自动重建跳过：站点语言清单或旧发布范围不可读（页面保持 stale）")

			s.markRebuildFailure(ctx, page, pageRebuildStagePlan)
			if strict {
				return rebuilt, fmt.Errorf("重建页面 %s（计划阶段）: %w", id, perr)
			}
			continue
		}
		r, p, rerr := s.rebuildPage(ctx, plan)
		rebuilt += r
		published += p
		if rerr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(rerr, "依赖失效后的自动重建失败（页面保持 stale）")
			s.markRebuildFailure(ctx, page, pageRebuildStageBuild)
			if strict {
				return rebuilt, fmt.Errorf("重建页面 %s: %w", id, rerr)
			}
			continue
		}
		s.clearRebuildFailure(ctx, page)
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).With("published", published).
			Info("依赖失效后的自动重建完成")
	}
	if overflowErr != nil {
		return rebuilt, overflowErr
	}
	if overloaded {
		return rebuilt, ErrRebuildOverloaded
	}
	return rebuilt, nil
}

func (s *Service) persistDependencies(ctx context.Context, projectID, pageID, artifactID string, deps []pipeline.Dependency) {
	rows, rerr := dependencyRows(pageID, artifactID, deps)
	if rerr != nil {
		return
	}
	if err := s.requireArtifactPage(ctx, pageID, artifactID); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Error(err, "依赖记录的产物归属校验失败（已降级，不影响构建结果）")
		return
	}
	if err := s.model.ReplaceDependencies(ctx, projectID, pageID, artifactID, rows); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Error(err, "依赖记录写入失败（已降级，不影响构建结果）")
	}
}

func (s *Service) persistDependenciesTx(ctx context.Context, tx *gorm.DB, projectID, pageID, artifactID string, deps []pipeline.Dependency) error {
	rows, rerr := dependencyRows(pageID, artifactID, deps)
	if rerr != nil {
		return rerr
	}
	if err := s.requireArtifactPage(ctx, pageID, artifactID); err != nil {
		return err
	}
	return s.model.ReplaceDependenciesTx(ctx, tx, projectID, pageID, artifactID, rows)
}

func (s *Service) requireArtifactPage(ctx context.Context, pageID, artifactID string) error {
	if s.pageArtifacts == nil {
		return errors.New("page: artifact 契约未注入，无法校验产物归属")
	}
	got, err := s.pageArtifacts.PageArtifactPageID(ctx, artifactID)
	if err != nil {
		return err
	}
	if got == "" || got != pageID {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Service) pageArtifactHashes(ctx context.Context) ([]string, error) {
	if s.pageArtifacts == nil {
		return nil, errors.New("page: artifact 契约未注入，孤儿对账退化为只按本模块清单判定")
	}
	return s.pageArtifacts.ListPageArtifactHashes(ctx)
}

func (s *Service) pageArtifactHashByID(ctx context.Context, id string) (string, error) {
	if s.pageArtifacts == nil {
		return "", errors.New("page: artifact 契约未注入")
	}
	return s.pageArtifacts.PageArtifactHashByID(ctx, id)
}

func dependencyRows(pageID, artifactID string, deps []pipeline.Dependency) ([]pagemodel.DependencyEntity, error) {
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil, nil
	}
	now := time.Now().UTC()
	rows := make([]pagemodel.DependencyEntity, 0, len(deps))
	seen := map[[2]string]bool{}
	for _, d := range deps {
		kind, key := strings.TrimSpace(d.Kind), strings.TrimSpace(d.Key)
		if kind == "" || key == "" {
			continue
		}

		if seen[[2]string{kind, key}] {
			continue
		}
		seen[[2]string{kind, key}] = true
		row := pagemodel.DependencyEntity{
			PageID: pageID, ArtifactID: artifactID,
			DependencyKind: kind, DependencyKey: key, LastChecked: now,
		}
		if rev := strings.TrimSpace(d.Revision); rev != "" {
			r := rev
			row.Revision = &r
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Service) persistDependenciesFromManifest(ctx context.Context, projectID, pageID, artifactID string, manifestJSON json.RawMessage) {
	if len(manifestJSON) == 0 {
		return
	}
	var m pipeline.Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Warn("产物 Manifest 解析失败，跳过依赖记录补写")
		return
	}
	s.persistDependencies(ctx, projectID, pageID, artifactID, m.Dependencies)
}

func (s *Service) pageDependencyKeys(ctx context.Context, page *pagemodel.PageEntity) []pipeline.Dependency {
	if page == nil {
		return nil
	}
	keys := map[[2]string]string{}
	add := func(k pipeline.DepKey) {
		keys[[2]string{k.Kind, k.Key}] = k.Key
	}

	if t := strings.TrimSpace(page.ContentTargetType); t != "" && t != "none" {
		if page.ContentTargetID != nil {
			if id := strings.TrimSpace(*page.ContentTargetID); id != "" {
				add(pipeline.DirectContentKey(t, id))
			}
		}
	}

	doc := page.DraftDocument
	if len(doc) > 0 {
		if parsed, err := builder.ParsePage(doc); err == nil {
			for _, id := range builder.ReferencedBlockIDs(parsed.Root) {
				add(pipeline.BlockKey(id))
			}

			for _, dep := range pipeline.StructureSlotDependencies(ctx, s.structureTemplates, page.ProjectID, parsed.Settings.Structure) {
				add(pipeline.DepKey{Kind: dep.Kind, Key: dep.Key})
			}
			for _, src := range s.collectionSourcesOf(ctx, parsed.Root) {
				add(pipeline.DepKey{Kind: pipeline.DepKindContentCollection, Key: "collection:" + src})
			}
		}
	}
	out := make([]pipeline.Dependency, 0, len(keys))
	for key := range keys {
		out = append(out, pipeline.Dependency{Kind: key[0], Key: key[1]})
	}
	return out
}

func (s *Service) collectionSourcesOf(ctx context.Context, roots []*core.Node) []string {
	seen := map[string]bool{}
	var out []string
	add := func(src string) {
		if src = strings.TrimSpace(src); src != "" && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}

	for _, src := range core.CollectionSourcesOf(roots) {
		add(src)
	}

	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	if asm != nil && len(asm.Specs) > 0 {
		resolver := plugincontract.AssemblyResolver(asm)
		var walk func(n *core.Node)
		walk = func(n *core.Node) {
			if n == nil {
				return
			}
			if spec, ok := resolver.LookupPluginComponent(n.Type); ok && spec != nil && spec.Collection != nil {
				add(spec.Collection.Source)
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		for _, r := range roots {
			walk(r)
		}
	}
	return out
}

const (
	pageRebuildStagePlan  = "plan"
	pageRebuildStageBuild = "build"
)

func (s *Service) markRebuildFailure(ctx context.Context, page *pagemodel.PageEntity, stage string) {
	if page == nil {
		return
	}
	if err := s.model.MarkRebuildFailure(ctx, page.ProjectID, page.ID, stage, time.Now()); err != nil {
		logger.Scene("dependency").With("page_id", page.ID).Error(err, "记录重建失败阶段失败（页面上的失败原因会缺失）")
	}
}

func (s *Service) clearRebuildFailure(ctx context.Context, page *pagemodel.PageEntity) {
	if page == nil {
		return
	}
	if err := s.model.ClearRebuildFailure(ctx, page.ProjectID, page.ID); err != nil {
		logger.Scene("dependency").With("page_id", page.ID).Error(err, "清除重建失败痕迹失败（该页可能继续显示上一次失败）")
	}
}

func (s *Service) FindPagesByDependency(ctx context.Context, projectID, dependencyKind, dependencyKey string) (
	refs []pagecontract.DependencyPageRef, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	pid, kind, key, err := normalizeDependencyLookup(projectID, dependencyKind, dependencyKey)
	if err != nil {
		return nil, err
	}
	if kind == "" || key == "" {

		return nil, nil
	}
	rows, lerr := s.model.ListPagesByDependency(ctx, pid, kind, key)
	if lerr != nil {
		return nil, lerr
	}
	return dependencyRefsOf(rows), nil
}

func normalizeDependencyLookup(projectID, dependencyKind, dependencyKey string) (pid, kind, key string, err error) {
	pid = strings.TrimSpace(projectID)
	if pid == "" {
		return "", "", "", ErrProjectRequired
	}

	return pid, strings.TrimSpace(dependencyKind), strings.TrimSpace(dependencyKey), nil
}

func dependencyRefsOf(rows []pagemodel.DependencyRefRow) []pagecontract.DependencyPageRef {
	if len(rows) == 0 {
		return nil
	}
	out := make([]pagecontract.DependencyPageRef, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		id := strings.TrimSpace(rows[i].ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, pagecontract.DependencyPageRef{
			ID: id, Title: strings.TrimSpace(rows[i].Title),
		})
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b pagecontract.DependencyPageRef) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

type staleIDCollector struct {
	seen map[string]bool
	ids  []string
}

func (c *staleIDCollector) add(hit []string) {
	for _, id := range hit {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if c.seen == nil {
			c.seen = make(map[string]bool, len(hit))
		}
		if c.seen[id] {
			continue
		}
		c.seen[id] = true
		c.ids = append(c.ids, id)
	}
}

func (c *staleIDCollector) list() []string { return c.ids }

const (
	staleImpactSampleLimit = 5

	staleImpactIDChunkSize = 500
)

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

func (s *Service) StaleImpactOfIDs(ctx context.Context, ids []string) *pagedto.StaleImpactSummaryResp {
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
		for _, chunk := range chunkIDs(uniq, staleImpactIDChunkSize) {
			rows, lerr := s.model.ListBriefsByIDs(ctx, projectID, chunk, limit, pagemodel.StaleOrderUpdateTime, true)
			if lerr != nil {

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
	return &pagedto.StaleImpactSummaryResp{
		Total:     len(uniq),
		Pages:     pages,
		Limit:     limit,
		Truncated: len(uniq) > len(pages),
	}
}

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

type staleImpactLogPlan struct {
	Total int

	NoSample bool

	Sample string

	SampleShown int

	Truncated bool

	Message string
}

func planStaleImpactLog(affected int, impact *pagedto.StaleImpactSummaryResp) staleImpactLogPlan {
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

func (s *Service) logStaleImpact(ctx context.Context, reason string, ids []string) {
	if s == nil {
		return
	}

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

		entry.Info(plan.Message)
		return
	}
	entry.With("truncated", plan.Truncated).With("sample", plan.Sample).Info(plan.Message)
}

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

func mergeStaleRows(parts [][]pagemodel.StalePageRow, limit int, orderBy string, descending bool) []pagemodel.StalePageRow {
	total := 0
	for i := range parts {
		total += len(parts[i])
	}
	all := make([]pagemodel.StalePageRow, 0, total)
	for i := range parts {
		all = append(all, parts[i]...)
	}
	slices.SortStableFunc(all, func(a, b pagemodel.StalePageRow) int {
		switch {
		case staleRowLess(a, b, orderBy, descending):
			return -1
		case staleRowLess(b, a, orderBy, descending):
			return 1
		default:
			return 0
		}
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

func staleRowLess(a, b pagemodel.StalePageRow, orderBy string, descending bool) bool {
	var less, equal bool
	switch orderBy {
	case pagemodel.StaleOrderDraftPath:
		less, equal = a.DraftPath < b.DraftPath, a.DraftPath == b.DraftPath
	case pagemodel.StaleOrderTitle:
		less, equal = a.Title < b.Title, a.Title == b.Title
	case pagemodel.StaleOrderID:
		less, equal = a.ID < b.ID, a.ID == b.ID
	default:
		less, equal = a.UpdatedAt.Before(b.UpdatedAt), a.UpdatedAt.Equal(b.UpdatedAt)
	}
	if equal {

		return a.ID < b.ID
	}
	if descending {
		return !less
	}
	return less
}

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

		RebuildFailedAt:    failedAtDTO(row.RebuildFailedAt),
		RebuildFailedStage: strings.TrimSpace(derefString(row.RebuildFailedStage)),
	}
}

func failedAtDTO(at *time.Time) *utils.JSONTime {
	if at == nil || at.IsZero() {
		return nil
	}
	v := utils.NewJSONTime(*at)
	return &v
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func staleDisplayPath(row pagemodel.StalePageRow) string {
	if row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != "" {
		return strings.TrimSpace(*row.ActivePath)
	}
	if p := strings.TrimSpace(row.DraftPath); p != "" {
		return p
	}
	return row.ID
}

func stalePublished(row pagemodel.StalePageRow) bool {
	return row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != ""
}

var _ mediacontract.StaleMarker = (*Service)(nil)

func (s *Service) RefKinds() []string { return []string{mediacontract.RefKindPage} }

func (s *Service) MarkStaleByMediaRefs(ctx context.Context, refs []mediacontract.MediaRef) (marked []string, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for i := range refs {
		if strings.TrimSpace(refs[i].Kind) != mediacontract.RefKindPage {
			continue
		}
		id := strings.TrimSpace(refs[i].ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	collector := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return collector.list(), ctx.Err()
		}
		hit, merr := s.model.MarkStaleByIDs(ctx, projectID, ids, at)
		if merr != nil {
			return nil, merr
		}
		collector.add(hit)
	}

	s.logStaleImpact(ctx, "media_replace", collector.list())
	return collector.list(), nil
}
