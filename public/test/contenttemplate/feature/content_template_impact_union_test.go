package feature

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/pkg/builder/core"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/pipeline"
	"go_wp/internal/routers"

	"go_wp/public/test/support"
)

const unionStructureTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"tpl-mark","type":"core.text","props":{"text":"UNION-STRUCTURE-HEADER"}}]}`

type unionStructureTemplatePort struct {
	svc *contenttemplateservice.Service
}

func (p unionStructureTemplatePort) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	tpl, err := p.svc.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return tpl.Document, nil
}

func newUnionPageService(t *testing.T, db *gorm.DB, projects projectcontract.ProjectService) pagecontract.PageService {
	t.Helper()
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks, nil, nil, nil, nil)
}

func createUnionPage(t *testing.T, svc pagecontract.PageService, projectID, path, doc string) *pagedto.PageResp {
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

func countUnionDepRows(t *testing.T, db *gorm.DB, pageID, templateID string) int64 {
	t.Helper()
	dep := pipeline.ContentTemplateKey(templateID)
	var n int64
	if err := db.Raw(`
		SELECT count(*)
		FROM page_dependencies d
		JOIN pages p ON p.id = d.page_id
		WHERE d.page_id = ?
		  AND d.dependency_kind = ?
		  AND d.dependency_key = ?
		  AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)`,
		pageID, dep.Kind, dep.Key).Scan(&n).Error; err != nil {
		t.Fatalf("统计依赖行失败: %v", err)
	}
	return n
}

func TestTemplateImpactUnionFindsBuiltAndUnbuiltPages(t *testing.T) {
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "模板反查并集工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}

	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, core.NewEntitySourceRegistry())
	header, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "站点页眉",
		DraftDocument: []byte(unionStructureTemplateDocument), ProjectID: project.ID,
	})
	if err != nil {
		t.Fatalf("创建结构模板失败: %v", err)
	}

	pageSvc := newUnionPageService(t, db, projects)
	setter, ok := pageSvc.(interface {
		SetStructureTemplatePort(pipeline.StructureTemplatePort)
	})
	if !ok {
		t.Fatal("page service 未暴露结构模板端口注入点（SetStructureTemplatePort）")
	}
	setter.SetStructureTemplatePort(unionStructureTemplatePort{svc: tplSvc})

	boundDoc := fmt.Sprintf(
		`{"settings":{"layout":{"mode":"full"},"structure":{"headerTemplateId":%q}},"root":[{"id":"p-bound","type":"core.heading","props":{"text":"绑定模板"}}]}`,
		header.ID)
	unboundDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"p-bound","type":"core.heading","props":{"text":"草稿已解绑"}}]}`
	plainDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"p-plain","type":"core.heading","props":{"text":"与模板无关"}}]}`

	built := createUnionPage(t, pageSvc, project.ID, "/union-built", boundDoc)
	if _, err := pageSvc.Build(ctx, &pagedto.BuildReq{ID: built.ID}); err != nil {
		t.Fatalf("构建页面失败: %v", err)
	}
	if n := countUnionDepRows(t, db, built.ID, header.ID); n == 0 {
		t.Fatalf("构建后应在「活跃或暂存」产物上留下 content_template 依赖行（否则本用例测不到依赖表那一半）")
	}
	if _, err := pageSvc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: built.ID, ExpectedVersion: built.DraftVersion,
		DraftPath: "/union-built", DraftDocument: []byte(unboundDoc),
	}); err != nil {
		t.Fatalf("改草稿（解绑模板）失败: %v", err)
	}
	var draftText string
	if err := db.Raw(`SELECT draft_document::text FROM pages WHERE id = ?`, built.ID).Scan(&draftText).Error; err != nil {
		t.Fatalf("回读草稿失败: %v", err)
	}
	if strings.Contains(draftText, header.ID) {
		t.Fatalf("前提不成立：解绑后草稿里仍留着模板 id，这个用例就区分不出「谁捞到的」：%s", draftText)
	}
	if n := countUnionDepRows(t, db, built.ID, header.ID); n == 0 {
		t.Fatalf("解绑草稿不应清掉既有产物的依赖行（那正是依赖表那一半的依据）")
	}

	unbuilt := createUnionPage(t, pageSvc, project.ID, "/union-unbuilt", boundDoc)
	if n := countUnionDepRows(t, db, unbuilt.ID, header.ID); n != 0 {
		t.Fatalf("从未构建的页面不该有依赖行（这一页只能靠文档扫描捞到），实际 %d 行", n)
	}

	plain := createUnionPage(t, pageSvc, project.ID, "/union-plain", plainDoc)

	refs, unparsable, err := routers.ContentTemplateImpactPort(pageSvc, nil).
		ListTemplateReferences(ctx, project.ID, []string{header.ID})
	if err != nil {
		t.Fatalf("引用反查失败: %v", err)
	}
	if unparsable != 0 {
		t.Fatalf("本用例的文档都是合法 JSON，不该有解析失败的条数：%d", unparsable)
	}
	slotsOf := map[string][]string{}
	for i := range refs {
		if refs[i].TemplateID != header.ID || refs[i].Kind != contenttemplatedto.ReferenceKindPage {
			continue
		}
		slotsOf[refs[i].PageID] = refs[i].Slots
	}
	if _, ok := slotsOf[built.ID]; !ok {
		t.Fatalf("草稿已解绑、但现行产物仍声明依赖的页面必须被反查出来（依赖表那一半）：refs=%+v", refs)
	}
	if _, ok := slotsOf[unbuilt.ID]; !ok {
		t.Fatalf("草稿绑着模板、从未构建的页面必须被反查出来（文档扫描那一半）：refs=%+v", refs)
	}
	if _, ok := slotsOf[plain.ID]; ok {
		t.Fatalf("与该模板无关的页面不该出现在引用里：refs=%+v", refs)
	}

	if len(slotsOf[unbuilt.ID]) == 0 {
		t.Fatalf("未构建页面来自文档扫描，应带出命中槽位，实际 %v", slotsOf[unbuilt.ID])
	}
}
