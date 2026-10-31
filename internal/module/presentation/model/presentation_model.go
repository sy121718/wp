package presentationmodel

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	tableNamePresentationInstances    = "presentation_instances"
	tableNameDocumentSnapshots        = "document_snapshots"
	tableNamePresentationArtifacts    = "presentation_artifacts"
	tableNamePresentationDependencies = "presentation_dependencies"
)

const (
	InstanceRoleDetail = "detail"

	InstanceRoleArchive = "archive"
)

func IsValidInstanceRole(role string) bool {
	return role == InstanceRoleDetail || role == InstanceRoleArchive
}

type InstanceEntity struct {
	ID         string `gorm:"column:id;primaryKey"`
	ProjectID  string `gorm:"column:project_id;not null"`
	EntityType string `gorm:"column:entity_type;not null"`
	EntityID   string `gorm:"column:entity_id;not null"`

	InstanceRole string `gorm:"column:instance_role;not null;default:detail"`
	URLPath      string `gorm:"column:url_path;not null"`
	TemplateID   string `gorm:"column:template_id;not null"`

	OverrideDocument json.RawMessage `gorm:"column:override_document;type:jsonb"`

	RenderMode        string  `gorm:"column:render_mode;not null;default:template"`
	CurrentSnapshotID *string `gorm:"column:current_snapshot_id"`
	StagedSnapshotID  *string `gorm:"column:staged_snapshot_id"`
	StagedArtifactID  *string `gorm:"column:staged_artifact_id"`
	ActiveArtifactID  *string `gorm:"column:active_artifact_id"`
	Stale             bool    `gorm:"column:stale;not null"`

	InputVersion int64 `gorm:"column:input_version;not null"`

	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	CreatedAt   time.Time  `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time  `gorm:"column:update_time;not null"`
}

func (InstanceEntity) TableName() string { return tableNamePresentationInstances }

type SnapshotEntity struct {
	ID                      string          `gorm:"column:id;primaryKey"`
	PresentationInstanceID  string          `gorm:"column:presentation_instance_id;not null"`
	SourceTemplateVersionID string          `gorm:"column:source_template_version_id;not null"`
	SourceEntityRevisionID  string          `gorm:"column:source_entity_revision_id;not null"`
	Document                json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt               time.Time       `gorm:"column:create_time;not null"`
}

func (SnapshotEntity) TableName() string { return tableNameDocumentSnapshots }

type ArtifactEntity struct {
	ID                     string `gorm:"column:id;primaryKey"`
	PresentationInstanceID string `gorm:"column:presentation_instance_id;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:1"`
	SnapshotID             string `gorm:"column:snapshot_id;not null"`
	Version                int64  `gorm:"column:version;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:2"`

	Lang             string          `gorm:"column:lang;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:3"`
	SourceHash       string          `gorm:"column:source_hash;not null"`
	BuildInputHash   string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey      string          `gorm:"column:artifact_key;not null"`
	ArtifactHash     string          `gorm:"column:artifact_hash;not null"`
	CompilerVersion  string          `gorm:"column:compiler_version;not null"`
	RegistryVersion  string          `gorm:"column:registry_version;not null"`
	Manifest         json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState     string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt *time.Time      `gorm:"column:payload_deleted_at"`
	Note             string          `gorm:"column:note;not null"`
	CreatedBy        string          `gorm:"column:created_by;not null"`
	CreatedAt        time.Time       `gorm:"column:create_time;not null"`
}

func (ArtifactEntity) TableName() string { return tableNamePresentationArtifacts }

type DependencyEntity struct {
	PresentationID string    `gorm:"column:presentation_id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;primaryKey"`
	DependencyKind string    `gorm:"column:dependency_kind;primaryKey"`
	DependencyKey  string    `gorm:"column:dependency_key;primaryKey"`
	Revision       *string   `gorm:"column:revision"`
	LastChecked    time.Time `gorm:"column:last_checked;not null"`
}

func (DependencyEntity) TableName() string { return tableNamePresentationDependencies }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) InstanceDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&InstanceEntity{})
}

func (m *Model) SnapshotDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SnapshotEntity{})
}

func (m *Model) ArtifactDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ArtifactEntity{})
}

func (m *Model) DependencyDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&DependencyEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

var ErrCommitUncertain = errors.New("presentation: 事务提交结果不明")

var ErrInstanceNil = errors.New("presentation: 实体 id 为空，无法取顾问锁")

func (m *Model) InTransaction(ctx context.Context, fn func(tx *gorm.DB) error) (callbackCompleted, uncertain bool, err error) {
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rerr := fn(tx)
		if rerr == nil {
			callbackCompleted = true
		}
		return rerr
	})
	if err != nil && callbackCompleted {
		return true, true, fmt.Errorf("%w: %v", ErrCommitUncertain, err)
	}
	return callbackCompleted, false, err
}

func (m *Model) CreateInstance(ctx context.Context, e *InstanceEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).Create(e).Error
	})
}

func (m *Model) GetInstance(ctx context.Context, projectID, id string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) GetInstanceByEntity(ctx context.Context, projectID, entityType, entityID string) (e *InstanceEntity, err error) {
	return m.GetInstanceByEntityRole(ctx, projectID, entityType, entityID, "detail")
}

func (m *Model) GetInstanceByEntityRole(ctx context.Context, projectID, entityType, entityID, role string) (e *InstanceEntity, err error) {
	if role == "" {
		role = "detail"
	}
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND instance_role = ?",
				projectID, entityType, entityID, role).
			First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) ListInstances(ctx context.Context, projectID, entityType string) (list []*InstanceEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).Where("project_id = ?", projectID).
			Order("update_time DESC, id DESC")
		if entityType != "" {
			q = q.Where("entity_type = ?", entityType)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) UpdateInstancePointers(ctx context.Context, e *InstanceEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("id = ? AND project_id = ?", e.ID, e.ProjectID).Updates(map[string]any{
			"current_snapshot_id": e.CurrentSnapshotID,
			"staged_snapshot_id":  e.StagedSnapshotID,
			"staged_artifact_id":  e.StagedArtifactID,
			"active_artifact_id":  e.ActiveArtifactID,
			"stale":               e.Stale,
			"published_at":        e.PublishedAt,
			"update_time":         e.UpdatedAt,
		}).Error
	})
}

const payloadStateAvailable = "available"

func (m *Model) ListStaleInstanceIDsByRegistryVersion(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if len(artifactIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("payload_state = ? AND registry_version <> ? AND id = ANY(string_to_array(?, ',')::uuid[])",
			payloadStateAvailable, current, strings.Join(artifactIDs, ",")).
		Distinct().Order("presentation_instance_id").Pluck("presentation_instance_id", &ids).Error
	return ids, err
}

func (m *Model) ListStaleInstanceIDs(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND stale", projectID).
			Order("update_time ASC, id ASC").
			Pluck("id", &ids).Error
	})
	return ids, err
}

func (m *Model) MarkStaleByIDs(ctx context.Context, projectID string, ids []string, at time.Time) (marked []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project id is required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.Model(&rows).
			Where("project_id = ? AND deleted_at IS NULL AND id IN ?", projectID, ids).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(at)).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		marked = append(marked, r.ID)
	}
	return marked, nil
}

func staleMarkUpdates(at time.Time) map[string]any {
	return map[string]any{
		"stale":         true,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}
}

func (m *Model) MarkStale(ctx context.Context, projectID string, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).
			Updates(staleMarkUpdates(at))
		n = res.RowsAffected
		return res.Error
	})
	return n, err
}

func (m *Model) MarkStaleForI18n(ctx context.Context, projectID string, at time.Time) (n int64, err error) {
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var merr error
		n, merr = m.markStaleForI18nIn(ctx, tx, projectID, at)
		return merr
	})
	return n, err
}

func (m *Model) markStaleForI18nIn(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (n int64, err error) {
	res := tx.WithContext(ctx).Model(&InstanceEntity{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Updates(staleMarkUpdates(at))
	return res.RowsAffected, res.Error
}

func (m *Model) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (n int64, err error) {
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	return m.markStaleForI18nIn(ctx, tx, projectID, at)
}

func (m *Model) DeleteInstance(ctx context.Context, projectID, id string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deleteInstanceRows(tx, projectID, id)
	})
}

func (m *Model) DeleteInstanceTx(tx *gorm.DB, projectID, id string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return m.deleteInstanceRows(tx, projectID, id)
}

func (m *Model) deleteInstanceRows(tx *gorm.DB, projectID, id string) error {
	if err := tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"current_snapshot_id": nil,
		"staged_snapshot_id":  nil,
		"staged_artifact_id":  nil,
		"active_artifact_id":  nil,
	}).Error; err != nil {
		return err
	}
	if err := tx.Where("presentation_id = ?", id).Delete(&DependencyEntity{}).Error; err != nil {
		return err
	}
	if err := tx.Where("presentation_instance_id = ?", id).Delete(&ArtifactEntity{}).Error; err != nil {
		return err
	}
	if err := tx.Where("presentation_instance_id = ?", id).Delete(&SnapshotEntity{}).Error; err != nil {
		return err
	}
	return tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&InstanceEntity{}).Error
}

func (m *Model) CreateSnapshot(ctx context.Context, e *SnapshotEntity) error {
	return m.SnapshotDB(ctx).Create(e).Error
}

func (m *Model) GetSnapshot(ctx context.Context, id string) (e *SnapshotEntity, err error) {
	var row SnapshotEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
func (m *Model) NextArtifactVersion(ctx context.Context, instanceID string) (v int64, err error) {
	var maxVersion *int64
	if err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Select("MAX(version)").Scan(&maxVersion).Error; err != nil {
		return 0, err
	}
	if maxVersion == nil {
		return 1, nil
	}
	return *maxVersion + 1, nil
}

func (m *Model) ListArtifactHashes(ctx context.Context) (hashes []string, err error) {
	hashes = []string{}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("artifact_hash <> ''").
		Distinct().
		Pluck("artifact_hash", &hashes).Error
	return hashes, err
}

func (m *Model) CreateArtifact(ctx context.Context, e *ArtifactEntity) error {
	return m.ArtifactDB(ctx).Create(e).Error
}

func (m *Model) GetArtifactByHash(ctx context.Context, instanceID, hash string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = m.db.WithContext(ctx).
		Where("presentation_instance_id = ? AND artifact_hash = ?", instanceID, hash).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) FindArtifactIDByHash(ctx context.Context, instanceID, hash string) (id string, err error) {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(hash) == "" {
		return "", gorm.ErrRecordNotFound
	}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("presentation_instance_id = ? AND artifact_hash = ?", instanceID, hash).
		Select("id").Limit(1).Scan(&id).Error
	if err != nil {
		return "", err
	}
	if id == "" {

		return "", gorm.ErrRecordNotFound
	}
	return id, nil
}

func (m *Model) GetArtifact(ctx context.Context, id string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) GetArtifactHash(ctx context.Context, id string) (hash string, err error) {
	if strings.TrimSpace(id) == "" {
		return "", gorm.ErrRecordNotFound
	}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("id = ?", id).Select("artifact_hash").Scan(&hash).Error
	if err != nil {
		return "", err
	}
	if hash == "" {

		return "", gorm.ErrRecordNotFound
	}
	return hash, nil
}

func (m *Model) ReplaceDependencies(ctx context.Context, artifactID string, rows []DependencyEntity) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if derr := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 200).Error
	})
}

func (m *Model) CreateSnapshotTx(tx *gorm.DB, e *SnapshotEntity) error {
	return tx.Create(e).Error
}

func (m *Model) GetArtifactByHashTx(tx *gorm.DB, instanceID, hash string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = tx.Where("presentation_instance_id = ? AND artifact_hash = ?", instanceID, hash).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) NextArtifactVersionTx(tx *gorm.DB, instanceID string) (v int64, err error) {
	var maxVersion *int64
	if err = tx.Model(&ArtifactEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Select("MAX(version)").Scan(&maxVersion).Error; err != nil {
		return 0, err
	}
	if maxVersion == nil {
		return 1, nil
	}
	return *maxVersion + 1, nil
}

func (m *Model) CreateArtifactTx(tx *gorm.DB, e *ArtifactEntity) error {
	return tx.Create(e).Error
}

func (m *Model) UpdateInstancePointersTx(tx *gorm.DB, projectID string, e *InstanceEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", e.ID, projectID).Updates(map[string]any{
		"current_snapshot_id": e.CurrentSnapshotID,
		"staged_snapshot_id":  e.StagedSnapshotID,
		"staged_artifact_id":  e.StagedArtifactID,
		"active_artifact_id":  e.ActiveArtifactID,
		"stale":               e.Stale,
		"published_at":        e.PublishedAt,
		"update_time":         e.UpdatedAt,
	}).Error
}

func (m *Model) UpdateInstanceTemplateTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"template_id":   templateID,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}).Error
}

func (m *Model) RecordBatchTemplateTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"template_id": templateID,
		"update_time": at,
	}).Error
}

func (m *Model) RecordBatchURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"url_path":    urlPath,
		"update_time": at,
	}).Error
}

func (m *Model) UpdateInstanceURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"url_path":      urlPath,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}).Error
}

func (m *Model) UpdateInstanceOverrideTx(tx *gorm.DB, projectID, id string, document json.RawMessage, at time.Time) error {
	if len(document) == 0 {
		return errors.New("override document is empty")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"override_document": document,
		"input_version":     gorm.Expr("input_version + 1"),
		"update_time":       at,
	}).Error
}

func (m *Model) ClearInstanceOverrideTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	updates := map[string]any{
		"override_document": nil,
		"input_version":     gorm.Expr("input_version + 1"),
		"update_time":       at,
	}
	if templateID != "" {
		updates["template_id"] = templateID
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
}

func (m *Model) PersistPublishInputURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) (int64, error) {
	if strings.TrimSpace(urlPath) == "" {
		return 0, errors.New("url path is required")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}

	var rows []InstanceEntity
	err := tx.Model(&rows).
		Where("id = ? AND project_id = ?", id, projectID).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "input_version"}}}).
		Updates(map[string]any{
			"url_path":      urlPath,
			"input_version": gorm.Expr("input_version + 1"),

			"stale":       true,
			"update_time": at,
		}).Error
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, gorm.ErrRecordNotFound
	}
	return rows[0].InputVersion, nil
}

func (m *Model) MarkStaleWithoutVersion(ctx context.Context, projectID string, ids []string, at time.Time) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var terr error
		n, terr = m.MarkStaleWithoutVersionTx(tx, projectID, ids, at)
		return terr
	})
	return n, err
}

func (m *Model) MarkStaleWithoutVersionTx(tx *gorm.DB, projectID string, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
	}
	if tx == nil {
		return 0, errors.New("presentation: MarkStaleWithoutVersionTx 需要调用方事务")
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	res := tx.Model(&InstanceEntity{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Updates(map[string]any{"stale": true, "update_time": at})
	return res.RowsAffected, res.Error
}

func (m *Model) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
	}
	if !IsMenuStaleKind(kind) {
		return 0, fmt.Errorf("unknown menu kind %q", kind)
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}

	key := "menu:" + projectID + ":" + kind
	var rows []InstanceEntity
	if err := tx.WithContext(ctx).Model(&rows).
		Where("deleted_at IS NULL AND project_id = ?", projectID).
		Where("id IN (?)", presentationDepScope(tx, depKindMenu, key)).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error; err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (m *Model) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
	}
	if strings.TrimSpace(navigationID) == "" {
		return 0, errors.New("navigation id is required")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	key := depKindNavigation + ":" + navigationID
	var rows []InstanceEntity
	if err := tx.WithContext(ctx).Model(&rows).
		Where("deleted_at IS NULL AND project_id = ?", projectID).
		Where("id IN (?)", presentationDepScope(tx, depKindNavigation, key)).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error; err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

const depKindNavigation = "navigation"

const depKindMenu = "menu"

const (
	MenuStaleKindHeader       = "header"
	MenuStaleKindHeaderMobile = "header_mobile"
	MenuStaleKindFooter       = "footer"
	MenuStaleKindFooterMobile = "footer_mobile"
)

func IsMenuStaleKind(kind string) bool {
	switch kind {
	case MenuStaleKindHeader, MenuStaleKindHeaderMobile, MenuStaleKindFooter, MenuStaleKindFooterMobile:
		return true
	}
	return false
}

func (m *Model) LockInstanceForActivationTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *InstanceEntity, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var row InstanceEntity
	if err = tx.WithContext(ctx).Model(&InstanceEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

var ErrInputVersionMoved = errors.New("presentation input version moved")

func (m *Model) WriteInstanceActivationTx(tx *gorm.DB, projectID string, e *InstanceEntity, expectInputVersion int64) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	res := tx.Model(&InstanceEntity{}).
		Where("id = ? AND project_id = ? AND input_version = ?", e.ID, projectID, expectInputVersion).
		Updates(map[string]any{
			"current_snapshot_id": e.CurrentSnapshotID,
			"staged_snapshot_id":  e.StagedSnapshotID,
			"staged_artifact_id":  e.StagedArtifactID,
			"active_artifact_id":  e.ActiveArtifactID,
			"stale":               e.Stale,
			"published_at":        e.PublishedAt,
			"update_time":         e.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInputVersionMoved
	}
	return nil
}

func (m *Model) FindInstanceByPath(ctx context.Context, projectID, urlPath, excludeInstanceID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND url_path = ? AND deleted_at IS NULL", projectID, urlPath)
		if excludeInstanceID != "" {
			q = q.Where("id <> ?", excludeInstanceID)
		}
		return q.First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) ReplaceDependenciesTx(tx *gorm.DB, artifactID string, rows []DependencyEntity) error {
	if err := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, 200).Error
}

func (m *Model) ListDependencies(ctx context.Context, artifactID string) (list []DependencyEntity, err error) {
	err = m.DependencyDB(ctx).Where("artifact_id = ?", artifactID).
		Order("dependency_kind, dependency_key").Find(&list).Error
	return list, err
}

func presentationDepScope(tx *gorm.DB, kind, key string) *gorm.DB {
	return tx.Table(tableNamePresentationDependencies+" AS d").
		Select("DISTINCT d.presentation_id").
		Joins("JOIN "+tableNamePresentationInstances+" p ON p.id = d.presentation_id").
		Where("d.dependency_kind = ? AND d.dependency_key = ? AND p.deleted_at IS NULL", kind, key)
}

func (m *Model) MarkStaleByDependency(ctx context.Context, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	var rows []InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		affected := presentationDepScope(tx, kind, key).
			Where("(d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePresentationPublications + " WHERE presentation_id = p.id))")
		return tx.Model(&rows).
			Where("deleted_at IS NULL AND project_id = ?", projectID).
			Where("id IN (?)", affected).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(at)).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

type BlockDocRefRow struct {
	InstanceID   string `gorm:"column:instance_id"`
	URLPath      string `gorm:"column:url_path"`
	EntityType   string `gorm:"column:entity_type"`
	EntityID     string `gorm:"column:entity_id"`
	FromSnapshot bool   `gorm:"column:from_snapshot"`
}

func (m *Model) ListBlockDocumentRefs(ctx context.Context, projectID, blockID string) (rows []BlockDocRefRow, err error) {
	if projectID == "" {
		return nil, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		var fromOverride []BlockDocRefRow
		if qerr := tx.Model(&InstanceEntity{}).
			Select("id::text AS instance_id, url_path, entity_type, entity_id, false AS from_snapshot").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Where("jsonb_path_query_array(override_document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&fromOverride).Error; qerr != nil {
			return qerr
		}
		var fromSnapshot []BlockDocRefRow
		if qerr := tx.Table(tableNameDocumentSnapshots+" AS s").
			Select("i.id::text AS instance_id, i.url_path, i.entity_type, i.entity_id, true AS from_snapshot").
			Joins("JOIN "+tableNamePresentationInstances+" i ON i.id = s.presentation_instance_id").
			Where("i.project_id = ? AND i.deleted_at IS NULL", projectID).
			Where("jsonb_path_query_array(s.document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&fromSnapshot).Error; qerr != nil {
			return qerr
		}
		rows = append(fromOverride, fromSnapshot...)

		slices.SortStableFunc(rows, func(a, b BlockDocRefRow) int {
			if c := cmp.Compare(a.URLPath, b.URLPath); c != 0 {
				return c
			}
			if c := cmp.Compare(a.InstanceID, b.InstanceID); c != 0 {
				return c
			}
			if a.FromSnapshot == b.FromSnapshot {
				return 0
			}
			if !a.FromSnapshot {
				return -1
			}
			return 1
		})
		return nil
	})
	return rows, err
}

func (m *Model) LockPresentationEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, instanceID string) error {
	if tx == nil {
		return errors.New("presentation: 实体顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(instanceID)
	if key == "" {
		return ErrInstanceNil
	}

	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"presentation:"+key).Error
}
