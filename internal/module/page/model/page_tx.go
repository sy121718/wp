package pagemodel

// page_tx.go — 跨聚合/跨模块事务的透传变体（AGENTS.md「写操作的事务与回滚」）。
//
// 定位：model 不自持事务边界，只提供「在调用方给的事务句柄上执行」的形态。
// service 决定边界（m.TransactionScoped），把 *gorm.DB 依次传给本文件的 *Tx 方法
// 与其它模块的 …Tx 方法（如 publication 的 ActivateTx / RenameReservedTx）。
//
// 两条硬约束：
//  1. 作用域：每写一张带 FORCE 策略的表之前都先 rls.ScopeTx(tx, projectID) ——
//     set_config(..., is_local => true) 在事务内可以反复改，所以「同一个事务里
//     先写 A 工程再写 B 工程」是允许的；但不设作用域在换非超级角色后是静默 0 行，
//     那正是这批 *Tx 方法要避免的失败形态。
//  2. 不接受「不传 tx 的变体」：本文件的方法一律只在外部 tx 上执行，
//     避免调用方以为自己在事务里、实际各写各的。
//
// 与既有非 Tx 方法的关系：非 Tx 方法（SaveDraftWithRevision / MarkPublishedLang …）
// 是「自足入口」（自带事务 + 作用域），保留给单步调用；两者共享同一段 SQL，
// 逻辑改动必须同时落两处（本文件的实现即从非 Tx 版本抽出，逐字一致）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// TransactionScoped 开启事务并设置工程作用域，供 service 编排跨聚合/跨模块的写。
//
// service 侧不再直接碰 rls：作用域的设置与事务边界是同一件事（set_config 只在事务内
// 有效），放在这里可以避免「开了事务忘了设作用域」——那种写法在换非超级角色后
// 每条语句都匹配 0 行且不报错。
func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// CreateWithRevisionTx 在外部事务内创建 Page 与初始 Revision。
func (m *Model) CreateWithRevisionTx(ctx context.Context, tx *gorm.DB, page *PageEntity, revision *RevisionEntity) error {
	if err := rls.ScopeTx(tx, page.ProjectID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(page).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(revision).Error
}

// SaveDraftWithRevisionTx 在外部事务内用乐观锁保存草稿与修订。
//
// 返回值语义与 SaveDraftWithRevision 一致：未命中当前版本返回 ErrDraftVersionConflict，
// 由外层回滚（此时调用方在同一事务里做的路径占用迁移也会一并撤销 —— 这正是
// 「先写 A 再补偿 B」做不到的事）。
func (m *Model) SaveDraftWithRevisionTx(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, projectID, expectedVersion).
		Updates(map[string]any{
			"draft_path":     path,
			"draft_document": document,
			"draft_version":  nextVersion,
			"stale":          true,
			"update_time":    updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return tx.WithContext(ctx).Create(revision).Error
}

// MarkStaleForI18nTx 在外部事务内把该工程全部未删除页面标记为待重建，返回真正命中的页面 ID。
//
// 与 MarkStaleForI18n 共用同一段 SQL（markStaleForI18nIn），差别只有事务边界：
// service 的 MarkStaleForI18n 要把「pages 标记」与跨模块 peer（其它发布来源，如自动发布
// 实例）的标记放进**同一个事务** —— 同库跨模块的写必须同进同出（AGENTS.md「写操作的事务
// 与回滚」），否则 peer 失败时会留下「页面已标、实例未标」的半截状态，而那一侧没有任何
// 自动补的入口。作用域仍在这里设：set_config(..., is_local => true) 在事务内可重复设置，
// 外层设过也不冲突（见 pkg/rls.ScopeTx 的分工说明）。
func (m *Model) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	return m.markStaleForI18nIn(ctx, tx, projectID, at)
}

// DeletePublicationsByLangTx 在外部事务内删除某页面某语言的发布指针（幂等）。
//
// 与 DeletePublicationsByLang 同一段 SQL（只按 page_id + lang 删，绝不整页删 ——
// 其它语言的指针必须留着）。语义差别是与 publication 的 DeactivateTx 落在同一事务里：
// 「路径占用解除 + 发布指针删除」是一次退役动作的两半，各自提交会留下
// 「页面已退役但路径仍占用」或反过来。作用域在此显式设置：page_publications 带 FORCE 策略。
func (m *Model) DeletePublicationsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PublicationEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error
}

