package pageservice

// page_redirect.go — 重定向管理（审计 SEO-025）。
//
// 问题：改 URL 时可以勾「保留旧链接」，系统会落一份 redirect.json 并激活到旧路径
//（SiteRedirectMiddleware 生效）。但没有任何界面能看见这些 301，也无法手动增删 ——
// 改十次 URL 就留下十条链，A→B、B→C 越积越长，没人知道线上到底有多少条、指向哪。
//
// 本文件提供四件事：
//  1. 列出工程下**全部**重定向（DB 占用账 + 访问面 redirect.json 事实合并成一行）；
//  2. 手动新增（营销短链 / 站内路径归并），校验源空闲、目标存在、不成环；
//  3. 删除（解除访问面激活 + 清占用账）；
//  4. 多跳链合并（A→B、B→C 合并成 A→C，一次跳转到位）。
//
// 事实来源的分工（与 SiteRedirectMiddleware 同一口径）：**访问面才是真源** ——
// 中间件只看「激活链接的目标目录里有没有 redirect.json」。所以列表里每条的目标路径
// 都从 Inspect 读，DB 行只回答「这条路径被登记为重定向、归属谁」。两者不一致时
//（有账无产物）条目显式为「未生效」，绝不假装生效。
//
// 成环与多跳的判定算法见 resolveRedirectTail。

import (
	"context"
	"errors"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"

	"gorm.io/gorm"
)

// maxRedirectHops 链长上限：visited 已保证遍历终止，这个上限只用来把「脏数据造成的
// 超长链」与「正常的两三跳」区分开 —— 超过按成环处理，拒绝写入。
const maxRedirectHops = 32

// 归属者类型（page_routes 的两个归属列二选一，见 init_builder_schema.sql 的 CHECK）。
const (
	redirectOwnerPage         = "page"
	redirectOwnerPresentation = "presentation"
)

// redirectRecord 一条重定向的合并事实（DB 占用账 + 访问面 redirect.json）。
type redirectRecord struct {
	sourcePath string
	// targetPath 为空 = 访问面上没有 redirect.json（未生效）。
	targetPath string
	statusCode int
	effective  bool
	ownerKind  string
	ownerID    string
	updatedAt  time.Time
}

