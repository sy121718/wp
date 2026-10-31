package pubservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

func (s *Service) RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error) {
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return err
	}
	newPath, err := normalizePath(req.NewPath)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	now := time.Now().UTC()
	if err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, req.ProjectID); serr != nil {
			return serr
		}
		return s.model.RenameReservedTx(ctx, tx, req.ProjectID, req.PageID, oldPath, newPath, req.OnlyReserved, now)
	}); err != nil {
		if errors.Is(err, errRouteOccupied) {
			return pubenums.Err(pubenums.ErrRouteOccupied)
		}
		return err
	}
	return nil
}

func (s *Service) Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}
	owner, err := parseRouteOwner(req.PageID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	receiptData, merr := json.Marshal(receiptPayload{To: req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}

	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return s.model.CreateReceiptTx(ctx, tx, receipt)
	}); cerr != nil {
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, req.ProjectID); serr != nil {
			return serr
		}
		if aerr := s.model.ActivateRouteTx(ctx, tx, req.ProjectID, path, owner.pageIDPtr(), owner.presentationIDPtr(), req.ArtifactID, now); aerr != nil {
			return aerr
		}
		return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {

		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", path).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, pubenums.Err(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pubenums.Err(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(err, "路由激活失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, path)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

func activatedRouteEntity(req *pubdto.ActivateReq, path string, owner routeOwner, now time.Time) *pubmodel.RouteEntity {
	return &pubmodel.RouteEntity{
		ProjectID: req.ProjectID, Path: path,
		PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
		RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
	}
}

func (s *Service) ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error) {
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return s.model.ReservePathTx(ctx, tx, req.ProjectID, path, req.PageID, now)
	})
	if errors.Is(err, errRouteOccupied) {
		return pubenums.Err(pubenums.ErrRouteOccupied)
	}
	return err
}

func (s *Service) DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error) {
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPage(ctx, req.ProjectID, req.PageID)
}

func (s *Service) DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error) {
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPresentation(ctx, req.ProjectID, req.PresentationID)
}

func (s *Service) ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error) {
	return s.model.ListReferencedArtifactIDs(ctx)
}

func (s *Service) ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error) {
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	paths, err = s.model.ListRoutePathsByPage(ctx, req.ProjectID, req.PageID)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *Service) ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error) {
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	paths, err = s.model.ListRoutePathsByPresentation(ctx, req.ProjectID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *Service) IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error) {
	if req == nil {
		return false, pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return false, err
	}

	return s.model.IsPathOccupied(ctx, req.ProjectID, path, req.ExcludePageID, req.ExcludePresentationID)
}

func (s *Service) Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error) {
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return err
	}
	if derr := s.model.DeactivateRoute(ctx, req.ProjectID, path, req.PageID, req.PresentationID); derr != nil {
		logger.Scene("publication").With("url", path).With("kind", "deactivate").Error(derr, "路由取消失败")
		return derr
	}
	return nil
}

func (s *Service) Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	oldPath, err := normalizePath(req.OldPath)
	if err != nil {
		return nil, err
	}
	owner, err := parseRouteOwner(req.PageID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	var toArtifact *string
	if strings.TrimSpace(req.ArtifactID) != "" {
		toArtifact = strPtr(req.ArtifactID)
	}
	receiptData, merr := json.Marshal(map[string]string{"redirect": req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage(`{}`)
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: "redirect", Path: oldPath,
		ToArtifact: toArtifact, ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}

	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return s.model.CreateReceiptTx(ctx, tx, receipt)
	}); cerr != nil {
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, req.ProjectID); serr != nil {
			return serr
		}
		if rerr := s.model.RedirectInTx(ctx, tx, req.ProjectID, oldPath, owner.pageID, owner.presentationID, toArtifact, now); rerr != nil {
			return rerr
		}
		return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {

		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", oldPath).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, pubenums.Err(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, pubenums.Err(pubenums.ErrRouteNotFound)
		}
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(err, "路由重定向失败")
		return nil, err
	}
	route, err := s.model.GetRoute(ctx, req.ProjectID, oldPath)
	if err != nil {
		return nil, err
	}
	return routeResp(route), nil
}

func redirectedRouteEntity(req *pubdto.RedirectReq, oldPath string, owner routeOwner, toArtifact *string, now time.Time) *pubmodel.RouteEntity {
	return &pubmodel.RouteEntity{
		ProjectID: req.ProjectID, Path: oldPath,
		PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
		RouteKind: pubmodel.RouteRedirect, ArtifactID: toArtifact, UpdatedAt: now,
	}
}

var errTxRequired = errors.New("publication: 事务透传变体需要非空的事务句柄")

