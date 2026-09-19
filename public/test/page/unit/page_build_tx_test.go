package unit

// page_build_tx_test.go —— 构建主链的事务边界（2026-09-19 事务审计：中优先第 3 条）。
//
// 构建成功时要落三处：产物元数据行（artifact 模块的幂等归档）、依赖记录
//（page_dependencies）、暂存指针（page_stagings + pages 镜像）。修复前依赖写失败
// 只记一行日志就继续，于是那一页在依赖源变更时不再被精确标 stale —— 内容改了、
// 页面不重建，站点长期显示旧内容，而错误不在任何返回值里。
//
// 下面用「返回一个不存在的产物行 id」注入依赖写失败：它让 requireArtifactOwned
// 判定越界（gorm.ErrRecordNotFound），从而证明构建**整体失败**而不是降级。

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactcontract "go_wp/internal/module/artifact/contract"
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
)

// bogusArtifactRow 包装 artifact 契约：归档时返回一个不存在的产物行 id。
// 用于让紧随其后的依赖记录写入失败（归属校验查不到该产物行）。
type bogusArtifactRow struct {
	artifactcontract.ArtifactService
}

func (bogusArtifactRow) EnsureRecord(context.Context, *artifactcontract.RecordReq) (*artifactcontract.ArtifactResp, error) {
	return &artifactcontract.ArtifactResp{ID: uuid.NewString()}, nil
}

// TestBuildFailsWhenDependencyWriteFails 依赖记录写失败 = 整次构建失败（不再降级）。
func TestBuildFailsWhenDependencyWriteFails(t *testing.T) {
	db, svc, projectID := newPageServiceWithArtifact(t, func(real artifactcontract.ArtifactService) artifactcontract.ArtifactService {
		return bogusArtifactRow{ArtifactService: real}
	})
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/deps-fail", headingDocument)

	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err == nil {
		t.Fatal("依赖记录写失败应让构建返回错误（修复前是只记日志、构建成功）")
	}
	// 同事务：暂存指针不得落库（构建失败就等于这次构建没有发生）。
	var staged int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_stagings WHERE page_id = ?`, page.ID).Scan(&staged).Error; err != nil {
		t.Fatalf("统计暂存指针失败: %v", err)
	}
	if staged != 0 {
		t.Fatalf("构建失败时暂存指针不应落库，实际 %d 行", staged)
	}
}

// newPageServiceWithArtifact 同 newPageService，但允许包装 artifact 契约（故障注入用）。
func newPageServiceWithArtifact(t *testing.T, wrap func(artifactcontract.ArtifactService) artifactcontract.ArtifactService) (*gorm.DB, pagecontract.PageService, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	if derr := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; derr != nil {
		t.Fatalf("清理默认主题失败: %v", derr)
	}
	pageModel := pagemodel.NewPageModel(db)
	var artifacts artifactcontract.ArtifactService = artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	if wrap != nil {
		artifacts = wrap(artifacts)
	}
	pubRoutes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return db, pageservice.NewService(pageModel, artifacts, pubRoutes, projects, blocks, nil, nil, nil, nil), project.ID
}
