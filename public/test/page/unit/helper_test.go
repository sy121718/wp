// Package unit page 模块 service 层单元测试。
//
// 覆盖 Page Document 生命周期：创建/保存草稿、查询、主题合入、
// 发布链路与软删语义。依赖按 NewService 签名注入真实实现
// （project/artifact/block/publication service，同一 PG schema）。
package unit

import (
	"context"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// pageDocument 最小合法页面文档（空 root）。
const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// headingDocument 含一个 heading 节点的合法文档。
const headingDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

// newPageService 装配 page service 及其全部真实依赖（同一 PG schema）。
// 返回 db（供直接 SQL 断言）、svc、projects（供主题用例）、projectID。
func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil, nil, ""
	}
	for _, statement := range []string{
		// uuid/jsonb 列与生产 DDL（public/migrations/init_builder_schema.sql）对齐：
		// pages.draft_document 为 jsonb，RefreshThemeForTheme 的 jsonb_set 依赖该类型。
		`CREATE TABLE projects (id UUID PRIMARY KEY, name TEXT NOT NULL, settings JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE themes (id UUID PRIMARY KEY, project_id UUID NOT NULL, name TEXT NOT NULL, settings JSONB NOT NULL, is_active BOOLEAN NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE pages (id UUID PRIMARY KEY, project_id UUID NOT NULL, theme_id UUID, kind TEXT NOT NULL, content_target_type TEXT NOT NULL, content_target_id UUID, draft_path TEXT NOT NULL, active_path TEXT, draft_document JSONB NOT NULL, draft_version INTEGER NOT NULL, staged_artifact_id UUID, active_artifact_id UUID, stale BOOLEAN NOT NULL, deleted_at TIMESTAMPTZ, published_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE page_revisions (id UUID PRIMARY KEY, page_id UUID NOT NULL, version INTEGER NOT NULL, draft_path TEXT NOT NULL, draft_document JSONB NOT NULL, source_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, UNIQUE(page_id, version))`,
		`CREATE TABLE page_routes (project_id UUID NOT NULL, path TEXT NOT NULL, page_id UUID, presentation_id UUID, route_kind TEXT NOT NULL, artifact_id UUID, updated_at TIMESTAMPTZ NOT NULL, PRIMARY KEY(project_id, path))`,
		`CREATE TABLE page_artifacts (id UUID PRIMARY KEY, page_id UUID NOT NULL, version INTEGER NOT NULL, source_document JSONB NOT NULL, page_document_schema_version INTEGER NOT NULL, source_hash TEXT NOT NULL, build_input_manifest JSONB NOT NULL, build_input_hash TEXT NOT NULL, artifact_provider TEXT NOT NULL, artifact_key TEXT NOT NULL, artifact_hash TEXT NOT NULL, compiler_version TEXT NOT NULL, registry_version TEXT NOT NULL, manifest JSONB NOT NULL, payload_state TEXT NOT NULL, payload_deleted_at TIMESTAMPTZ, note TEXT NOT NULL, created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL, UNIQUE(page_id, version), UNIQUE(id, page_id))`,
		`CREATE TABLE content_objects (content_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, object_key TEXT NOT NULL, byte_size INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE page_artifact_objects (artifact_id UUID NOT NULL, content_hash TEXT NOT NULL, PRIMARY KEY(artifact_id, content_hash))`,
		`CREATE TABLE publication_receipts (id UUID PRIMARY KEY, source_type TEXT NOT NULL, source_id UUID NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, from_artifact_id UUID, to_artifact_id UUID, receipt_state TEXT NOT NULL, receipt_data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ)`,
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
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return db, pageservice.NewService(pageModel, artifacts, routes, projects, blocks), projects, project.ID
}

// createPage 创建指定路径的页面并返回投影。
func createPage(t *testing.T, svc pagecontract.PageService, projectID, path, doc string) *pagedto.PageResp {
	t.Helper()
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建 Page(%s) 失败: %v", path, err)
	}
	return created
}

// buildAndPublish 构建并发布页面，返回暂存 hash。
func buildAndPublish(t *testing.T, svc pagecontract.PageService, pageID string) string {
	t.Helper()
	ctx := context.Background()
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: pageID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	return built.StagedHash
}

// routeKind 读取指定路径的路由占用状态。
func routeKind(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, path).Scan(&kind).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	return kind
}