// DeleteStagingsByLangTx 在外部事务内删除某页面某语言的暂存指针（幂等，理由同上）。
func (m *Model) DeleteStagingsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&StagingEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&StagingEntity{}).Error
}

// MoveDraftPathTx 在外部事务内同步草稿路径（逻辑路径，不含语言前缀）。
func (m *Model) MoveDraftPathTx(ctx context.Context, tx *gorm.DB, projectID, pageID, newPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Updates(map[string]any{"draft_path": newPath, "update_time": at}).Error
}

// MovePublicationPathTx 在外部事务内同步某语言的激活路径与 pages 单值镜像。
func (m *Model) MovePublicationPathTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang, activePath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if uerr := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).
		Updates(map[string]any{"active_path": activePath, "update_time": at}).Error; uerr != nil {
		return uerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"active_path": activePath, "update_time": at}).Error
}

// MarkPublishedLangTx 在外部事务内记录某语言激活状态（page_publications upsert + pages 镜像）。
func (m *Model) MarkPublishedLangTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &PublicationEntity{
		PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", rec.PageID).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		}).Error
}

// MarkStagedLangTx 在外部事务内回写某语言的暂存产物指针（page_stagings upsert + pages 镜像）。
func (m *Model) MarkStagedLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang, artifactID, artifactHash string, draftVersion int64, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &StagingEntity{
		PageID: pageID, Lang: lang, ArtifactID: artifactID,
		ArtifactHash: artifactHash, DraftVersion: draftVersion, UpdatedAt: at,
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"artifact_id", "artifact_hash", "draft_version", "update_time",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"staged_artifact_id": artifactID, "stale": false, "update_time": at}).Error
}

// ReplaceDependenciesTx 在外部事务内全量替换某产物的依赖记录（delete + insert）。
func (m *Model) ReplaceDependenciesTx(ctx context.Context, tx *gorm.DB, projectID, pageID, artifactID string, rows []DependencyEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
		return err
	}
	if derr := tx.WithContext(ctx).Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
		return derr
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 200).Error
}

// SoftDeleteTx 在外部事务内软删页面（含同聚合的 page_publications / page_stagings 清理）。
func (m *Model) SoftDeleteTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return m.softDeleteTx(ctx, tx, projectID, pageID, at)
}

// UpsertSiteSlotTx 在外部事务内绑定槽位（同工程同槽位已有绑定时原地换页）。
func (m *Model) UpsertSiteSlotTx(ctx context.Context, tx *gorm.DB, e *SiteSlotEntity) error {
	if err := rls.ScopeTx(tx, e.ProjectID); err != nil {
		return err
	}
	var existing SiteSlotEntity
	ferr := tx.WithContext(ctx).Model(&SiteSlotEntity{}).
		Where("project_id = ? AND slot = ?", e.ProjectID, e.Slot).First(&existing).Error
	switch {
	case ferr == nil:
		return tx.WithContext(ctx).Model(&SiteSlotEntity{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"page_id":     e.PageID,
			"update_time": e.UpdatedAt,
		}).Error
	case errors.Is(ferr, gorm.ErrRecordNotFound):
		return tx.WithContext(ctx).Model(&SiteSlotEntity{}).Create(e).Error
	default:
		return ferr
	}
}

// DeleteSiteSlotTx 在外部事务内解绑槽位；返回受影响行数（0 = 本来就没绑）。
func (m *Model) DeleteSiteSlotTx(ctx context.Context, tx *gorm.DB, projectID, slot string) (int64, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	res := tx.WithContext(ctx).Model(&SiteSlotEntity{}).
		Where("project_id = ? AND slot = ?", projectID, slot).Delete(&SiteSlotEntity{})
	return res.RowsAffected, res.Error
}

