package pagemodel

// page_publication_model.go — 页面「每语言激活状态」持久化
// （多语言 P3，docs/06-D-site-i18n.md §15.5 第 2 条）。
//
// 为什么单独成表：pages.active_path 单值只能记住一个语言的激活路径，
// Publish(en-US) 于是把 /zh-CN/about 当成「本页旧路径」取消激活。
// 激活状态按 (page_id, lang) 独立落表后，每语言各自 Publish / Rollback /
// UpdateURL / Deactivate 互不干扰；pages 的单值列退化为「最近发布语言的镜像」，
// 既有读取方（导航来源候选、列表投影）行为不变。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNamePagePublications = "page_publications"

// PublicationEntity 对应 page_publications 表。
type PublicationEntity struct {
	PageID       string    `gorm:"column:page_id;primaryKey"`
	Lang         string    `gorm:"column:lang;primaryKey"`
	ActivePath   string    `gorm:"column:active_path;not null"`
	ArtifactID   *string   `gorm:"column:artifact_id"`
	ArtifactHash string    `gorm:"column:artifact_hash;not null"`
	PublishedAt  time.Time `gorm:"column:published_at;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`
}

func (PublicationEntity) TableName() string { return tableNamePagePublications }

// PublicationRecord 单语言激活状态写入参数（ActivePath 为实际访问路径，
// 多语言开启前缀时带 /{lang}/ 前缀，与 page_routes.path 口径一致）。
type PublicationRecord struct {
	PageID       string
	Lang         string
	ActivePath   string
	ArtifactID   string
	ArtifactHash string
	PublishedAt  time.Time
}

// PublicationDB 返回已绑定 page_publications 表的 GORM 实例。
func (m *Model) PublicationDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationEntity{})
}

// GetPublication 按 (page_id, lang) 查询激活状态；不存在返回 gorm.ErrRecordNotFound。
func (m *Model) GetPublication(ctx context.Context, pageID, lang string) (e *PublicationEntity, err error) {
	e = &PublicationEntity{}
	if err = m.PublicationDB(ctx).Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListPublicationsByPages 按页面 id 批量取激活状态；lang 非空时只取该语言。
//
// 槽位解析要一次问出「这批页面在当前语言下的线上路径」——逐页问会退化成 N 次查询，
// 而它在每次构建与每次片段渲染里都会跑。
func (m *Model) ListPublicationsByPages(ctx context.Context, pageIDs []string, lang string) (list []PublicationEntity, err error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	q := m.PublicationDB(ctx).Where("page_id IN ?", pageIDs)
	if lang != "" {
		q = q.Where("lang = ?", lang)
	}
	err = q.Find(&list).Error
	return list, err
}

// ListPublications 列出页面全部语言的激活状态（语言升序，输出稳定）。
func (m *Model) ListPublications(ctx context.Context, pageID string) (list []PublicationEntity, err error) {
	err = m.PublicationDB(ctx).Where("page_id = ?", pageID).Order("lang ASC").Find(&list).Error
	return list, err
}

// MarkPublishedLang 记录某语言激活状态：page_publications upsert 与 pages 单值镜像
// 在同一事务提交（同聚合原子组合）。pages 的 active_path/active_artifact_id/published_at
// 是「最近发布语言」的镜像，只为兼容既有单值读取方，不再作为多语言真源。
func (m *Model) MarkPublishedLang(ctx context.Context, rec PublicationRecord) (err error) {
	if rec.PageID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 page_id/lang/active_path")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		row := &PublicationEntity{
			PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
			ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
		}
		if rec.ArtifactID != "" {
			id := rec.ArtifactID
			row.ArtifactID = &id
		}
		if cerr := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"active_path", "artifact_id", "artifact_hash", "published_at", "update_time",
			}),
		}).Create(row).Error; cerr != nil {
			return cerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", rec.PageID).
			Updates(map[string]any{
				"active_artifact_id": rec.ArtifactID,
				"active_path":        rec.ActivePath,
				"published_at":       rec.PublishedAt,
				"stale":              false,
				"update_time":        rec.PublishedAt,
			}).Error
	})
}

// MovePublicationPath 改 URL 后同步某语言的激活路径与 pages 单值镜像（同一事务）。
// 该语言尚无激活记录时只更新镜像（幂等，不新建行——未发布就没有激活状态）。
func (m *Model) MovePublicationPath(ctx context.Context, pageID, lang, activePath string, at time.Time) (err error) {
	if pageID == "" || lang == "" || activePath == "" {
		return errors.New("迁移激活路径缺少 page_id/lang/active_path")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := tx.Model(&PublicationEntity{}).
			Where("page_id = ? AND lang = ?", pageID, lang).
			Updates(map[string]any{"active_path": activePath, "update_time": at}).Error; uerr != nil {
			return uerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"active_path": activePath, "update_time": at}).Error
	})
}

// DeletePublications 清理页面的全部语言激活状态（软删页面时调用，幂等）。
func (m *Model) DeletePublications(ctx context.Context, pageID string) (err error) {
	return m.PublicationDB(ctx).Where("page_id = ?", pageID).Delete(&PublicationEntity{}).Error
}

const tableNamePageStagings = "page_stagings"

// StagingEntity 对应 page_stagings 表：页面在某语言下已构建、待激活的产物指针。
type StagingEntity struct {
	PageID       string    `gorm:"column:page_id;primaryKey"`
	Lang         string    `gorm:"column:lang;primaryKey"`
	ArtifactID   string    `gorm:"column:artifact_id;not null"`
	ArtifactHash string    `gorm:"column:artifact_hash;not null"`
	DraftVersion int64     `gorm:"column:draft_version;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`
}

