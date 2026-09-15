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
// 表结构由生产迁移建立（support.NewMigratedPGTestDB，真实 106 张表），不再手抄 DDL；
// 测试只补真实父行（这里通过 project service 建一个真实工程）。
// 返回 db（供直接 SQL 断言）、svc、projects（供主题用例）、projectID。
func newPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
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
	// 「建站即有默认主题」由 public/test/project/feature 的用例守着。
	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return db, pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, nil, nil), projects, project.ID
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
