package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"go_wp/internal/builder/core"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

const fallbackHeaderBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"FALLBACK-HEADER-BLOCK"}}]}`

const structureTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"tpl-mark","props":{"text":"STRUCTURE-TEMPLATE-HEADER"}}]}`

type structureTemplatePortForTest struct {
	svc *contenttemplateservice.Service
}

func (p structureTemplatePortForTest) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	tpl, err := p.svc.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return tpl.Document, nil
}

func injectStructureTemplatePort(t *testing.T, db *gorm.DB, svc pagecontract.PageService) *contenttemplateservice.Service {
	t.Helper()
	setter, ok := svc.(interface {
		SetStructureTemplatePort(pipeline.StructureTemplatePort)
	})
	if !ok {
		t.Fatal("page service 未暴露结构模板端口注入点（SetStructureTemplatePort）")
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, core.NewEntitySourceRegistry())
	setter.SetStructureTemplatePort(structureTemplatePortForTest{svc: tplSvc})
	return tplSvc
}

func createHeaderBlock(t *testing.T, db *gorm.DB, projectID, doc string) (blockID string) {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	block, err := blocks.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页眉", Kind: "header", Document: json.RawMessage(doc),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	return block.ID
}

func bindThemeStructure(t *testing.T, db *gorm.DB, projectID string, settings map[string]any) {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("序列化主题设置失败: %v", err)
	}
	if _, err := projects.CreateTheme(context.Background(), &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "默认主题", Settings: raw,
	}); err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
}

func createPageAndReadStructure(t *testing.T, svc pagecontract.PageService, projectID, path string) (pageID, headerBlockID, headerTemplateID string) {
	t.Helper()
	ctx := context.Background()
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	parsed, err := pipeline.ParseStructureBindings(detail.DraftDocument)
	if err != nil {
		t.Fatalf("解析 settings.structure 失败: %v", err)
	}
	return page.ID, parsed.HeaderBlockID, parsed.HeaderTemplateID
}

func readBuiltHTML(t *testing.T, stagedHash string) string {
	t.Helper()
	html, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", stagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return string(html)
}

func readBuiltManifest(t *testing.T, stagedHash string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", stagedHash, "manifest.json"))
	if err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	return string(raw)
}

func countStagedDepRows(t *testing.T, db *gorm.DB, pageID, kind, key string) int64 {
	t.Helper()
	q := `SELECT COUNT(*) FROM page_dependencies d JOIN pages p ON p.id = d.page_id
		WHERE d.page_id = ? AND d.dependency_kind = ? AND d.dependency_key = ?
		AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)`
	var n int64
	if err := db.Raw(q, pageID, kind, key).Scan(&n).Error; err != nil {
		t.Fatalf("统计依赖记录失败: %v", err)
	}
	return n
}

func stagedArtifactIDOf(t *testing.T, svc pagecontract.PageService, projectID, pageID string) string {
	t.Helper()
	detail, err := svc.Detail(context.Background(), &pagedto.DetailReq{ProjectID: projectID, ID: pageID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if detail.StagedArtifactID == nil {
		return ""
	}
	return *detail.StagedArtifactID
}

func previewOf(t *testing.T, svc pagecontract.PageService, projectID, pageID, currentPath string) string {
	t.Helper()
	ctx := context.Background()
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: pageID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	html, err := svc.CompilePreview(ctx, detail.DraftDocument, projectID, currentPath, "", false)
	if err != nil {
		t.Fatalf("预览不该失败（编辑期容忍配置缺失）: %v", err)
	}
	return string(html)
}

func TestPageStructureTemplateMissingFailsPublish(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	injectStructureTemplatePort(t, db, svc)

	headerID := createHeaderBlock(t, db, projectID, fallbackHeaderBlockDocument)
	missing := uuid.NewString()
	bindThemeStructure(t, db, projectID, map[string]any{
		"headerBlockId":    headerID,
		"headerTemplateId": missing,
	})
	pageID, blockID, templateID := createPageAndReadStructure(t, svc, projectID, "/structure-missing")

	if templateID != missing || blockID != headerID {
		t.Fatalf("settings.structure 快照应同时保留模板与块绑定: block=%s template=%s", blockID, templateID)
	}

	_, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err == nil {
		t.Fatal("绑定的结构模板不存在时发布必须失败（旧行为：静默回退到块绑定并发布成功）")
	}
	if !strings.Contains(err.Error(), "结构槽位 header") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("失败原因必须能定位到槽位与那份模板，实际: %v", err)
	}

	if got := stagedArtifactIDOf(t, svc, projectID, pageID); got != "" {
		t.Fatalf("发布失败不该产生暂存产物，实际 %q", got)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindBlock, "block:"+headerID); n != 0 {
		t.Fatalf("发布失败不该登记任何依赖，实际块依赖 %d 条", n)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindContentTemplate, "content_template:"+missing); n != 0 {
		t.Fatalf("发布失败不该登记任何依赖，实际模板依赖 %d 条", n)
	}

	preview := previewOf(t, svc, projectID, pageID, "/structure-missing")
	if !containsBytes([]byte(preview), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("预览应回退到块绑定，产物里必须出现块内容: %s", preview[:min(len(preview), 400)])
	}
	if containsBytes([]byte(preview), []byte("STRUCTURE-TEMPLATE-HEADER")) {
		t.Fatalf("模板拿不到时不该渲染出模板内容: %s", preview[:min(len(preview), 400)])
	}
}

func TestPageStructureTemplateMissingPreviewDegradesWithAttribution(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	injectStructureTemplatePort(t, db, svc)

	missing := uuid.NewString()
	bindThemeStructure(t, db, projectID, map[string]any{"headerTemplateId": missing})
	pageID, blockID, templateID := createPageAndReadStructure(t, svc, projectID, "/structure-missing-attr")
	if templateID != missing || blockID != "" {
		t.Fatalf("本用例要求只绑定模板、不绑定块: block=%q template=%q", blockID, templateID)
	}

	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID}); err == nil {
		t.Fatal("绑定的结构模板不存在时发布必须失败")
	}

	preview := previewOf(t, svc, projectID, pageID, "/structure-missing-attr")
	for _, want := range []string{
		`data-sky-id="__layout_header"`,
		`data-sky-slot="header"`,
		`data-sky-ref="__structure_template__` + missing + `"`,
		`data-sky-degrade="ref_template_unavailable"`,
	} {
		if !containsBytes([]byte(preview), []byte(want)) {
			t.Fatalf("预览占位必须可归因：缺 %s\n%s", want, preview[:min(len(preview), 800)])
		}
	}
}

