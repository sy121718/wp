package pageservice

import (
	"context"
	"errors"
	"fmt"
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

const maxRedirectHops = 32

const (
	redirectOwnerPage         = "page"
	redirectOwnerPresentation = "presentation"
)

type redirectRecord struct {
	sourcePath string

	targetPath string
	statusCode int
	effective  bool
	ownerKind  string
	ownerID    string
	updatedAt  time.Time
}

func (s *Service) ListRedirects(ctx context.Context, req *pagedto.RedirectListReq) (res *pagedto.RedirectListResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	res = &pagedto.RedirectListResp{Projects: []pagedto.RedirectProjectOptionResp{}, Items: []pagedto.RedirectItemResp{}}
	if s.project != nil {
		projects, lerr := s.project.List(ctx)
		if lerr != nil {
			return nil, lerr
		}
		for _, p := range projects {
			res.Projects = append(res.Projects, pagedto.RedirectProjectOptionResp{ID: p.ID, Name: p.Name})
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
	ownerLabels, err := s.redirectOwnerLabels(ctx, projectID, records)
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

func (s *Service) CreateRedirect(ctx context.Context, req *pagedto.RedirectCreateReq) (res *pagedto.RedirectItemResp, err error) {
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

	redirectReq := &pubcontract.RedirectReq{ProjectID: projectID, OldPath: source}
	if owner.PageID != nil {
		redirectReq.PageID = *owner.PageID
	} else if owner.PresentationID != nil {
		redirectReq.PresentationID = *owner.PresentationID
	} else {

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

	return &pagedto.RedirectItemResp{
		SourcePath: source,
		TargetPath: tail,
		StatusCode: 301,
		Effective:  true,
		OwnerKind:  ownerKindOf(owner),
		OwnerID:    ownerIDOf(owner),
		UpdatedAt:  time.Now().Local().Format(utils.LayoutSecond),
	}, nil
}

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

	var owner *pubcontract.RouteOwnerResp
	oerr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		row, rerr := s.routes.RouteOwnerTx(ctx, tx, projectID, path)
		owner = row
		return rerr
	})
	if oerr != nil {
		if errors.Is(oerr, gorm.ErrRecordNotFound) {
			return ErrRedirectNotFound
		}
		return oerr
	}
	if owner == nil || owner.RouteKind != publisherRouteKindRedirect {
		return ErrRedirectConflict
	}

	if s.routes == nil {
		return ErrRedirectUnavailable
	}
	intent, rerr := s.registerProjectDeactivationIntent(ctx, projectID, path)
	if rerr != nil {
		return rerr
	}

	callbackCompleted := false
	txErr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {

		if aerr := s.model.LockProjectEntityAdvisoryTx(ctx, tx, projectID); aerr != nil {
			return aerr
		}
		return s.withPath(path, func() error {
			if oerr := s.verifyProjectRedirectOwner(ctx, tx, projectID, path, intent.PathArtifacts[path]); oerr != nil {
				return oerr
			}
			n, derr := s.model.DeleteRedirectRouteTx(ctx, tx, projectID, path)
			if derr != nil {
				return derr
			}

			if n != 1 {
				return fmt.Errorf("%w: 路径 %s 的重定向行已不归本事务处置", ErrRedirectConflict, path)
			}
			if merr := s.markDeactivationCommitted(ctx, tx, intent); merr != nil {
				return merr
			}
			callbackCompleted = true
			return nil
		})
	})
	if txErr != nil && !callbackCompleted {

		s.abortPublishReceipt(ctx, intent.ReceiptID, "重定向删除的业务事务未提交，访问面保持原样")
		logger.Scene("page").With("path", path).Error(txErr, "重定向删除失败（业务事务未提交）")
		return redirectDeleteError(txErr)
	}
	if txErr != nil {

		s.keepPublishReceiptPending(intent.ReceiptID, "重定向删除的业务事务提交结果不明")
		logger.Scene("page").With("path", path).
			Error(txErr, "重定向删除提交结果不明（回执保持 pending 交收敛重判）")
		return mapPersistenceError(txErr)
	}
	if !callbackCompleted {
		s.abortPublishReceipt(ctx, intent.ReceiptID, "重定向删除的业务事务未置提交标记")
		return nil
	}

	if werr := s.withdrawProjectDeactivatedLinks(ctx, projectID, intent.ReceiptID); werr != nil {
		s.keepPublishReceiptPending(intent.ReceiptID, "重定向已删除，撤链失败")
		logger.Scene("page").With("path", path).
			Error(werr, "重定向已删除但撤链失败（回执保持 pending 交收敛重放）")
		return nil
	}
	if cerr := s.completePublishReceipt(ctx, intent.ReceiptID); cerr != nil {
		s.NotifyPendingReceipt()
		return nil
	}
	logger.Scene("page").With("path", path).Info("重定向已删除")
	return nil
}

func redirectDeleteError(err error) error {
	if errors.Is(err, ErrRedirectConflict) {
		return ErrRedirectConflict
	}
	return mapPersistenceError(err)
}

func (s *Service) MergeRedirectChain(ctx context.Context, req *pagedto.RedirectMergeReq) (res *pagedto.RedirectItemResp, err error) {
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

	item := buildRedirectItem(records, path, nil)
	item.TargetPath = tail
	item.FinalPath = ""
	item.MultiHop = false
	item.Loop = false
	item.StatusCode = 301
	item.Effective = true
	return &item, nil
}

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

func (s *Service) redirectOwnerLabels(ctx context.Context, projectID string, recs map[string]*redirectRecord) (labels map[string]string, err error) {
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
	pages, err := s.model.FindPagesByIDs(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	for i := range pages {
		labels[pages[i].ID] = pages[i].DraftPath
	}
	return labels, nil
}

func (s *Service) writeRedirectArtifact(sourcePath, targetPath string) (err error) {
	ra, err := pipeline.NewRedirectArtifact(targetPath, 301)
	if err != nil {
		return err
	}
	loc, err := s.store.PutRedirect(ra)
	if err != nil {
		return err
	}
	return s.withPath(sourcePath, func() error { return s.publication.Activate(sourcePath, loc) })
}

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

func buildRedirectItem(recs map[string]*redirectRecord, source string, ownerLabels map[string]string) pagedto.RedirectItemResp {
	rec, ok := recs[source]
	if !ok || rec == nil {

		return pagedto.RedirectItemResp{SourcePath: source}
	}
	item := pagedto.RedirectItemResp{
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

		item.Loop = true
	case tail != rec.targetPath:
		item.MultiHop = true
		item.FinalPath = tail
	}
	return item
}

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
