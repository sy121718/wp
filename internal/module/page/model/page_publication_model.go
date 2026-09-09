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
	PageID       string    `gorm:"column:page_id;type:uuid;primaryKey"`
	Lang         string    `gorm:"column:lang;type:text;primaryKey"`
	ActivePath   string    `gorm:"column:active_path;type:text;not null"`
	ArtifactID   *string   `gorm:"column:artifact_id;type:uuid"`
	ArtifactHash string    `gorm:"column:artifact_hash;type:text;not null"`
	PublishedAt  time.Time `gorm:"column:published_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
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
				"active_path", "artifact_id", "artifact_hash", "published_at", "updated_at",
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
				"updated_at":         rec.PublishedAt,
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
			Updates(map[string]any{"active_path": activePath, "updated_at": at}).Error; uerr != nil {
			return uerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"active_path": activePath, "updated_at": at}).Error
	})
}

// DeletePublications 清理页面的全部语言激活状态（软删页面时调用，幂等）。
func (m *Model) DeletePublications(ctx context.Context, pageID string) (err error) {
	return m.PublicationDB(ctx).Where("page_id = ?", pageID).Delete(&PublicationEntity{}).Error
}
const tableNamePageStagings = "page_stagings"

// StagingEntity 对应 page_stagings 表：页面在某语言下已构建、待激活的产物指针。
type StagingEntity struct {
	PageID       string    `gorm:"column:page_id;type:uuid;primaryKey"`
	Lang         string    `gorm:"column:lang;type:text;primaryKey"`
	ArtifactID   string    `gorm:"column:artifact_id;type:uuid;not null"`
	ArtifactHash string    `gorm:"column:artifact_hash;type:text;not null"`
	DraftVersion int64     `gorm:"column:draft_version;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
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
				"artifact_id", "artifact_hash", "draft_version", "updated_at",
			}),
		}).Create(row).Error; cerr != nil {
			return cerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"staged_artifact_id": artifactID, "stale": false, "updated_at": at}).Error
	})
}

// DeleteStagings 清理页面全部语言的暂存指针（软删页面时调用，幂等）。
func (m *Model) DeleteStagings(ctx context.Context, pageID string) (err error) {
	return m.db.WithContext(ctx).Model(&StagingEntity{}).Where("page_id = ?", pageID).
		Delete(&StagingEntity{}).Error
}
