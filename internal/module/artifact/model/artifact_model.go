package artifactmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tableNamePageArtifacts       = "page_artifacts"
	tableNameContentObjects      = "content_objects"
	tableNamePageArtifactObjects = "page_artifact_objects"
)

type PageArtifactEntity struct {
	ID                        string          `gorm:"column:id;primaryKey"`
	PageID                    string          `gorm:"column:page_id;not null;uniqueIndex:uk_page_artifacts_page_hash,priority:1"`
	Version                   int64           `gorm:"column:version;not null"`
	Lang                      string          `gorm:"column:lang;not null;default:'zh-CN'"`
	SourceDocument            json.RawMessage `gorm:"column:source_document;type:jsonb;not null"`
	PageDocumentSchemaVersion int             `gorm:"column:page_document_schema_version;not null"`
	SourceHash                string          `gorm:"column:source_hash;not null"`
	BuildInputHash            string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider          string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey               string          `gorm:"column:artifact_key;not null"`
	ArtifactHash              string          `gorm:"column:artifact_hash;not null;uniqueIndex:uk_page_artifacts_page_hash,priority:2"`
	CompilerVersion           string          `gorm:"column:compiler_version;not null"`
	RegistryVersion           string          `gorm:"column:registry_version;not null"`
	Manifest                  json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState              string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt          *time.Time      `gorm:"column:payload_deleted_at"`
	Note                      string          `gorm:"column:note;not null"`
	CreatedBy                 string          `gorm:"column:created_by;not null"`
	CreatedAt                 time.Time       `gorm:"column:create_time;not null"`
}

func (PageArtifactEntity) TableName() string { return tableNamePageArtifacts }

