package pubmodel

// publication_route_tx_model.go — 路由与回执的「事务透传写方法」。
//
// 这批方法在**调用方已开启的事务句柄**上执行（service 起事务，句柄透传进来），
// 与 publication_route_tx.go 的 …Tx 服务方法一一对应：service 只做参数归一化与
// 错误映射，不再在句柄上拼 gorm 查询（AGENTS.md「model 层定位」）。SQL 文本、
// WHERE、更新的列与错误语义从原 service 内联实现逐字搬运，不是重写。

import (
	"context"
	"errors"
	"strings"
	"time"

	pubenums "go_wp/internal/module/publication/enums"

	"gorm.io/gorm"
)

// ErrRouteOccupied 目标路径已被其他实体占用（(project_id, path) 唯一约束冲突）。
//
// model 侧在唯一键冲突时归一返回它，service 层统一映射为 pubenums.ErrRouteOccupied
// 文案 —— 归一逻辑（ErrDuplicatedKey / 23505 双判据）跟写入语句在一起，调用方
// 只认哨兵，不重复写方言判据。
var ErrRouteOccupied = errors.New("publication: 目标路径已被其他实体占用")

// markRouteOccupiedErr 唯一键冲突归一：其余错误原样返回。
func markRouteOccupiedErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "23505") {
		return ErrRouteOccupied
	}
	return err
}

// MarkReceiptStateTx 在外层事务内回写回执状态（receipt_state + completed_at 一起写）。
//
// Activate / Redirect 的第二段路由事务用它把 pending 回执置 committed，失败补偿
// 置 rolled_back —— 回执结案必须与路由操作留在同一事务里，否则会出现
// 「路由已切换但回执停在 pending」的假残留。
func (m *Model) MarkReceiptStateTx(ctx context.Context, tx *gorm.DB, id int64, state string, now time.Time) error {
	return tx.WithContext(ctx).Model(&ReceiptEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}

// RenameReservedTx 在外层事务内迁移保留路由（改名）。
//
// 语义与原 service 内联实现逐字一致：
//   - 旧路径无任何占用 → 幂等成功（草稿路由可能尚未建立）；
//   - 旧行不是 reserved 且 OnlyReserved → 幂等成功（不动本页 active/redirect 行）；
//   - 旧行是他人 active/redirect → ErrRouteActiveRename（不触碰他人线上路径）；
//   - 先清理新路径上本页 active 残留（避免与 (project_id, path) 唯一约束冲突），
//     再迁移旧路径行；新路径被他人占用时唯一约束冲突归一为 ErrRouteOccupied；
//   - UPDATE 影响 0 行（旧路径被其他页面 reserved 占用）→ 幂等成功。
func (m *Model) RenameReservedTx(ctx context.Context, tx *gorm.DB, projectID, pageID, oldPath, newPath string, onlyReserved bool, now time.Time) error {
	// 区分「旧路径无任何占用」与「旧路径不是 reserved」：
	// 前者视为幂等成功（草稿路由可能尚未建立）；后者需按归属判断——
	// 本页 active/redirect 行是页面改 URL 流程（UpdateURL）的 DB 同步步骤
	// （发布时 reserved 被原地升级为 active，无独立 reserved 行），必须允许迁移；
	// 他人 active/redirect 行禁止直接改名（不应触碰他人线上路径）。
	var existing RouteEntity
	switch ferr := tx.WithContext(ctx).Where("project_id = ? AND path = ?", projectID, oldPath).
		First(&existing).Error; {
	case errors.Is(ferr, gorm.ErrRecordNotFound):
		return nil
	case ferr != nil:
		return ferr
	}
	if existing.RouteKind != RouteReserved {
		// OnlyReserved：调用方（多语言下改其他语言 URL）只希望迁移草稿占用，
		// 明确要求不动本页 active/redirect 行。
		if onlyReserved {
			return nil
		}
		if existing.PageID == nil || *existing.PageID != pageID {
			return errors.New(pubenums.ErrRouteActiveRename)
		}
	}
	// 先清理新路径上本页 active 残留（避免迁移行与 (project_id, path)
	// 唯一约束冲突），再迁移旧路径行（reserved 或本页 active）。
	if err := tx.WithContext(ctx).Where("project_id = ? AND path = ? AND page_id = ? AND route_kind = ?",
		projectID, newPath, pageID, RouteActive).
		Delete(&RouteEntity{}).Error; err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ? AND page_id = ?", projectID, oldPath, pageID).
		Updates(map[string]any{"path": newPath, "update_time": now})
	if result.Error != nil {
		// 新路径被他人占用时 UPDATE 撞 (project_id, path) 唯一约束——
		// 归一为 ErrRouteOccupied（语义：改名目标路径已被其他页面占用）。
		return markRouteOccupiedErr(result.Error)
	}
	if result.RowsAffected != 1 {
		// 旧路径被其他页面 reserved 占用（page_id 不匹配）：保持幂等成功，
		// 不触碰他人草稿占用。
		return nil
	}
	return nil
}

// RedirectInTx 在外层事务内把旧路径切为 redirect 占用。
//
// 语义与原 service 内联实现逐字一致：
//   - 按归属者精确匹配既有行（展示实例按 presentation_id、页面按 page_id），
//     UPDATE 为 redirect；影响 0 行（无既有占用）时幂等建立 redirect 行；
//   - 对他人占用路径建 redirect 行撞 (project_id, path) 唯一约束 → ErrRouteOccupied；
//   - toArtifact 非 nil 时回填 artifact_id（重定向产物入库后才指向）。
//
// 归属者二选一（page_routes 的 CHECK 约束要求恰好一个非空），与 service 侧
// routeOwner 的 match() / pageIDPtr() / presentationIDPtr() 同一口径。
func (m *Model) RedirectInTx(ctx context.Context, tx *gorm.DB, projectID, oldPath, pageID, presentationID string, toArtifact *string, now time.Time) error {
	q := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ?", projectID, oldPath)
	if presentationID != "" {
		q = q.Where("presentation_id = ?", presentationID)
	} else {
		q = q.Where("page_id = ?", pageID)
	}
	result := q.Updates(map[string]any{
		"route_kind":  RouteRedirect,
		"update_time": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 无既有占用时直接建立 redirect 行（幂等）。
		ins := &RouteEntity{
			ProjectID: projectID, Path: oldPath,
			PageID: nil, PresentationID: nil,
			RouteKind: RouteRedirect, UpdatedAt: now,
		}
		if pageID != "" {
			ins.PageID = &pageID
		}
		if presentationID != "" {
			ins.PresentationID = &presentationID
		}
		if cerr := tx.WithContext(ctx).Create(ins).Error; cerr != nil {
			// 对他人占用路径建 redirect 行撞 (project_id, path) 唯一约束——
			// 归一为 ErrRouteOccupied（语义：重定向目标路径已被其他页面占用）。
			return markRouteOccupiedErr(cerr)
		}
	}
	if toArtifact != nil {
		if uerr := tx.WithContext(ctx).Model(&RouteEntity{}).
			Where("project_id = ? AND path = ?", projectID, oldPath).
			Update("artifact_id", *toArtifact).Error; uerr != nil {
			return uerr
		}
	}
	return nil
}
