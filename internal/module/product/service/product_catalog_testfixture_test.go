package productservice_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
)

// catalogProjectStub 给公开目录用例提供工程端口：List 交工程行，
// DefaultLocale 复刻 pipeline.DefaultLocale 实际调用的那个方法。
type catalogProjectStub struct {
	projectcontract.ProjectService
	rows        []projectdto.ProjectResp
	defaultLang string
}

func (s catalogProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, nil
}

func (s catalogProjectStub) DefaultLocale(context.Context, string) (string, error) {
	return s.defaultLang, nil
}

func newCatalogProjectStub(projectID, defaultLang string) catalogProjectStub {
	return catalogProjectStub{
		rows:        []projectdto.ProjectResp{{ID: projectID, Name: "目录工程"}},
		defaultLang: defaultLang,
	}
}

// seedCatalogPublication 造一条真实发布链：模板 → 模板版本 → 实例 → 快照 → 产物 → 语言激活行。
// artifactPath 传空串表示「有实例但没有产物」，pathOverride 非 nil 时用它写 active_path
// （用来造全空白路径），两者互不影响。
func seedCatalogPublication(t *testing.T, db *gorm.DB, projectID, entityType, entityID, role, lang, artifactPath, urlPath string) {
	t.Helper()
	if urlPath == "" {
		urlPath = "/seed/" + entityID
	}
	templateID := uuid.NewString()
	versionID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO content_templates (id, project_id, name, entity_type, draft_document, create_time, update_time) "+
			"VALUES (?, ?, '目录模板', ?, '{}'::jsonb, now(), now())",
		templateID, projectID, entityType).Error; err != nil {
		t.Fatalf("插入目录模板失败：%v", err)
	}
	if err := db.Exec(
		"INSERT INTO content_template_versions (id, template_id, version, document, source_hash, created_by, create_time) "+
			"VALUES (?, ?, 1, '{}'::jsonb, 'hash', ?, now())",
		versionID, templateID, uuid.NewString()).Error; err != nil {
		t.Fatalf("插入目录模板版本失败：%v", err)
	}
	instanceID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO presentation_instances (id, project_id, entity_type, entity_id, instance_role, url_path, template_id, create_time, update_time) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, now(), now())",
		instanceID, projectID, entityType, entityID, role, urlPath, templateID).Error; err != nil {
		t.Fatalf("插入发布实例失败：%v", err)
	}
	snapshotID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO document_snapshots (id, presentation_instance_id, source_template_version_id, source_entity_revision_id, document, create_time) "+
			"VALUES (?, ?, ?, ?, '{}'::jsonb, now())",
		snapshotID, instanceID, versionID, uuid.NewString()).Error; err != nil {
		t.Fatalf("插入文档快照失败：%v", err)
	}
	var artifactID any
	if artifactPath != "" {
		id := uuid.NewString()
		if err := db.Exec(
			"INSERT INTO presentation_artifacts (id, presentation_instance_id, snapshot_id, version, source_hash, "+
				"build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version, "+
				"manifest, lang, created_by, create_time) "+
				"VALUES (?, ?, ?, 1, 'src', 'in', 'local', ?, 'hash', 'v1', 'v1', '{}'::jsonb, ?, ?, now())",
			id, instanceID, snapshotID, artifactPath, lang, uuid.NewString()).Error; err != nil {
			t.Fatalf("插入发布产物失败：%v", err)
		}
		artifactID = id
	}
	if err := db.Exec(
		"INSERT INTO presentation_publications (presentation_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time) "+
			"VALUES (?, ?, ?, ?, 'hash', now(), now())",
		instanceID, lang, artifactPath, artifactID).Error; err != nil {
		t.Fatalf("插入发布语言行失败：%v", err)
	}
}
