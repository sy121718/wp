package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, settings JSON NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE themes (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, settings JSON NOT NULL, is_active BOOLEAN NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE pages (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, theme_id TEXT, kind TEXT NOT NULL, content_target_type TEXT NOT NULL, content_target_id TEXT, draft_path TEXT NOT NULL, active_path TEXT, draft_document JSON NOT NULL, draft_version INTEGER NOT NULL, staged_artifact_id TEXT, active_artifact_id TEXT, stale BOOLEAN NOT NULL, deleted_at DATETIME, published_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE page_revisions (id TEXT PRIMARY KEY, page_id TEXT NOT NULL, version INTEGER NOT NULL, draft_path TEXT NOT NULL, draft_document JSON NOT NULL, source_hash TEXT NOT NULL, created_at DATETIME NOT NULL, UNIQUE(page_id, version))`,
		`CREATE TABLE page_routes (project_id TEXT NOT NULL, path TEXT NOT NULL, page_id TEXT, presentation_id TEXT, route_kind TEXT NOT NULL, artifact_id TEXT, updated_at DATETIME NOT NULL, PRIMARY KEY(project_id, path))`,
		`CREATE TABLE page_artifacts (id TEXT PRIMARY KEY, page_id TEXT NOT NULL, version INTEGER NOT NULL, source_document JSON NOT NULL, page_document_schema_version INTEGER NOT NULL, source_hash TEXT NOT NULL, build_input_manifest JSON NOT NULL, build_input_hash TEXT NOT NULL, artifact_provider TEXT NOT NULL, artifact_key TEXT NOT NULL, artifact_hash TEXT NOT NULL, compiler_version TEXT NOT NULL, registry_version TEXT NOT NULL, manifest JSON NOT NULL, payload_state TEXT NOT NULL, payload_deleted_at DATETIME, note TEXT NOT NULL, created_by TEXT NOT NULL, created_at DATETIME NOT NULL, UNIQUE(page_id, version), UNIQUE(id, page_id))`,
		`CREATE TABLE content_objects (content_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, object_key TEXT NOT NULL, byte_size INTEGER NOT NULL, created_at DATETIME NOT NULL, deleted_at DATETIME)`,
		`CREATE TABLE page_artifact_objects (artifact_id TEXT NOT NULL, content_hash TEXT NOT NULL, PRIMARY KEY(artifact_id, content_hash))`,
		`CREATE TABLE publication_receipts (id TEXT PRIMARY KEY, source_type TEXT NOT NULL, source_id TEXT NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, from_artifact_id TEXT, to_artifact_id TEXT, receipt_state TEXT NOT NULL, receipt_data JSON NOT NULL, created_at DATETIME NOT NULL, completed_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	return db, pageservice.NewService(pageModel, artifacts, routes, projects), project.ID
}

func TestPageDraftLifecycle(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/about/", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if created.DraftVersion != 1 || created.DraftPath != "/about" || !created.Stale {
		t.Fatalf("初始草稿状态错误: %+v", created)
	}

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: 1, DraftPath: "/company", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if saved.DraftVersion != 2 || saved.DraftPath != "/company" || !saved.Stale {
		t.Fatalf("保存后的草稿状态错误: %+v", saved)
	}

	revisions, err := svc.ListRevisions(ctx, &pagedto.RevisionReq{PageID: created.ID})
	if err != nil {
		t.Fatalf("查询修订失败: %v", err)
	}
	if len(revisions) != 2 || revisions[0].Version != 2 || revisions[1].Version != 1 || revisions[0].SourceHash == "" {
		t.Fatalf("修订快照错误: %+v", revisions)
	}

	var paths []string
	if err := db.Table("page_routes").Where("project_id = ?", projectID).Order("path").Pluck("path", &paths).Error; err != nil {
		t.Fatalf("查询路径占用失败: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/company" {
		t.Fatalf("路径占用应仅保留最新草稿路径: %v", paths)
	}

	if _, err = svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: 1, DraftPath: "/again", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrDraftVersionConflict) {
		t.Fatalf("旧版本保存应被拒绝: %v", err)
	}
	revisions, err = svc.ListRevisions(ctx, &pagedto.RevisionReq{PageID: created.ID})
	if err != nil || len(revisions) != 2 {
		t.Fatalf("版本冲突不得写入修订: revisions=%+v err=%v", revisions, err)
	}
}

func TestPageDraftRejectsInvalidAndOccupiedPath(t *testing.T) {
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	request := &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/about", DraftDocument: json.RawMessage(pageDocument),
	}
	if _, err := svc.Create(ctx, request); err != nil {
		t.Fatalf("创建首个 Page 失败: %v", err)
	}
	if _, err := svc.Create(ctx, request); err == nil || !strings.Contains(err.Error(), pageenums.ErrPathOccupied) {
		t.Fatalf("重复路径应被拒绝: %v", err)
	}
	if _, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/%2e%2e/escape", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidPath) {
		t.Fatalf("编码路径穿越应被拒绝: %v", err)
	}
	if _, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "page", DraftPath: "/invalid-kind", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidKind) {
		t.Fatalf("非法 kind/target 组合应被拒绝: %v", err)
	}
	blankTargetID := " "
	if _, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none", ContentTargetID: &blankTargetID, DraftPath: "/blank-target", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidKind) {
		t.Fatalf("空内容目标 ID 应被拒绝: %v", err)
	}
}