type ContentObjectEntity struct {
	ContentHash string     `gorm:"column:content_hash;primaryKey"`
	Provider    string     `gorm:"column:provider;not null"`
	ObjectKey   string     `gorm:"column:object_key;not null"`
	ByteSize    int64      `gorm:"column:byte_size;not null"`
	CreatedAt   time.Time  `gorm:"column:create_time;not null"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

func (ContentObjectEntity) TableName() string { return tableNameContentObjects }

type PageArtifactObjectEntity struct {
	ArtifactID  string `gorm:"column:artifact_id;primaryKey"`
	ContentHash string `gorm:"column:content_hash;primaryKey"`
}

func (PageArtifactObjectEntity) TableName() string { return tableNamePageArtifactObjects }

const pageArtifactSummaryColumns = "id, page_id, version, lang, source_hash, build_input_hash, " +
	"artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version, payload_state, create_time"

type PageArtifactSummary struct {
	ID               string    `gorm:"column:id"`
	PageID           string    `gorm:"column:page_id"`
	Version          int64     `gorm:"column:version"`
	Lang             string    `gorm:"column:lang"`
	SourceHash       string    `gorm:"column:source_hash"`
	BuildInputHash   string    `gorm:"column:build_input_hash"`
	ArtifactProvider string    `gorm:"column:artifact_provider"`
	ArtifactKey      string    `gorm:"column:artifact_key"`
	ArtifactHash     string    `gorm:"column:artifact_hash"`
	CompilerVersion  string    `gorm:"column:compiler_version"`
	RegistryVersion  string    `gorm:"column:registry_version"`
	PayloadState     string    `gorm:"column:payload_state"`
	CreatedAt        time.Time `gorm:"column:create_time"`
}

const gcCandidateColumns = "id, page_id, version, lang, artifact_hash, artifact_key, create_time"

type GCCandidate struct {
	ID           string    `gorm:"column:id"`
	PageID       string    `gorm:"column:page_id"`
	Version      int64     `gorm:"column:version"`
	Lang         string    `gorm:"column:lang"`
	ArtifactHash string    `gorm:"column:artifact_hash"`
	ArtifactKey  string    `gorm:"column:artifact_key"`
	CreatedAt    time.Time `gorm:"column:create_time"`
}

type Model struct {
	db *gorm.DB
}

func NewArtifactModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageArtifactEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) GetByHash(ctx context.Context, pageID, hash string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("page_id = ? AND artifact_hash = ?", pageID, hash).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) CreateArtifactTx(ctx context.Context, tx *gorm.DB, e *PageArtifactEntity) error {
	return tx.WithContext(ctx).Create(e).Error
}

func (m *Model) CreateArtifactObjectTx(ctx context.Context, tx *gorm.DB, o *PageArtifactObjectEntity) error {
	return tx.WithContext(ctx).Create(o).Error
}

func (m *Model) EnsureContentObjectTx(ctx context.Context, tx *gorm.DB, contentHash, provider, objectKey string, now time.Time) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "content_hash"}},
		DoNothing: true,
	}).Create(&ContentObjectEntity{
		ContentHash: contentHash,
		Provider:    provider,
		ObjectKey:   objectKey,
		ByteSize:    0,
		CreatedAt:   now,
	}).Error
}

func (m *Model) GetByID(ctx context.Context, id string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

const (
	PayloadStateAvailable = "available"

	PayloadStateGCPending = "gc_pending"

	PayloadStateDeleted = "deleted"
)

func (m *Model) ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if len(artifactIDs) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).
		Where("payload_state = ? AND registry_version <> ? AND id = ANY(string_to_array(?, ',')::uuid[])",
			PayloadStateAvailable, current, strings.Join(artifactIDs, ",")).
		Distinct().Order("page_id").Pluck("page_id", &ids).Error
	return ids, err
}

func (m *Model) ListPageArtifactHashes(ctx context.Context) (hashes []string, err error) {
	hashes = []string{}
	err = m.DB(ctx).Where("artifact_hash <> ''").Distinct().Order("artifact_hash").
		Pluck("artifact_hash", &hashes).Error
	return hashes, err
}

func (m *Model) PageArtifactHashByID(ctx context.Context, id string) (hash string, err error) {
	if strings.TrimSpace(id) == "" {
		return "", nil
	}
	var row struct {
		ArtifactHash string `gorm:"column:artifact_hash"`
	}
	if err = m.DB(ctx).Select("artifact_hash").Where("id = ?", id).Scan(&row).Error; err != nil {
		return "", err
	}
	return row.ArtifactHash, nil
}

func (m *Model) PageArtifactPageID(ctx context.Context, id string) (pageID string, err error) {
	if strings.TrimSpace(id) == "" {
		return "", nil
	}
	var row struct {
		PageID string `gorm:"column:page_id"`
	}
	if err = m.DB(ctx).Select("page_id").Where("id = ?", id).Scan(&row).Error; err != nil {
		return "", err
	}
	return row.PageID, nil
}

type pageArtifactMissRow struct {
	PageID     string `gorm:"column:page_id"`
	Lang       string `gorm:"column:lang"`
	Version    int64  `gorm:"column:version"`
	Misses     int64  `gorm:"column:misses"`
	Candidates int64  `gorm:"column:candidates"`
}

func (m *Model) TranslationMisses(ctx context.Context, pageIDs []string) (rows []pageArtifactMissRow, err error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).
		Select("DISTINCT ON (page_id, lang) page_id, lang, version, "+
			"COALESCE((manifest->'translationMisses'->>'misses')::bigint, 0) AS misses, "+
			"COALESCE((manifest->'translationMisses'->>'candidates')::bigint, 0) AS candidates").
		Where("page_id = ANY(string_to_array(?, ',')::uuid[])", strings.Join(pageIDs, ",")).
		Order("page_id, lang, version DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	kept := make([]pageArtifactMissRow, 0, len(rows))
	for i := range rows {
		if rows[i].Misses > 0 {
			kept = append(kept, rows[i])
		}
	}
	return kept, nil
}

func (m *Model) ListByPage(ctx context.Context, pageID string) (list []PageArtifactSummary, err error) {
	err = m.DB(ctx).Select(pageArtifactSummaryColumns).
		Where("page_id = ?", pageID).Order("version DESC, lang ASC").Find(&list).Error
	return list, err
}

func (m *Model) ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []GCCandidate, err error) {
	if len(excludeIDs) == 0 {
		return nil, nil
	}
	q := m.DB(ctx).Select(gcCandidateColumns).
		Where("payload_state = ? AND create_time < ?", PayloadStateAvailable, before)
	q = q.Where("id NOT IN ?", excludeIDs)
	err = q.Order("create_time ASC").Find(&list).Error
	return list, err
}

func (m *Model) CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error) {
	err = m.DB(ctx).
		Where("artifact_hash = ? AND payload_state = ? AND id <> ?", hash, PayloadStateAvailable, excludeID).
		Count(&n).Error
	return n, err
}

const orphanContentObjectFilter = `NOT EXISTS (
		SELECT 1 FROM page_artifact_objects o
		JOIN page_artifacts a ON a.id = o.artifact_id
		WHERE o.content_hash = content_objects.content_hash AND a.payload_state <> ?
	)`

func (m *Model) orphanContentObjectScope(ctx context.Context, before time.Time, hashes []string) *gorm.DB {
	q := m.db.WithContext(ctx).Table(tableNameContentObjects).
		Where("content_objects.create_time < ?", before).
		Where(orphanContentObjectFilter, PayloadStateDeleted)
	if len(hashes) > 0 {
		q = q.Where("content_objects.content_hash IN ?", hashes)
	}
	return q
}

func (m *Model) CountOrphanContentObjects(ctx context.Context, before time.Time) (n int64, err error) {
	err = m.orphanContentObjectScope(ctx, before, nil).Count(&n).Error
	return n, err
}

func (m *Model) ListOrphanContentObjects(ctx context.Context, before time.Time, limit int) (list []ContentObjectEntity, err error) {
	q := m.orphanContentObjectScope(ctx, before, nil).Select("content_objects.*").Order("content_objects.create_time ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

func (m *Model) ListStillOrphanHashes(ctx context.Context, hashes []string, before time.Time) (list []string, err error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	type hashRow struct {
		ContentHash string
	}
	var rows []hashRow
	err = m.orphanContentObjectScope(ctx, before, hashes).
		Select("content_objects.content_hash").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list = make([]string, 0, len(rows))
	for _, r := range rows {
		list = append(list, r.ContentHash)
	}
	return list, nil
}

func (m *Model) DeleteOrphanContentObjects(ctx context.Context, hashes []string, before time.Time) (deleted []string, err error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	var gone []ContentObjectEntity
	err = m.orphanContentObjectScope(ctx, before, hashes).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "content_hash"}}}).
		Delete(&gone).Error
	if err != nil {
		return nil, err
	}
	for i := range gone {
		deleted = append(deleted, gone[i].ContentHash)
	}
	return deleted, nil
}

func (m *Model) MarkPayloadState(ctx context.Context, ids []string, state string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	updates := map[string]any{"payload_state": state}
	if state == PayloadStateDeleted {
		updates["payload_deleted_at"] = at
	}
	res := m.DB(ctx).Where("id IN ?", ids).Updates(updates)
	return res.RowsAffected, res.Error
}
