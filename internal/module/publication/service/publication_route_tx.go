package pubservice

// publication_route_tx.go — 路由写入的事务透传变体（AGENTS.md「写操作的事务与回滚」）。
//
// 为什么需要它们：page 的发布链（改 URL / 回滚 / 建页 / 删页 / 存草稿）是「同库跨模块」
// 的一次写操作 —— 它既要写自己的 pages / page_publications，又要写 publication 的
// page_routes。规则要求这两部分落在**同一个事务**里，而不是「先写 A 再补偿 B」
// （补偿只允许用于跨库/外部系统：文件、Redis、第三方）。
//
// 分工：本文件的每个方法都在**调用方已经开着的事务**上执行，因此
//   - 不新开事务（另开事务会让外层未提交的数据不可见，原子性也被悄悄破坏）；
//   - 不做补偿（外层回滚会把这里的写入一并撤掉，补偿反而会写出撤销不掉的残留）；
//   - 不回读自己刚写的行（同事务内的行对连接池里的另一条连接不可见），
//     返回的投影按写入参数组装。
//
// 调用方负责：开启事务 + 设置工程作用域（rls.ScopeTx）+ 提交/回滚。
// 回执：ActivateTx / RedirectTx 仍写一条路由回执，但它与外层事务同生共死 ——
// 外层回滚时回执一并消失，不留「查不到对应变更的 pending 回执」。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

// errTxRequired 事务透传变体拿到 nil 句柄时的显式错误。
//
// 不能降级为「自己开一个事务」：那正是这些变体要消灭的形态 —— 调用方以为
// 两处写在同一个事务里，实际各写各的。
var errTxRequired = errors.New("publication: 事务透传变体需要非空的事务句柄")

// ActivateTx 在外层事务内激活路径占用（占用归属校验 + 原子 upsert + 路由回执）。
func (s *Service) ActivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if tx == nil {
		return nil, errTxRequired
	}
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
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
	if cerr := tx.WithContext(ctx).Create(receipt).Error; cerr != nil {
		return nil, cerr
	}
	if aerr := activateRouteIn(tx.WithContext(ctx), req, path, owner, now); aerr != nil {
		if errors.Is(aerr, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		return nil, aerr
	}
	if rerr := s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now); rerr != nil {
		return nil, rerr
	}
	return routeResp(activatedRouteEntity(req, path, owner, now)), nil
}

// DeactivateTx 在外层事务内取消路径占用（幂等：无占用时不报错）。
func (s *Service) DeactivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeactivateReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, nerr := normalizePath(req.Path)
	if nerr != nil {
		return nerr
	}
	if derr := deactivateIn(tx.WithContext(ctx), req, path).Error; derr != nil {
		return derr
	}
	return nil
}

// RedirectTx 在外层事务内把旧路径切为 redirect（占用切换 + 路由回执）。
func (s *Service) RedirectTx(ctx context.Context, tx *gorm.DB, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if tx == nil {
		return nil, errTxRequired
	}
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
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
	// ArtifactID 允许为空（重定向产物不入库，DTO 契约）：回执不得写入空串 uuid。
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
	if cerr := tx.WithContext(ctx).Create(receipt).Error; cerr != nil {
		return nil, cerr
	}
	if rerr := s.model.RedirectInTx(ctx, tx, req.ProjectID, oldPath, owner.pageID, owner.presentationID, toArtifact, now); rerr != nil {
		if errors.Is(rerr, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		return nil, rerr
	}
	if rerr := s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now); rerr != nil {
		return nil, rerr
	}
	return routeResp(redirectedRouteEntity(req, oldPath, owner, toArtifact, now)), nil
}

// RenameReservedTx 在外层事务内迁移保留路由（改名）。
func (s *Service) RenameReservedTx(ctx context.Context, tx *gorm.DB, req *pubdto.RenameReservedReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
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
			return errors.New(pubenums.ErrRouteOccupied)
		}
		return rerr
	}
	return nil
}

// ReservePathTx 在外层事务内创建草稿路径 reserved 占用（页面创建时预留）。
func (s *Service) ReservePathTx(ctx context.Context, tx *gorm.DB, req *pubdto.ReserveReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	path, nerr := normalizePath(req.Path)
	if nerr != nil {
		return nerr
	}
	if rerr := reservePathIn(tx.WithContext(ctx), req, path, time.Now().UTC()); rerr != nil {
		if errors.Is(rerr, errRouteOccupied) {
			return errors.New(pubenums.ErrRouteOccupied)
		}
		return rerr
	}
	return nil
}

// DeleteRoutesByPageTx 在外层事务内清理页面全部路径占用（幂等）。
func (s *Service) DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesReq) (err error) {
	if tx == nil {
		return errTxRequired
	}
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	return deleteRoutesByPageIn(tx.WithContext(ctx), req)
}