// ListRedirects 列出工程下全部重定向（含未生效条目与链状态标记）。
//
// 工程为空时回落到第一个工程：后台页面首次打开不带 query 参数，
// 直接空白页会让人以为「一条重定向都没有」。
func (s *Service) ListRedirects(ctx context.Context, req *pagedto.RedirectListReq) (res *pagedto.RedirectListResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	res = &pagedto.RedirectListResp{Projects: []pagedto.RedirectProjectOption{}, Items: []pagedto.RedirectItem{}}
	if s.project != nil {
		projects, lerr := s.project.List(ctx)
		if lerr != nil {
			return nil, lerr
		}
		for _, p := range projects {
			res.Projects = append(res.Projects, pagedto.RedirectProjectOption{ID: p.ID, Name: p.Name})
		}
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" && len(res.Projects) > 0 {
		projectID = res.Projects[0].ID
	}
	res.ProjectID = projectID
	if projectID == "" {
		return res, nil
	}

	records, order, err := s.redirectRecords(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ownerLabels, err := s.redirectOwnerLabels(ctx, records)
	if err != nil {
		return nil, err
	}
	for _, source := range order {
		item := buildRedirectItem(records, source, ownerLabels)
		res.Items = append(res.Items, item)
		res.Total++
		if item.Effective {
			res.EffectiveCount++
		}
		if item.MultiHop {
			res.MultiHopCount++
		}
		if item.Loop {
			res.LoopCount++
		}
	}
	return res, nil
}

// CreateRedirect 手动新增一条重定向。
//
// 校验链（每条都对应一个真实会出事的场景）：
//  1. 源路径空闲 —— 已被 active/reserved/redirect 任一行占用时写入会撞 (project_id, path)
//     唯一约束，或更糟：覆盖一条别人正在服务的路径；
//  2. 目标路径存在 —— 目标是站内已达路径，指向不存在的路径等于把 404 换了层皮；
//  3. 不成环 —— A→B→…→A 在浏览器上是 ERR_TOO_MANY_REDIRECTS；
//  4. 目标若本身是重定向源，落点直接取链尾 —— 新加的这条不该是多跳链的中间环节。
func (s *Service) CreateRedirect(ctx context.Context, req *pagedto.RedirectCreateReq) (res *pagedto.RedirectItem, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if s.routes == nil || s.publication == nil || s.store == nil {
		return nil, ErrRedirectUnavailable
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, ErrInvalidParam
	}
	source, err := pipeline.NormalizeURL(strings.TrimSpace(req.SourcePath))
	if err != nil || source == "/" {
		return nil, ErrInvalidPath
	}
	target, err := pipeline.NormalizeURL(strings.TrimSpace(req.TargetPath))
	if err != nil || target == "/" {
		return nil, ErrInvalidPath
	}
	if source == target {
		return nil, ErrRedirectLoop
	}
	occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{ProjectID: projectID, Path: source})
	if err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrRedirectOccupied
	}
	records, _, err := s.redirectRecords(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tail, err := resolveRedirectTail(records, source, target)
	if err != nil {
		return nil, err
	}
	owner, err := s.model.GetActiveRouteByPath(ctx, projectID, tail)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRedirectTargetMiss
		}
		return nil, err
	}
	// 顺序：先登记占用（DB），再落产物并激活（FS）。
	// 反过来的话，激活成功而登记失败会留下「线上 301 生效、路由表却空着」的隐形态 ——
	// 该路径可被别的页面抢占，冲突要到线上才暴露。登记成功而激活失败则是可见的半成品
	//（列表显示「未生效」），删掉重试即可，所以回滚的那一侧选 FS。
	redirectReq := &pubcontract.RedirectReq{ProjectID: projectID, OldPath: source}
	if owner.PageID != nil {
		redirectReq.PageID = *owner.PageID
	} else if owner.PresentationID != nil {
		redirectReq.PresentationID = *owner.PresentationID
	} else {
		// 路由行两个归属列都空不是合法状态（表 CHECK 不允许），到这里说明数据已异常。
		return nil, ErrRedirectTargetMiss
	}
	if _, err = s.routes.Redirect(ctx, redirectReq); err != nil {
		return nil, err
	}
	if werr := s.writeRedirectArtifact(source, tail); werr != nil {
		if _, derr := s.model.DeleteRedirectRoute(ctx, projectID, source); derr != nil {
			logger.Scene("page").With("path", source).Error(derr, "重定向回滚失败：占用行残留，需人工清理")
		}
		return nil, werr
	}
	logger.Scene("page").With("source", source).With("target", tail).Info("重定向已新增")
	// 直接构造返回条目，不复用 buildRedirectItem：它的输入是「列表读取时的快照」，
	// 而这条重定向刚刚才写进去，快照里没有它。
	return &pagedto.RedirectItem{
		SourcePath: source,
		TargetPath: tail,
		StatusCode: 301,
		Effective:  true,
		OwnerKind:  ownerKindOf(owner),
		OwnerID:    ownerIDOf(owner),
		UpdatedAt:  time.Now().Local().Format(utils.LayoutSecond),
	}, nil
}

// DeleteRedirect 删除一条重定向：先解除访问面激活，再清占用账。
//
// 顺序不能反：先删账的话，一旦解激活失败就再也查不到「该清哪个链接」，
// 线上会残留一条只有符号链接知道的重定向（page 删除路径踩过同一个坑）。
func (s *Service) DeleteRedirect(ctx context.Context, req *pagedto.RedirectDeleteReq) (err error) {
	if req == nil {
		return ErrInvalidParam
	}
	if s.publication == nil {
		return ErrRedirectUnavailable
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return ErrInvalidParam
	}
	path, err := pipeline.NormalizeURL(strings.TrimSpace(req.Path))
	if err != nil {
		return ErrInvalidPath
	}
	if _, err = s.model.GetRedirectRoute(ctx, projectID, path); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRedirectNotFound
		}
		return err
	}
	if err = s.publication.Deactivate(path); err != nil {
		return err
	}
	if _, err = s.model.DeleteRedirectRoute(ctx, projectID, path); err != nil {
		return err
	}
	logger.Scene("page").With("path", path).Info("重定向已删除")
	return nil
}

