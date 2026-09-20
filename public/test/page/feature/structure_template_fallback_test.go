package feature

// structure_template_fallback_test.go — 结构模板的**发布 / 预览分界**（审计 ARCH-05）。
//
// 判据（本次整改唯一的分界）：
//   - 显式绑定了结构模板却拿不到（不存在 / 跨工程 / 文档非法）→ **发布失败**，
//     且失败整体失败：不产生暂存产物、不推进指针（线上保持原样）；
//   - 同一份文档的**预览不失败**：有块绑定则回退到块（存量保护），没有块绑定则留下
//     带归因的槽位占位（哪个槽位 / 哪套模板 / 为什么没展开）；
//   - 回退时**不得**登记 content_template:{id}（依赖表是「实际消费了什么」的投影）。
//
// 为什么第一条必须单独钉住：旧行为在两条路径上都静默回退，于是「删除已绑定模板」
// 会被发布成一份缺页眉的页面，而构建接口返回成功 —— 这正是 ARCH-05 的问题本身。

import (
	"context"
	"encoding/json"
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

// fallbackHeaderBlockDocument 页眉块文档（回退路径的产物标记）。
const fallbackHeaderBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"FALLBACK-HEADER-BLOCK"}}]}`

// structureTemplateDocument 结构模板文档（模板优先路径的产物标记，无字段绑定）。
const structureTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"tpl-mark","props":{"text":"STRUCTURE-TEMPLATE-HEADER"}}]}`

// structureTemplatePortForTest 结构模板端口的测试适配器。
//
// 生产侧是 internal/routers/structure_template_port.go 的同形适配（未导出，测试包拿不到）；
// presentation 侧是它自己的服务方法。三处都只做同一件事：收窄成「一份文档」，
// 失败一律交给调用方按编译模式处置。
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

// readBuiltManifest 读取暂存产物的 manifest.json。
func readBuiltManifest(t *testing.T, stagedHash string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", stagedHash, "manifest.json"))
	if err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	return string(raw)
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

// stagedArtifactIDOf 读页面当前暂存的产物 ID（空串 = 没有暂存）。
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

// previewOf 用页面当前草稿文档跑一次预览编译。
func previewOf(t *testing.T, svc pagecontract.PageService, projectID, pageID, currentPath string) string {
	t.Helper()
	ctx := context.Background()
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: pageID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	html, err := svc.CompilePreview(ctx, detail.DraftDocument, projectID, currentPath, "")
	if err != nil {
		t.Fatalf("预览不该失败（编辑期容忍配置缺失）: %v", err)
	}
	return string(html)
}

// TestPageStructureTemplateMissingFailsPublish 模板 ID 不存在：发布必须失败，不产生产物。
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
	// 前提断言：快照必须**同时**带上两个通道 —— 「绑定了模板但拿不到」正是在这种
	// 「模板优先 + 有块可回退」的存量配置下被静默降级的。
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
	// 失败即整体失败：不产生暂存产物、不写依赖行 —— 线上保持原样。
	if got := stagedArtifactIDOf(t, svc, projectID, pageID); got != "" {
		t.Fatalf("发布失败不该产生暂存产物，实际 %q", got)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindBlock, "block:"+headerID); n != 0 {
		t.Fatalf("发布失败不该登记任何依赖，实际块依赖 %d 条", n)
	}
	if n := countStagedDepRows(t, db, pageID, pipeline.DepKindContentTemplate, "content_template:"+missing); n != 0 {
		t.Fatalf("发布失败不该登记任何依赖，实际模板依赖 %d 条", n)
	}

	// 预览：不失败，且按「有块可回退」的存量保护回退到块绑定。
	preview := previewOf(t, svc, projectID, pageID, "/structure-missing")
	if !containsBytes([]byte(preview), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("预览应回退到块绑定，产物里必须出现块内容: %s", preview[:min(len(preview), 400)])
	}
	if containsBytes([]byte(preview), []byte("STRUCTURE-TEMPLATE-HEADER")) {
		t.Fatalf("模板拿不到时不该渲染出模板内容: %s", preview[:min(len(preview), 400)])
	}
}

// TestPageStructureTemplateMissingPreviewDegradesWithAttribution 无块可回退时，
// 预览留下**可归因**的槽位占位（槽位 / 节点 / 模板 / 原因），发布仍然失败。
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

// TestPageStructureTemplateInvalidDocumentFailsPublish 模板存在但文档非法：
// 发布失败且**已暂存的旧产物保持**；预览按存量保护回退到块绑定。
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
	stagedBefore := stagedArtifactIDOf(t, svc, projectID, pageID)

	// 2) 把模板的当前版本文档改成非法（绕过服务校验，模拟历史脏数据 / 手工改库）。
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
	// 关键验收：失败不破坏已有产物 —— 暂存指针仍指向那次成功构建的产物，字节不变。
	if got := stagedArtifactIDOf(t, svc, projectID, pageID); got != stagedBefore || got == "" {
		t.Fatalf("发布失败不得改动暂存指针：失败前 %q，失败后 %q", stagedBefore, got)
	}
	if again := readBuiltHTML(t, built.StagedHash); again != html {
		t.Fatal("发布失败不得改动已有产物字节")
	}

	// 3) 预览：不失败，按存量保护回退到块绑定。
	preview := previewOf(t, svc, projectID, pageID, "/structure-invalid")
	if !containsBytes([]byte(preview), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("预览应回退到块绑定: %s", preview[:min(len(preview), 400)])
	}
}
