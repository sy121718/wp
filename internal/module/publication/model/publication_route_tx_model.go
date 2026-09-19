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
	"gorm.io/gorm/clause"
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

// CreateReceiptTx 在外层事务内写一条发布回执（pending）。
//
// Activate / Redirect 与它们的 Tx 变体都先落 pending 回执再动路由：进程在后续任一步
// 崩溃时，恢复流程（RollbackReceipts）有据可查（docs/03-pipeline.md §9）。
// 事务句柄由 service 决定归属 —— Activate 用独立事务先提交回执，ActivateTx 让它与外层
// 事务同生共死；两种语义的差别在 service，写回执这一条 SQL 只有这一份。
func (m *Model) CreateReceiptTx(ctx context.Context, tx *gorm.DB, r *ReceiptEntity) error {
	return tx.WithContext(ctx).Create(r).Error
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

// ownershipExprSQL ON CONFLICT DO UPDATE 的归属者一致性判定。
//
// 用 IS NOT DISTINCT FROM 而不是 = ：展示实例的行 page_id 为 NULL，而
// NULL = NULL 在 SQL 里求值为 NULL（不成立），按 page_id 比会让实例连
// 「重复激活自己」都失败（第二次发布会误判成 ErrRouteOccupied）。
// IS NOT DISTINCT FROM 把 NULL 当作可比较值，两类归属者都能正确判等。
//
// 留在 model 侧：它是 upsert 语句的 WHERE 片段（SQL），不是业务判定 —— 归属者「恰好一个非空」
// 由 service 的 routeOwner 校验后以两个 *string 传进来，这里只负责 SQL 怎么比。
const ownershipExprSQL = "page_routes.page_id IS NOT DISTINCT FROM EXCLUDED.page_id" +
	" AND page_routes.presentation_id IS NOT DISTINCT FROM EXCLUDED.presentation_id"

// ActivateRouteTx 在外层事务内完成路由激活（占用归属校验 + 原子 upsert）。
//
// 原子抢占：单条 INSERT ... ON CONFLICT DO UPDATE（PG 方言，主库）。语义：
//   - 无既有占用          → 插入 active 行（RowsAffected=1）；
//   - 既有占用且归属者本人  → 原地升级 active（含 reserved 升级，RowsAffected=1，幂等重复激活）；
//   - 既有占用且非归属者    → DO UPDATE WHERE 不匹配，PG 静默 DO NOTHING
//     （RowsAffected=0）→ ErrRouteOccupied。
//
// 消除原实现 SELECT→CREATE 的 TOCTOU 窗口：并发抢占时败者不再产生失败的
// CREATE 撞 23505 与补偿回执，唯一约束冲突在语句内被原子消化。
//
// 两条调用路径（Activate 自开事务、ActivateTx 在外层事务里）共用这一个方法：
// 抢占语义必须逐字一致，各写一遍迟早分叉 —— 分叉的表现是「同一条路径，
// 一个入口能抢占、另一个入口报占用」这种最难查的一类不一致。
func (m *Model) ActivateRouteTx(ctx context.Context, tx *gorm.DB, projectID, path string, pageID, presentationID *string, artifactID string, now time.Time) error {
	result := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "path"}},
		// DO UPDATE 仅当冲突行归属者本人：页面按 page_id、展示实例按
		// presentation_id 比对（用 IS NOT DISTINCT FROM 让 NULL 也能相等，
		// 否则「实例行 page_id 为 NULL」这一半永远匹配不上）。他人时
		// WHERE 不成立 → 0 行 → occupied。
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: ownershipExprSQL}}},
		DoUpdates: clause.AssignmentColumns([]string{"route_kind", "artifact_id", "update_time"}),
	}).Create(&RouteEntity{
		ProjectID: projectID, Path: path,
		PageID: pageID, PresentationID: presentationID,
		RouteKind: RouteActive, ArtifactID: &artifactID, UpdatedAt: now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 目标路径被其他实体占用：ON CONFLICT 未执行更新。
		return ErrRouteOccupied
	}
	return nil
}

// ReservePathTx 创建草稿路径 reserved 占用（页面创建时预留）。
//
// 路径已被其他实体占用（含展示实例）时归一为 ErrRouteOccupied —— 替代原 page model
// 三表事务里直接 INSERT reserved 撞主键的检测方式，使 URL 占用单一归 publication 所有。
func (m *Model) ReservePathTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID string, now time.Time) error {
	pageIDCopy := pageID
	if cerr := tx.WithContext(ctx).Create(&RouteEntity{
		ProjectID: projectID, Path: path, PageID: &pageIDCopy,
		RouteKind: RouteReserved, UpdatedAt: now,
	}).Error; cerr != nil {
		return markRouteOccupiedErr(cerr)
	}
	return nil
}

