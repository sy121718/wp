package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
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

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// newPageService 装配 page service 及其真实依赖。
//
// 库由**生产迁移**建（support.NewMigratedPGTestDB 跑 migrations.Run 建全部 106 张表），
// 测试只补必要的父行 —— 此前这里手抄 13 张建表语句，与生产 DDL 静默分叉
// （jsonb 写成 JSON、version 写成 INTEGER、缺 CHECK/外键），迁移一改列就整片变红。
func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	// 建站会自带一套默认主题（生产语义：先有主题再有页面）；这批用例测的是
	// 「完全没有主题时怎么办」与「自己建的主题怎么走」，前提需要干净，所以先清掉。
	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return db, pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, nil, nil), project.ID
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
