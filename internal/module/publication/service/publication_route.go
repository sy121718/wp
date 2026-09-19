package pubservice

// publication_route.go — 路由占用与激活（占用预检、激活/取消激活、重定向、按页面或实例删除路由）。
//
// 每个写入口都拆成「校验 + 内层 *In 实现」两层：内层只接收一个 *gorm.DB 句柄，
// 由调用方决定事务边界（本文件的方法自开事务，事务透传变体见 publication_route_tx.go）。
// 事务透传是 AGENTS.md「写操作的事务与回滚」的硬要求 —— 同库跨模块的写入必须能与
// 调用方（page 的改 URL / 回滚 / 建页）落在同一个事务里，不能各写各的再补偿。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// RenameReserved 修改页面的草稿路径占用；仅允许 reserved 状态改名。
func (s *Service) RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
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
		return s.model.RenameReservedTx(ctx, tx, req.ProjectID, req.PageID, oldPath, newPath, req.OnlyReserved, now)
	}); err != nil {
		if errors.Is(err, errRouteOccupied) {
			return errors.New(pubenums.ErrRouteOccupied)
		}
		return err
	}
	return nil
}

// Activate 把路径占用切换为 active（两段式回执，docs/03-pipeline.md §9）：
//
//	第一段：pending 回执在独立事务中先行持久化并提交——进程在后续任一步
//	崩溃时，恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用归属校验 + 路由切换 + 置 committed，三者原子；
//	路由事务失败时把 pending 回执补偿为 rolled_back（补偿失败则保持
//	pending 供下次恢复处理）。
//
// 占用归属校验：目标路径已被其他页面（或展示实例，page_id 为空）占用时
// 返回 ErrRouteOccupied，绝不覆盖他人占用（H2/H7 的 DB 层兜底）。
func (s *Service) Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
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

	// 第一段：pending 回执独立事务提交（故障恢复依据，H1）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return s.model.CreateReceiptTx(ctx, tx, receipt)
	}); cerr != nil {
		logger.Scene("publication").With("url", path).With("kind", "activate").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用归属校验 + 路由切换 + 置 committed）。
	//
	// 原子抢占：单条 INSERT ... ON CONFLICT DO UPDATE（PG 方言，主库）替代
	// 原「SELECT 无锁检查 → UPDATE → CREATE」三语句。语义：
	//   - 无既有占用            → 插入 active 行（RowsAffected=1）；
	//   - 既有占用且归属者本人    → 原地升级 active（含 reserved 升级，
	//                              RowsAffected=1，幂等重复激活）；
	//   - 既有占用且非归属者      → DO UPDATE WHERE 不匹配，PG 静默 DO NOTHING
	//                              （RowsAffected=0）→ ErrRouteOccupied。
	// 消除原实现 SELECT→CREATE 的 TOCTOU 窗口：并发抢占时败者不再产生失败的
	// CREATE 撞 23505 与补偿回执，唯一约束冲突在语句内被原子消化。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if aerr := s.model.ActivateRouteTx(ctx, tx, req.ProjectID, path, owner.pageIDPtr(), owner.presentationIDPtr(), req.ArtifactID, now); aerr != nil {
			return aerr
		}
		return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", path).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
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

// 路由激活的原子 upsert（INSERT ... ON CONFLICT DO UPDATE）已下移到 model：
// 见 pubmodel.ActivateRouteTx —— 占用归属判定、TOCTOU 论证与 SQL 放在一起，
// Activate（自开事务）与 ActivateTx（外层事务）两条路径共用同一个实现。

// activatedRouteEntity 组装「刚写入的 active 行」的读回投影。
//
// 事务内不能用 GetRoute 回读：它走 model 的裸句柄（连接池另取一条连接），
// 看不见本事务尚未提交的行。Tx 变体因此按写入参数直接组装响应。
func activatedRouteEntity(req *pubdto.ActivateReq, path string, owner routeOwner, now time.Time) *pubmodel.RouteEntity {
	return &pubmodel.RouteEntity{
		ProjectID: req.ProjectID, Path: path,
		PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
		RouteKind: pubmodel.RouteActive, ArtifactID: strPtr(req.ArtifactID), UpdatedAt: now,
	}
}

// ReservePath 创建草稿路径 reserved 占用（页面创建时预留）。
// 路径已被其他实体占用（含展示实例）时返回 ErrRouteOccupied——
// 替代原 page model 三表事务里直接 INSERT reserved 撞主键的检测方式，
// 使 URL 占用单一归 publication 所有。
func (s *Service) ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
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
		return errors.New(pubenums.ErrRouteOccupied)
	}
	return err
}

// reserved 占用的写入已下移到 model：见 pubmodel.ReservePathTx ——
// 唯一键冲突归一（ErrDuplicatedKey / 23505 双判据）与插入语句同处一地，
// service 只把哨兵映射成用户文案（ReservePath / ReservePathTx 共用同一实现）。

// DeleteRoutesByPage 清理页面全部路径占用（reserved/active/redirect 任一 kind），
// 页面删除时释放路径。幂等（无占用时 RowsAffected=0 不报错）。
func (s *Service) DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPage(ctx, req.ProjectID, req.PageID)
}

// 页面路径占用的清理下移到 model：DeleteRoutesByPage / DeleteRoutesByPageTx
// 共用同一份删除条件（见 pubmodel.deleteRoutesByPageScope）——此前事务与非事务
// 两条路径各写一遍同义 WHERE，改条件时只能靠人记住改两处。

// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放）。
// 幂等（无占用时 RowsAffected=0 不报错）。
func (s *Service) DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
	}
	return s.model.DeleteRoutesByPresentation(ctx, req.ProjectID, req.PresentationID)
}

// ListReferencedArtifactIDs 返回全部被路由引用的产物行 ID（产物 GC 的保护集合）。
func (s *Service) ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error) {
	return s.model.ListReferencedArtifactIDs(ctx)
}

// ListActivePaths 返回页面已激活（active/redirect）的路径集合。
//
// 访问面（/site）直接服务 active 目录的文件系统状态：调用方清理页面时必须
// 按这些路径解除激活（删除符号链接），只删 DB 路由行不会让内容下线。
func (s *Service) ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	paths, err = s.model.ListRoutePathsByPage(ctx, req.ProjectID, req.PageID)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// ListActivePathsByPresentation 返回展示实例已激活（active/redirect）的路径集合。
//
// 改过 URL 的实例有两条路径：新路径（active）与旧路径（redirect）。删除实例时
// 必须按这两条都解除访问面激活 —— 只删 DB 路由行会让旧路径的符号链接留在
// active 目录里继续 301，指向一个已经不存在的页面。
func (s *Service) ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
	}
	paths, err = s.model.ListRoutePathsByPresentation(ctx, req.ProjectID, req.PresentationID)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// IsPathOccupied 查询路径是否被其他实体占用（page_id 为空即展示实例占用，
// page_id 非 excludePageID 即他人页面占用），供页面创建/发布前预检。
func (s *Service) IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error) {
	if req == nil {
		return false, errors.New(pubenums.ErrInvalidParam)
	}
	path, err := normalizePath(req.Path)
	if err != nil {
		return false, err
	}
	// 三个排除条件的构造下移到 model（IsPathOccupied）：此处只做参数归一化。
	return s.model.IsPathOccupied(ctx, req.ProjectID, path, req.ExcludePageID, req.ExcludePresentationID)
}

// Deactivate 取消路径占用；路由不存在时幂等返回。
//
// 归属者三分支：展示实例按 presentation_id 精确匹配（它的行 page_id 为 NULL，
// 历史判据 page_id IS NOT NULL 够不着）、页面按 page_id 精确匹配、都未指定时
// 保持「任意页面占用」的历史行为（page 侧既有调用不携带归属者，逐字不变）。
// 只删 active 行：redirect 行是「旧路径的 301 承诺」，取消激活不该顺手销毁它。
func (s *Service) Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error) {
	if req == nil {
		return errors.New(pubenums.ErrInvalidParam)
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

// 取消占用的删除语句已下移到 model：见 pubmodel.DeactivateRoute / DeactivateRouteTx
// （归属者三分支与「只删 active 行」的语义跟 SQL 放在一起，两条路径共用一份条件）。

// Redirect 把旧路径占用改为 redirect 并指向重定向产物（两段式回执，与 Activate 对齐）。
//
//	第一段：pending 回执在独立事务中先行持久化——进程在后续任一步崩溃时
//	恢复流程（RollbackReceipts）有据可查；
//	第二段：路由事务内完成占用切换 + 置 committed，二者原子；失败时把
//	pending 回执补偿为 rolled_back（补偿失败保持 pending 供恢复）。
func (s *Service) Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error) {
	if req == nil {
		return nil, errors.New(pubenums.ErrInvalidParam)
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

	// ArtifactID 允许为空（重定向产物不入库，DTO 契约）：回执不得写入空串 uuid。
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

	// 第一段：pending 回执独立事务提交（故障恢复依据，对齐 Activate）。
	if cerr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		return s.model.CreateReceiptTx(ctx, tx, receipt)
	}); cerr != nil {
		logger.Scene("publication").With("url", oldPath).With("kind", "redirect").Error(cerr, "pending 回执写入失败")
		return nil, cerr
	}

	// 第二段：路由事务（占用切换 + 置 committed）。
	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if rerr := s.model.RedirectInTx(ctx, tx, req.ProjectID, oldPath, owner.pageID, owner.presentationID, toArtifact, now); rerr != nil {
			return rerr
		}
		return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptCommitted, now)
	})
	if err != nil {
		// 路由事务失败：pending → rolled_back（补偿失败保持 pending 供恢复）。
		if rberr := s.model.Transaction(ctx, func(tx *gorm.DB) error {
			return s.model.MarkReceiptStateTx(ctx, tx, receipt.ID, pubmodel.ReceiptRolledBack, time.Now().UTC())
		}); rberr != nil {
			logger.Scene("publication").With("url", oldPath).With("receiptId", receipt.ID).
				Error(rberr, "pending 回执补偿失败（保持 pending 供恢复流程处理）")
		}
		if errors.Is(err, errRouteOccupied) {
			return nil, errors.New(pubenums.ErrRouteOccupied)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(pubenums.ErrRouteNotFound)
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

// redirectedRouteEntity 组装「刚写入的 redirect 行」的读回投影（事务内无法回读，见 activatedRouteEntity）。
func redirectedRouteEntity(req *pubdto.RedirectReq, oldPath string, owner routeOwner, toArtifact *string, now time.Time) *pubmodel.RouteEntity {
	return &pubmodel.RouteEntity{
		ProjectID: req.ProjectID, Path: oldPath,
		PageID: owner.pageIDPtr(), PresentationID: owner.presentationIDPtr(),
		RouteKind: pubmodel.RouteRedirect, ArtifactID: toArtifact, UpdatedAt: now,
	}
}