func (StagingEntity) TableName() string { return tableNamePageStagings }

// GetStaging 按 (page_id, lang) 查询暂存指针；不存在返回 gorm.ErrRecordNotFound。
func (m *Model) GetStaging(ctx context.Context, pageID, lang string) (e *StagingEntity, err error) {
	e = &StagingEntity{}
	if err = m.db.WithContext(ctx).Model(&StagingEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// MarkStagedLang 回写某语言的暂存产物指针（构建成功、尚未激活）：
// page_stagings upsert 与 pages.staged_artifact_id 单值镜像同一事务提交。
// pages 的列只是「最近构建语言」的镜像（列表/详情投影兼容），不再作为多语言真源。
func (m *Model) MarkStagedLang(ctx context.Context, pageID, lang, artifactID, artifactHash string, draftVersion int64, at time.Time) (err error) {
	if pageID == "" || lang == "" || artifactID == "" {
		return errors.New("暂存记录缺少 page_id/lang/artifact_id")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		row := &StagingEntity{
			PageID: pageID, Lang: lang, ArtifactID: artifactID,
			ArtifactHash: artifactHash, DraftVersion: draftVersion, UpdatedAt: at,
		}
		if cerr := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"artifact_id", "artifact_hash", "draft_version", "update_time",
			}),
		}).Create(row).Error; cerr != nil {
			return cerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"staged_artifact_id": artifactID, "stale": false, "update_time": at}).Error
	})
}

// DeleteStagings 清理页面全部语言的暂存指针（软删页面时调用，幂等）。
// DeletePublicationsByLang 删除某页面某语言的发布指针（审计 I18N-017）。
//
// 与 DeletePublications 的区别是**只删一个语言**：禁用语言时其它语言的指针必须留着 ——
// 无 lang 的整页删除会把仍在服务的语言一起清掉，那些语言的页面会立刻失去「已发布」状态，
// 而访问面上产物还在（路径没被下线），于是后台与访问面开始各说各话。
func (m *Model) DeletePublicationsByLang(ctx context.Context, pageID, lang string) (err error) {
	return m.PublicationDB(ctx).Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error
}

// DeleteStagingsByLang 删除某页面某语言的暂存指针（同上，只删一个语言）。
func (m *Model) DeleteStagingsByLang(ctx context.Context, pageID, lang string) (err error) {
	return m.db.WithContext(ctx).Model(&StagingEntity{}).Where("page_id = ? AND lang = ?", pageID, lang).
		Delete(&StagingEntity{}).Error
}
func (m *Model) DeleteStagings(ctx context.Context, pageID string) (err error) {
	return m.db.WithContext(ctx).Model(&StagingEntity{}).Where("page_id = ?", pageID).
		Delete(&StagingEntity{}).Error
}

// ListCurrentArtifactIDs 返回本模块语言账本里「当前」指向的产物行 ID（去重、输出稳定）。
//
// 「当前产物」= 各语言 active/staged 指针指向的行：
//   - page_publications.artifact_id（每语言激活真源）
//   - page_stagings.artifact_id（每语言暂存指针）
//
// 供组件升级后的待重建识别使用（见 Service.MarkStaleByRegistryVersion）：判据必须是
// 「现在真正在服务的产物」，而不是 page_artifacts 里所有 payload_state='available' 的行 ——
// 后者含未 GC 的历史回滚产物，拿它比对版本会让「已重建的页面」在每次重启时被重新标成
// stale（报告 ARCH-03）。
//
// 刻意**不**并入 pages.active_artifact_id / staged_artifact_id：那两列是「最近发布/构建语言」
// 的单值镜像（见 MarkPublishedLang / MarkStagedLang 的注释），并入只会把镜像滞后或残留的
// 旧指针重新变成误标来源 —— 与本次收口的目的一致，语言账本才是真源。
//
// 存量覆盖：迁移 062 已把 pages.active_artifact_id 回填成 page_publications 行，激活侧无缺口；
// 063 未回填 page_stagings，但暂存指针由每次 Build 重写，且「只有暂存、从未发布」的页面本来
// 就不在线上服务 —— 这一小段存量不参与判定是安全的。
//
// UNION 自带去重；两张表都没有 project_id（不在迁移 215 的清单里），无需工程作用域。
func (m *Model) ListCurrentArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	err = m.db.WithContext(ctx).Raw(`
		SELECT artifact_id::text FROM page_publications WHERE artifact_id IS NOT NULL
		UNION
		SELECT artifact_id::text FROM page_stagings
	`).Scan(&ids).Error
	return ids, err
}

// ListProtectedArtifactIDs 返回「当前仍被引用、绝不可回收」的产物行 ID 集合。
//
// 集合来源（任一命中即保护）：
//   - pages.active_artifact_id / pages.staged_artifact_id（单值镜像）
//   - page_publications.artifact_id（每语言激活真源）
//   - page_stagings.artifact_id（每语言暂存指针）
//
// 供产物 GC 使用：这些产物一旦丢了文件，线上立即 404 或下次发布直接失败。
// 三张表都属本模块，单条 SQL UNION 完成，不跨模块。
func (m *Model) ListProtectedArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	err = m.db.WithContext(ctx).Raw(`
		SELECT active_artifact_id::text FROM pages
		 WHERE deleted_at IS NULL AND active_artifact_id IS NOT NULL
		UNION
		SELECT staged_artifact_id::text FROM pages
		 WHERE deleted_at IS NULL AND staged_artifact_id IS NOT NULL
		UNION
		SELECT artifact_id::text FROM page_publications WHERE artifact_id IS NOT NULL
		UNION
		SELECT artifact_id::text FROM page_stagings
	`).Scan(&ids).Error
	return ids, err
}
