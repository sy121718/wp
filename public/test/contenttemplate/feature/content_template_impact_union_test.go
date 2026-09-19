package feature

// content_template_impact_union_test.go — 模板引用反查的**并集**语义（装配层实现）。
//
// 这个文件覆盖的是并集存在的**唯一理由**：两类来源各自能捞到、对方捞不到的一类页面。
//   · 依赖表独有：草稿里已经把绑定解掉，但现行 active / staged 产物仍声明着
//     content_template:{id}（它此前构建过，产物还在）—— 文档扫描看的是草稿，扫不到它；
//   · 文档扫描独有：草稿里绑着该模板、但从未构建过（page_dependencies 里没有它的行）
//     —— 依赖表里没有它。
// 缺任一来源，删模板时都会漏掉一类：前者让线上产物引用着一套已删的模板，
// 后者让还没构建的页面在下次构建时静默少一套页眉。
//
// 走的是**生产同一条路径**：装配层的 routers.ContentTemplateImpactPort（不是文档扫描
// 或依赖表任一单独来源），因此「并集」这件事本身被钉在这里 —— 谁把依赖表那一半删掉，
// 本条用例立刻变红。
//
// presentation 侧传 nil：本用例只钉页面侧的并集；实例侧的引用由
// public/test/presentation 与 contenttemplate 既有用例覆盖（端口的 presents==nil 分支
// 有显式判空，不是隐式跳过）。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/internal/builder/core"
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

// unionStructureTemplateDocument 结构模板文档（页眉用，无字段绑定）。
const unionStructureTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"tpl-mark","type":"core.text","props":{"text":"UNION-STRUCTURE-HEADER"}}]}`

// unionStructureTemplatePort 结构模板端口的最小适配（与装配期注入的端口同语义：
// 只回答「一份文档」）。
//
// 必须注入：page 构建时经 pipeline.StructureSlotDependencies 判定「该槽位实际消费的是模板」
// 才会登记 content_template:{id}；端口缺失时一律回退块绑定、依赖表里不会有这一行，
// 于是本条用例测到的就不是并集而是「依赖表永远为空」。
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

// newUnionPageService 装配一套真实的 page service（依赖与生产同形，只把可选端口留空）。
func newUnionPageService(t *testing.T, db *gorm.DB, projects projectcontract.ProjectService) pagecontract.PageService {
	t.Helper()
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks, nil, nil, nil, nil)
}

// createUnionPage 建一个未构建的页面（kind=home，无内容绑定）。
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

// countUnionDepRows 统计某页面「活跃或暂存」产物上声明的某条模板依赖行数。
//
// 与生产反查（page 契约 FindPagesByDependency）用的是同一条谓词，但这里独立写一次：
// 它是本用例用来证明「这一页确实在依赖表里」的**客观事实**，不能反过来用被测代码自证。
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

// TestTemplateImpactUnionFindsBuiltAndUnbuiltPages 并集的两半各捞到一类页面，且不多捞。
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
	// 建站自带默认主题：主题设置里的结构会在页面保存时快照进文档，会搅乱
	// 「草稿里已解绑」这一前提 —— 先清掉（与 page 侧用例同一做法）。
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

	// ① 依赖表独有：先构建（依赖表落行），再把草稿里的绑定改掉（扫描再也看不到它）。
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

	// ② 文档扫描独有：草稿里绑着，但从未构建（依赖表里没有它的行）。
	unbuilt := createUnionPage(t, pageSvc, project.ID, "/union-unbuilt", boundDoc)
	if n := countUnionDepRows(t, db, unbuilt.ID, header.ID); n != 0 {
		t.Fatalf("从未构建的页面不该有依赖行（这一页只能靠文档扫描捞到），实际 %d 行", n)
	}

	// ③ 阴性对照：与该模板无关的页面，两条来源都不该命中它。
	plain := createUnionPage(t, pageSvc, project.ID, "/union-plain", plainDoc)

	// 走装配层的端口（与生产同一条路径）。
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
	// 扫描那一半要带出命中槽位（依赖表那一半没有槽位信息 —— 它的定位信息只有页面 id 与标题）。
	if len(slotsOf[unbuilt.ID]) == 0 {
		t.Fatalf("未构建页面来自文档扫描，应带出命中槽位，实际 %v", slotsOf[unbuilt.ID])
	}
}