// deleteRoutesByPageScope 页面全部路径占用的删除范围（事务 / 非事务两条入口共用一份条件）。
func (m *Model) deleteRoutesByPageScope(db *gorm.DB, projectID, pageID string) *gorm.DB {
	return db.Where("project_id = ? AND page_id = ?", projectID, pageID)
}

// DeleteRoutesByPageTx 在外层事务内清理页面全部路径占用（reserved/active/redirect 任一 kind），
// 页面删除时释放路径。幂等（无占用时 RowsAffected=0 不报错）。
func (m *Model) DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) error {
	return m.deleteRoutesByPageScope(tx.WithContext(ctx), projectID, pageID).Delete(&RouteEntity{}).Error
}

// DeleteRoutesByPage 非事务路径的同名操作（同一份条件，见 deleteRoutesByPageScope）。
func (m *Model) DeleteRoutesByPage(ctx context.Context, projectID, pageID string) error {
	return m.deleteRoutesByPageScope(m.db.WithContext(ctx), projectID, pageID).Delete(&RouteEntity{}).Error
}

// deleteRoutesByPresentationScope 展示实例全部路径占用的删除范围（事务 / 非事务两条入口共用一份条件）。
//
// 归属列是 presentation_id 而不是 page_id：实例的行 page_id 为 NULL（page_routes 的
// CHECK 约束要求两个归属者恰好一个非空），按 page_id 匹配一行都删不到。
func (m *Model) deleteRoutesByPresentationScope(db *gorm.DB, projectID, presentationID string) *gorm.DB {
	return db.Where("project_id = ? AND presentation_id = ?", projectID, presentationID)
}

// DeleteRoutesByPresentationTx 在外层事务内清理展示实例全部路径占用（实例删除时释放）。
// 幂等（无占用时 RowsAffected=0 不报错）。
//
// 为什么要有事务形态：删除实例是「实例行 + 它登记的 page_routes 行」两处同库写，
// 分属 presentation / publication 两个模块（AGENTS.md「写操作的事务与回滚」——
// 跨模块 DB 写只能 tx 透传，不能用补偿）。只删一处会留下悬空引用或死路径。
func (m *Model) DeleteRoutesByPresentationTx(ctx context.Context, tx *gorm.DB, projectID, presentationID string) error {
	return m.deleteRoutesByPresentationScope(tx.WithContext(ctx), projectID, presentationID).
		Delete(&RouteEntity{}).Error
}

// deactivateRouteScope 取消占用的删除范围（归属者三分支，事务 / 非事务两条入口共用）。
//
// 归属者三分支：展示实例按 presentation_id 精确匹配（它的行 page_id 为 NULL，
// 历史判据 page_id IS NOT NULL 够不着）、页面按 page_id 精确匹配、都未指定时
// 保持「任意页面占用」的历史行为（page 侧既有调用不携带归属者，逐字不变）。
// 只删 active 行：redirect 行是「旧路径的 301 承诺」，取消激活不该顺手销毁它。
//
// 原 service 自由函数返回 *gorm.DB（为的是调用方能看 RowsAffected），实际上两处调用方
// 都只取 .Error：这里返回 error，语义与调用方行为逐条等价。
func (m *Model) deactivateRouteScope(db *gorm.DB, projectID, path, pageID, presentationID string) *gorm.DB {
	q := db.Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteActive)
	switch {
	case strings.TrimSpace(presentationID) != "":
		q = q.Where("presentation_id = ?", strings.TrimSpace(presentationID))
	case strings.TrimSpace(pageID) != "":
		q = q.Where("page_id = ?", strings.TrimSpace(pageID))
	default:
		q = q.Where("page_id IS NOT NULL")
	}
	return q
}

// DeactivateRouteTx 在外层事务内取消路径占用（幂等：无占用时不报错）。
func (m *Model) DeactivateRouteTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID, presentationID string) error {
	return m.deactivateRouteScope(tx.WithContext(ctx), projectID, path, pageID, presentationID).
		Delete(&RouteEntity{}).Error
}

// DeactivateRoute 非事务路径的同名操作（同一份条件，见 deactivateRouteScope）。
func (m *Model) DeactivateRoute(ctx context.Context, projectID, path, pageID, presentationID string) error {
	return m.deactivateRouteScope(m.db.WithContext(ctx), projectID, path, pageID, presentationID).
		Delete(&RouteEntity{}).Error
}