// MarkStaleForProjectTx 在外部事务内把该工程全部未删除页面标记为待重建。
func (m *Model) MarkStaleForProjectTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("project_id = ? AND deleted_at IS NULL AND stale = ?", projectID, false).
		Updates(map[string]any{"stale": true, "update_time": at}).Error
}

// MarkStaleByDependencyTx 在外部事务内按依赖源 (kind,key) 精确标记指定工程的受影响页面。
func (m *Model) MarkStaleByDependencyTx(ctx context.Context, tx *gorm.DB, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	err = tx.WithContext(ctx).Raw(
		"WITH affected AS (\n"+
			"\tSELECT DISTINCT d.page_id AS page_id\n"+
			"\tFROM page_dependencies d\n"+
			"\tJOIN pages p ON p.id = d.page_id\n"+
			"\tWHERE d.dependency_kind = ?\n"+
			"\t  AND d.dependency_key = ?\n"+
			"\t  AND p.project_id = ?\n"+
			"\t  AND p.deleted_at IS NULL\n"+
			"\t  AND (d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM page_publications WHERE page_id = p.id UNION SELECT artifact_id FROM page_stagings WHERE page_id = p.id))\n"+
			")\n"+
			"UPDATE pages SET stale = true, update_time = ?\n"+
			"WHERE project_id = ? AND deleted_at IS NULL AND id IN (SELECT page_id FROM affected)\n"+
			"RETURNING id", kind, key, projectID, at, projectID).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ClearPublicationLangTx 在外部事务内退役某页面某语言的发布状态（PIPE-7 定时下线用）。
//
// 两步必须同进同出（AGENTS.md「写操作的事务与回滚」）：
//
//  1. 删 page_publications 该语言的激活行（**只该语言**：其它语言还在服务，
//     整页删会让它们立刻失去「已发布」状态而访问面上产物还在 —— 见
//     DeletePublicationsByLangTx 的同一条论证）；
//  2. 同步 pages 的单值镜像（active_artifact_id / active_path / published_at）。
//
// 为什么第 2 步不能省：pages 那三列是「最近发布语言的镜像」，而列表页与
// 详情页的「已发布 / 已下线」投影读的正是它（page_list.go 的 pageResp）。
// 只删 page_publications 会让下线后的页面在后台仍显示「已发布」—— 那是一句假话，
// 且没有任何报错。镜像的取值按**剩余语言里最近发布的一条**（无剩余则清空）：
// 这与 MarkPublishedLangTx 的写入口径一致（写入方永远把镜像设成自己那次发布）。
//
// 与 RetireLocale 的差别（那边只删页发布指针、不动镜像）：那批是「整页的全部语言一起退役」，
// 镜像随后由下一次发布覆盖；这里是单语言的定时下线，页面仍在列表页上显示状态，
// 镜像必须当场正确。
func (m *Model) ClearPublicationLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string, at time.Time) error {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if derr := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error; derr != nil {
		return derr
	}
	// 同事务内读剩余行：本连接看得到自己刚删掉的结果（这正是必须同事务的一半理由 ——
	// 分开提交时第二次读会读到「删了一半」的中间态）。
	var rest []PublicationEntity
	if lerr := tx.WithContext(ctx).Model(&PublicationEntity{}).Where("page_id = ?", pageID).
		Order("published_at DESC, lang ASC").Find(&rest).Error; lerr != nil {
		return lerr
	}
	updates := map[string]any{"update_time": at}
	if len(rest) == 0 {
		updates["active_artifact_id"] = nil
		updates["active_path"] = nil
		updates["published_at"] = nil
	} else {
		updates["active_artifact_id"] = rest[0].ArtifactID
		updates["active_path"] = rest[0].ActivePath
		updates["published_at"] = rest[0].PublishedAt
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Updates(updates).Error
}
