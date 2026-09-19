package feature

// structure_template_fallback_test.go — 结构模板的**回退防线**（本批最关键的存量保护）。
//
// 判据：settings.structure 同时有结构模板绑定与块绑定时——
//   - 模板可用 → 产物是模板内容，依赖登记 content_template:{id}；
//   - 模板不存在 / 文档非法 → **回退到块绑定**，产物里必须是块的内容；
//   - 回退时**不得**登记 content_template:{id}。
//
// 第三条同样重要：回退却登记依赖，会让依赖表里留下一条永命中不了的记录 ——
// 读者按它以为「改了那套模板本页会重建」，而本页根本没在用那套模板。
//
// 缺失这组断言的风险：存量站的页眉页脚在一次模板绑定改造后整片消失，
// 而构建照常成功、只在日志里留一行 Warn。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// fallbackHeaderBlockDocument 页眉块文档（回退路径的产物标记）。
const fallbackHeaderBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"FALLBACK-HEADER-BLOCK"}}]}`

// structureTemplateDocument 结构模板文档（模板优先路径的产物标记，无字段绑定）。
const structureTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"tpl-mark","props":{"text":"STRUCTURE-TEMPLATE-HEADER"}}]}`

// structureTemplatePortForTest 结构模板端口的测试适配器。
//
// 生产侧是 internal/routers/structure_template_port.go 的同形适配（未导出，测试包拿不到）；
// presentation 侧是它自己的服务方法。三处都只做同一件事：收窄成「一份文档」，
// 失败一律让调用方回退。
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

// injectStructureTemplatePort 注入结构模板端口（装配期做的事，测试里手动做一次），
// 返回同一 schema 上的真实 contenttemplate 服务。
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

// createHeaderBlock 建一个页眉类全局块，返回块 ID。
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

// bindThemeStructure 建主题并把结构绑定写进主题设置（页面保存时快照进文档）。
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

// createPageAndReadStructure 建页面并回读 settings.structure 的两个通道。
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

// readBuiltHTML 读取暂存产物 HTML。
func readBuiltHTML(t *testing.T, stagedHash string) string {
	t.Helper()
	html, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", stagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return string(html)
}

// countStagedDepRows 统计「当前暂存/活跃产物」声明的某条依赖。
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

// TestPageStructureTemplateMissingFallsBackToBlock 模板 ID 不存在时的回退防线。
func TestPageStructureTemplateMissingFallsBackToBlock(t *testing.T) {
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
	// 前提断言：快照必须**同时**带上两个通道，否则本用例测的不是回退。
	if templateID != missing || blockID != headerID {
		t.Fatalf("settings.structure 快照应同时保留模板与块绑定: block=%s template=%s", blockID, templateID)
	}

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	html := readBuiltHTML(t, built.StagedHash)
	if !containsBytes([]byte(html), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("模板不存在时必须回退到块绑定，产物里应出现块内容: %s", html[:min(len(html), 400)])
	}
	if containsBytes([]byte(html), []byte("STRUCTURE-TEMPLATE-HEADER")) {
		t.Fatalf("模板不存在时不该渲染出模板内容: %s", html[:min(len(html), 400)])
	}
	// 依赖侧：回退后登记的是块，且**不得**留下命中不了的 content_template 行。
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindBlock, "block:"+headerID); n != 1 {
		t.Fatalf("回退后应登记块依赖 block:%s，实际 %d 条", headerID, n)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindContentTemplate, "content_template:"+missing); n != 0 {
		t.Fatalf("未使用的模板不该被登记为依赖，实际 %d 条", n)
	}
}

// TestPageStructureTemplateInvalidDocumentFallsBackToBlock 模板存在但文档非法时的回退防线
// （含「模板可用时优先于块」的正向断言）。
func TestPageStructureTemplateInvalidDocumentFallsBackToBlock(t *testing.T) {
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

	// 1) 模板可用：模板优先，块不参与产物，依赖登记 content_template。
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

	// 2) 把模板的当前版本文档改成非法（绕过服务校验，模拟历史脏数据 / 手工改库）。
	if err := db.Exec(`UPDATE content_template_versions SET document = ?::jsonb WHERE template_id = ?`,
		`{"settings":{"layout":{"mode":"full"}},"root":"not-an-array"}`, tpl.ID).Error; err != nil {
		t.Fatalf("构造非法模板文档失败: %v", err)
	}

	built, err = svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("模板非法时构建不应失败（回退路径）: %v", err)
	}
	html = readBuiltHTML(t, built.StagedHash)
	if !containsBytes([]byte(html), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("模板文档非法时必须回退到块绑定: %s", html[:min(len(html), 400)])
	}
	if containsBytes([]byte(html), []byte("STRUCTURE-TEMPLATE-HEADER")) {
		t.Fatalf("模板文档非法时不该渲染出模板内容: %s", html[:min(len(html), 400)])
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindContentTemplate, "content_template:"+tpl.ID); n != 0 {
		t.Fatalf("回退后不该登记 content_template:%s，实际 %d 条", tpl.ID, n)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindBlock, "block:"+headerID); n != 1 {
		t.Fatalf("回退后应登记块依赖 block:%s，实际 %d 条", headerID, n)
	}
}
