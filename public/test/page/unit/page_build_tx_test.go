package unit

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

type bogusArtifactRow struct {
	artifactcontract.ArtifactService
}

func (bogusArtifactRow) EnsureRecord(context.Context, *artifactcontract.RecordReq) (*artifactcontract.ArtifactResp, error) {
	return &artifactcontract.ArtifactResp{ID: uuid.NewString()}, nil
}

func TestBuildFailsWhenDependencyWriteFails(t *testing.T) {
	db, svc, projectID := newPageServiceWithArtifact(t, func(real artifactcontract.ArtifactService) artifactcontract.ArtifactService {
		return bogusArtifactRow{ArtifactService: real}
	})
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/deps-fail", headingDocument)

	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err == nil {
		t.Fatal("依赖记录写失败应让构建返回错误（修复前是只记日志、构建成功）")
	}

	var staged int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_stagings WHERE page_id = ?`, page.ID).Scan(&staged).Error; err != nil {
		t.Fatalf("统计暂存指针失败: %v", err)
	}
	if staged != 0 {
		t.Fatalf("构建失败时暂存指针不应落库，实际 %d 行", staged)
	}
}

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