// MergeRedirectChain 把一条重定向的跳转链合并为直达（A→B、B→C 合成 A→C）。
//
// 只改访问面产物（重写 redirect.json 并重新激活）：链的形态本来就只存在于产物里，
// DB 行只记「这条路径是重定向」，不存目标 —— 不需要额外迁移就能合并。
// 已经是直达时幂等返回（重复点一下不该报错）。
func (s *Service) MergeRedirectChain(ctx context.Context, req *pagedto.RedirectMergeReq) (res *pagedto.RedirectItem, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if s.publication == nil || s.store == nil {
		return nil, ErrRedirectUnavailable
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, ErrInvalidParam
	}
	path, err := pipeline.NormalizeURL(strings.TrimSpace(req.Path))
	if err != nil {
		return nil, ErrInvalidPath
	}
	records, _, err := s.redirectRecords(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rec, ok := records[path]
	if !ok {
		return nil, ErrRedirectNotFound
	}
	if !rec.effective || strings.TrimSpace(rec.targetPath) == "" {
		return nil, ErrRedirectTargetMiss
	}
	tail, err := resolveRedirectTail(records, path, rec.targetPath)
	if err != nil {
		return nil, err
	}
	if tail == rec.targetPath {
		item := buildRedirectItem(records, path, nil)
		return &item, nil
	}
	// 链尾必须真的存在：中间几跳把流量导向一条空路径时，合并等于把「多跳后 404」
	// 提前成「一跳后 404」，看起来修好了其实没有。
	if _, err = s.model.GetActiveRouteByPath(ctx, projectID, tail); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRedirectTargetMiss
		}
		return nil, err
	}
	if err = s.writeRedirectArtifact(path, tail); err != nil {
		return nil, err
	}
	logger.Scene("page").With("source", path).With("from", rec.targetPath).With("to", tail).Info("重定向链已合并")
	// 返回合并**之后**的状态：buildRedirectItem 读的是合并前快照，这条链在那份快照里
	// 仍是多跳的，直接返回会把「已经合并好了」报成「还是多跳」。
	item := buildRedirectItem(records, path, nil)
	item.TargetPath = tail
	item.FinalPath = ""
	item.MultiHop = false
	item.Loop = false
	item.StatusCode = 301
	item.Effective = true
	return &item, nil
}

// redirectRecords 读取工程下全部重定向行并与访问面事实合并（路径升序）。
func (s *Service) redirectRecords(ctx context.Context, projectID string) (recs map[string]*redirectRecord, order []string, err error) {
	rows, err := s.model.ListRedirectRoutes(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	recs = make(map[string]*redirectRecord, len(rows))
	order = make([]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		rec := &redirectRecord{sourcePath: row.Path, updatedAt: row.UpdatedAt}
		rec.ownerKind, rec.ownerID = ownerKindOf(&row), ownerIDOf(&row)
		if s.publication != nil {
			switch st, ierr := s.publication.Inspect(row.Path); {
			case ierr != nil:
				// 激活链接不可达（产物被删 / 链接损坏）：按未生效展示，但不静默 ——
				// 「账上有、线上没有」正是这一页要暴露的问题之一。
				logger.Scene("page").With("path", row.Path).Warn("重定向激活状态读取失败：" + ierr.Error())
			case st != nil && st.Kind == pipeline.PublicationRedirect && st.Redirect != nil:
				rec.effective = true
				rec.targetPath = st.Redirect.TargetPath
				rec.statusCode = st.Redirect.StatusCode
			}
		}
		recs[row.Path] = rec
		order = append(order, row.Path)
	}
	return recs, order, nil
}

// redirectOwnerLabels 把页面归属者映射成可读标识（页面没有标题列，用草稿路径辨识）。
func (s *Service) redirectOwnerLabels(ctx context.Context, recs map[string]*redirectRecord) (labels map[string]string, err error) {
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		if rec.ownerKind == redirectOwnerPage && rec.ownerID != "" {
			ids = append(ids, rec.ownerID)
		}
	}
	labels = map[string]string{}
	if len(ids) == 0 {
		return labels, nil
	}
	pages, err := s.model.FindPagesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range pages {
		labels[pages[i].ID] = pages[i].DraftPath
	}
	return labels, nil
}

