package i18n

import (
	"context"
	"errors"
	"strings"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

var ErrContentStoreUnavailable = errors.New("内容译文存储不可用")

const contentTranslationQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2
ORDER BY source_hash, context, (project_id IS NULL) DESC, update_time DESC`

const contentTranslationProjectQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3::uuid)
ORDER BY source_hash, context, (project_id IS NOT NULL) DESC, update_time DESC`

type contentTranslationRow struct {
	SourceHash string `gorm:"column:source_hash"`
	Context    string `gorm:"column:context"`
	TargetText string `gorm:"column:target_text"`
}

type ContentStore interface {
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
}

type DBContentStore struct {
	db        *gorm.DB
	projectID string
}

func NewDBContentStore(db *gorm.DB) *DBContentStore {
	return &DBContentStore{db: db}
}

func NewDBContentStoreForProject(db *gorm.DB, projectID string) *DBContentStore {
	return &DBContentStore{db: db, projectID: strings.TrimSpace(projectID)}
}

type ProjectScopedStore interface {
	ForProject(projectID string) ContentStore
}

func ContentStoreForProject(store ContentStore, projectID string) ContentStore {
	if store == nil {
		return nil
	}
	if scoped, ok := store.(ProjectScopedStore); ok {
		return scoped.ForProject(projectID)
	}
	return store
}

func (s *DBContentStore) ForProject(projectID string) ContentStore {
	if s == nil {
		return nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return s
	}
	clone := *s
	clone.projectID = projectID
	return &clone
}

func (s *DBContentStore) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrContentStoreUnavailable
	}
	return loadContentTargets(ctx, s.db, s.projectID, lang, hashes)
}

func LoadContentTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	return LoadContentTargetsForProject(ctx, "", lang, hashes)
}

func LoadContentTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return loadContentTargets(ctx, db, projectID, lang, hashes)
}

func loadContentTargets(ctx context.Context, db *gorm.DB, projectID, lang string, hashes []string) (map[string]string, error) {
	targets := make(map[string]string)

	lang = strings.TrimSpace(lang)
	if db == nil {
		return nil, ErrContentStoreUnavailable
	}
	if lang == "" || len(hashes) == 0 {
		return targets, nil
	}

	projectID = strings.TrimSpace(projectID)
	var rows []contentTranslationRow
	if projectID == "" {
		if err := db.WithContext(ctx).Raw(contentTranslationQuery, hashes, lang).Scan(&rows).Error; err != nil {
			return nil, err
		}
	} else {

		if err := rls.InProjectScope(ctx, db, projectID, func(tx *gorm.DB) error {
			return tx.Raw(contentTranslationProjectQuery, hashes, lang, projectID).Scan(&rows).Error
		}); err != nil {
			return nil, err
		}
	}

	for _, row := range rows {

		if row.TargetText == "" {
			continue
		}
		targets[ContentIndexKey(row.SourceHash, row.Context)] = row.TargetText
	}
	return targets, nil
}

func defaultContentStore() ContentStore {
	db, err := database.GetDB()
	if err != nil {
		logContentStoreFailure("", err)
		return nil
	}
	return NewDBContentStore(db)
}

func defaultProjectContentStore(projectID string) ContentStore {
	db, err := database.GetDB()
	if err != nil {
		logContentStoreFailure("", err)
		return nil
	}
	return NewDBContentStoreForProject(db, projectID)
}

func logContentStoreFailure(lang string, err error) {
	fields := map[string]any{"component": "sys_translation"}
	if lang != "" {
		fields["lang"] = lang
	}
	logger.WithFields(fields).Warn("内容译文取数失败，本次构建回退原文: " + err.Error())
}