func TestPageStructureTemplateInvalidDocumentFailsPublish(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	tplSvc := injectStructureTemplatePort(t, db, svc)

	tpl, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "页眉模板",
		DraftDocument: []byte(structureTemplateDocument), ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("创建结构模板失败: %v", err)
	}
	headerID := createHeaderBlock(t, db, projectID, fallbackHeaderBlockDocument)
	bindThemeStructure(t, db, projectID, map[string]any{
		"headerBlockId":    headerID,
		"headerTemplateId": tpl.ID,
	})
	pageID, blockID, templateID := createPageAndReadStructure(t, svc, projectID, "/structure-invalid")
	if templateID != tpl.ID || blockID != headerID {
		t.Fatalf("settings.structure 快照应同时保留模板与块绑定: block=%s template=%s", blockID, templateID)
	}

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	html := readBuiltHTML(t, built.StagedHash)
	if !containsBytes([]byte(html), []byte("STRUCTURE-TEMPLATE-HEADER")) {
		t.Fatalf("模板可用时应渲染模板内容: %s", html[:min(len(html), 400)])
	}
	if containsBytes([]byte(html), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("模板优先时不该再渲染块绑定内容: %s", html[:min(len(html), 400)])
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindContentTemplate, "content_template:"+tpl.ID); n != 1 {
		t.Fatalf("模板被消费时应登记 content_template:%s，实际 %d 条", tpl.ID, n)
	}
	stagedBefore := stagedArtifactIDOf(t, svc, projectID, pageID)

	if err := db.Exec(`UPDATE content_template_versions SET document = ?::jsonb WHERE template_id = ?`,
		`{"settings":{"layout":{"mode":"full"}},"root":"not-an-array"}`, tpl.ID).Error; err != nil {
		t.Fatalf("构造非法模板文档失败: %v", err)
	}

	_, err = svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err == nil {
		t.Fatal("模板文档非法时发布必须失败（旧行为：静默回退到块绑定并发布成功）")
	}
	if !strings.Contains(err.Error(), "结构槽位 header") || !strings.Contains(err.Error(), tpl.ID) {
		t.Fatalf("失败原因必须能定位到槽位与那套模板，实际: %v", err)
	}

	if got := stagedArtifactIDOf(t, svc, projectID, pageID); got != stagedBefore || got == "" {
		t.Fatalf("发布失败不得改动暂存指针：失败前 %q，失败后 %q", stagedBefore, got)
	}
	if again := readBuiltHTML(t, built.StagedHash); again != html {
		t.Fatal("发布失败不得改动已有产物字节")
	}

	preview := previewOf(t, svc, projectID, pageID, "/structure-invalid")
	if !containsBytes([]byte(preview), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("预览应回退到块绑定: %s", preview[:min(len(preview), 400)])
	}
}