func (s *Service) ActivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if tx == nil {
		return nil, errTxRequired
	}
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, nerr := normalizePath(req.Path)
	if nerr != nil {
		return nil, nerr
	}
	owner, oerr := parseRouteOwner(req.PageID, req.PresentationID)
	if oerr != nil {
		return nil, oerr
	}
	now := time.Now().UTC()
	receiptData, merr := json.Marshal(receiptPayload{To: req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage("{}")
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: receiptAction(req.Action, "activate"), Path: path,
		ToArtifact: strPtr(req.ArtifactID), ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}
	if cerr := s.model.CreateReceiptTx(ctx, tx, receipt); cerr != nil {
		return nil, cerr
	}
	if aerr := s.model.ActivateRouteTx(ctx, tx, req.ProjectID, path, owner.pageIDPtr(), owner.presentationIDPtr(), req.ArtifactID, now); aerr != nil {
		if errors.Is(aerr, errRouteOccupied) {
			return nil, pubenums.Err(pubenums.ErrRouteOccupied)
		}
		return nil, aerr
	}
	if rerr := s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now); rerr != nil {
		return nil, rerr
	}
	return routeResp(activatedRouteEntity(req, path, owner, now)), nil
}

func (s *Service) RouteOwnerTx(ctx context.Context, tx *gorm.DB, projectID, path string) (*pubdto.RouteOwnerResp, error) {
	row, err := s.model.RouteOwnerTx(ctx, tx, projectID, path)
	if err != nil {
		return nil, err
	}
	out := &pubdto.RouteOwnerResp{
		ProjectID: row.ProjectID, Path: row.Path, RouteKind: row.RouteKind,
		PageID: row.PageID, PresentationID: row.PresentationID, ArtifactID: row.ArtifactID,
	}
	return out, nil
}

func (s *Service) DeactivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeactivateReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, nerr := normalizePath(req.Path)
	if nerr != nil {
		return nerr
	}
	if derr := s.model.DeactivateRouteTx(ctx, tx, req.ProjectID, path, req.PageID, req.PresentationID); derr != nil {
		return derr
	}
	return nil
}

func (s *Service) RedirectTx(ctx context.Context, tx *gorm.DB, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if tx == nil {
		return nil, errTxRequired
	}
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	oldPath, nerr := normalizePath(req.OldPath)
	if nerr != nil {
		return nil, nerr
	}
	owner, oerr := parseRouteOwner(req.PageID, req.PresentationID)
	if oerr != nil {
		return nil, oerr
	}
	now := time.Now().UTC()

	var toArtifact *string
	if strings.TrimSpace(req.ArtifactID) != "" {
		toArtifact = strPtr(req.ArtifactID)
	}
	receiptData, merr := json.Marshal(map[string]string{"redirect": req.ArtifactID})
	if merr != nil {
		receiptData = json.RawMessage("{}")
	}
	receipt := &pubmodel.ReceiptEntity{
		SourceType: owner.sourceType(), SourceID: owner.sourceID(),
		Action: "redirect", Path: oldPath,
		ToArtifact: toArtifact, ReceiptState: pubmodel.ReceiptPending,
		ReceiptData: receiptData, CreateTime: now,
	}
	if cerr := s.model.CreateReceiptTx(ctx, tx, receipt); cerr != nil {
		return nil, cerr
	}
	if rerr := s.model.RedirectInTx(ctx, tx, req.ProjectID, oldPath, owner.pageID, owner.presentationID, toArtifact, now); rerr != nil {
		if errors.Is(rerr, errRouteOccupied) {
			return nil, pubenums.Err(pubenums.ErrRouteOccupied)
		}
		return nil, rerr
	}
	if rerr := s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now); rerr != nil {
		return nil, rerr
	}
	return routeResp(redirectedRouteEntity(req, oldPath, owner, toArtifact, now)), nil
}

func (s *Service) RenameReservedTx(ctx context.Context, tx *gorm.DB, req *pubdto.RenameReservedReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	oldPath, nerr := normalizePath(req.OldPath)
	if nerr != nil {
		return nerr
	}
	newPath, nerr := normalizePath(req.NewPath)
	if nerr != nil {
		return nerr
	}
	if oldPath == newPath {
		return nil
	}
	if rerr := s.model.RenameReservedTx(ctx, tx, req.ProjectID, req.PageID, oldPath, newPath, req.OnlyReserved, time.Now().UTC()); rerr != nil {
		if errors.Is(rerr, errRouteOccupied) {
			return pubenums.Err(pubenums.ErrRouteOccupied)
		}
		return rerr
	}
	return nil
}

func (s *Service) ReservePathTx(ctx context.Context, tx *gorm.DB, req *pubdto.ReserveReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	path, nerr := normalizePath(req.Path)
	if nerr != nil {
		return nerr
	}
	if rerr := s.model.ReservePathTx(ctx, tx, req.ProjectID, path, req.PageID, time.Now().UTC()); rerr != nil {
		if errors.Is(rerr, errRouteOccupied) {
			return pubenums.Err(pubenums.ErrRouteOccupied)
		}
		return rerr
	}
	return nil
}

func (s *Service) DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPageTx(ctx, tx, req.ProjectID, req.PageID)
}

func (s *Service) DeleteRoutesByPresentationTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesByPresentationReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPresentationTx(ctx, tx, req.ProjectID, req.PresentationID)
}