// writeRedirectArtifact 落盘 301 产物并激活到源路径（覆盖既有链接，幂等）。
func (s *Service) writeRedirectArtifact(sourcePath, targetPath string) (err error) {
	ra, err := pipeline.NewRedirectArtifact(targetPath, 301)
	if err != nil {
		return err
	}
	loc, err := s.store.PutRedirect(ra)
	if err != nil {
		return err
	}
	return s.publication.Activate(sourcePath, loc)
}

// resolveRedirectTail 沿重定向链前进，返回最终落点；途中出现重复节点即成环。
//
// 为什么把 sourcePath 当起点：新增 A→B 时，真正要挡的是「A 能不能到达自己」。
// 沿途每个节点都进 visited，于是三类问题一次覆盖：
//   - A→A（自环，调用方在更早处也挡了一次）；
//   - A→B→A（目标指回源，链闭合）；
//   - A→B→C→B（中间成环：A 永远走不到终点）。
//
// 未生效的条目（effective=false）视为无出边，链在此终止 —— 目标缺失由调用方
// 用 active 路由校验兜底，本函数只负责「有没有环、终点在哪」。
func resolveRedirectTail(recs map[string]*redirectRecord, sourcePath, targetPath string) (string, error) {
	visited := map[string]bool{sourcePath: true}
	cur := targetPath
	for hop := 0; hop <= maxRedirectHops; hop++ {
		if visited[cur] {
			return "", ErrRedirectLoop
		}
		visited[cur] = true
		rec, ok := recs[cur]
		if !ok || !rec.effective || strings.TrimSpace(rec.targetPath) == "" {
			return cur, nil
		}
		cur = rec.targetPath
	}
	return "", ErrRedirectLoop
}

// buildRedirectItem 组装一条列表条目（含多跳 / 环标记）。
func buildRedirectItem(recs map[string]*redirectRecord, source string, ownerLabels map[string]string) pagedto.RedirectItem {
	rec, ok := recs[source]
	if !ok || rec == nil {
		// 调用方传了快照里没有的路径：返回空条目而不是 panic（列表渲染不该被一条
		// 不一致的数据打崩）。
		return pagedto.RedirectItem{SourcePath: source}
	}
	item := pagedto.RedirectItem{
		SourcePath: rec.sourcePath,
		TargetPath: rec.targetPath,
		StatusCode: rec.statusCode,
		Effective:  rec.effective,
		OwnerKind:  rec.ownerKind,
		OwnerID:    rec.ownerID,
		UpdatedAt:  rec.updatedAt.Local().Format(utils.LayoutSecond),
	}
	if rec.ownerKind == redirectOwnerPage && ownerLabels != nil {
		item.OwnerTitle = ownerLabels[rec.ownerID]
	}
	if !rec.effective || strings.TrimSpace(rec.targetPath) == "" {
		return item
	}
	tail, err := resolveRedirectTail(recs, rec.sourcePath, rec.targetPath)
	switch {
	case err != nil:
		// 成环：链上任何一点都到不了终点，页面上单独标红并禁用合并。
		item.Loop = true
	case tail != rec.targetPath:
		item.MultiHop = true
		item.FinalPath = tail
	}
	return item
}

// ownerKindOf / ownerIDOf 从路由行取归属者（两个归属列恰好一个非空）。
func ownerKindOf(row *pagemodel.PageRouteEntity) string {
	if row.PresentationID != nil {
		return redirectOwnerPresentation
	}
	if row.PageID != nil {
		return redirectOwnerPage
	}
	return ""
}

func ownerIDOf(row *pagemodel.PageRouteEntity) string {
	if row.PresentationID != nil {
		return *row.PresentationID
	}
	if row.PageID != nil {
		return *row.PageID
	}
	return ""
}
