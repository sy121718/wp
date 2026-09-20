package feature

// structure_slot_publish_strict_test.go — 结构槽位「绑定了但拿不到」的发布硬失败，
// 以及「被容忍的降级」进 Manifest（审计 ARCH-05 的另一半）。
//
// 三条覆盖：
//   1. 槽位绑定的全局块不存在 → 发布失败；
//   2. 槽位绑定的结构模板属于别的工程（跨工程绑定）→ 发布失败；
//   3. 绑定了、拿得到、只是**空**（块存在但 root 为空）→ **不算**拿不到：
//      发布照常成功，但 Manifest.diagnostics 里必须留下归因；
//      且没有降级的产物 Manifest 里根本不出现 diagnostics 字段（字节兼容）。
//
// 预览侧另断言占位可归因（data-sky-* 属性）：预览不产出 Manifest，
// 归因只能落在 DOM 上。

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// emptyHeaderBlockDocument 空页眉块：块存在、文档合法、root 为空。
const emptyHeaderBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// TestPageStructureSlotBlockMissingFailsPublish 槽位绑定的全局块拿不到 → 发布失败。
func TestPageStructureSlotBlockMissingFailsPublish(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	missing := uuid.NewString()
	bindThemeStructure(t, db, projectID, map[string]any{"headerBlockId": missing})
	pageID, blockID, _ := createPageAndReadStructure(t, svc, projectID, "/slot-block-missing")
	if blockID != missing {
		t.Fatalf("settings.structure 应保留块绑定: %s", blockID)
	}

	_, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err == nil {
		t.Fatal("槽位绑定的全局块不存在时发布必须失败（旧行为：渲染一个空占位并发布成功）")
	}
	if !strings.Contains(err.Error(), "结构槽位 header") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("失败原因必须能定位到槽位与被引用的块，实际: %v", err)
	}
	if got := stagedArtifactIDOf(t, svc, projectID, pageID); got != "" {
		t.Fatalf("发布失败不该产生暂存产物，实际 %q", got)
	}

	// 预览：不失败，输出可归因的占位（哪个节点 / 哪个槽位 / 引用哪个块 / 为什么）。
	preview := previewOf(t, svc, projectID, pageID, "/slot-block-missing")
	for _, want := range []string{
		`data-sky-id="__layout_header"`,
		`data-sky-slot="header"`,
		`data-sky-ref="` + missing + `"`,
		`data-sky-degrade="ref_unavailable"`,
	} {
		if !containsBytes([]byte(preview), []byte(want)) {
			t.Fatalf("预览占位必须可归因：缺 %s\n%s", want, preview[:min(len(preview), 800)])
		}
	}
}

// TestPageStructureSlotTemplateCrossProjectFailsPublish 跨工程绑定结构模板 → 发布失败。
//
// 跨工程在数据层就是「拿不到」（模板按 (project_id, id) 查）；旧行为把它降级成
// 「回退到块绑定 / 什么都不渲染」，于是「配错了工程」的表现是页面悄悄少一截。
func TestPageStructureSlotTemplateCrossProjectFailsPublish(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	tplSvc := injectStructureTemplatePort(t, db, svc)

	// 另一个工程里的同类型结构模板。测试用超级用户连接，RLS 不生效 ——
	// 少了工程作用域照样查得到，这正好用来证明走的是**带作用域**那条查询。
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建另一工程失败: %v", err)
	}
	foreign, err := tplSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "别的工程的页眉",
		DraftDocument: []byte(structureTemplateDocument), ProjectID: other.ID,
	})
	if err != nil {
		t.Fatalf("创建跨工程模板失败: %v", err)
	}

	bindThemeStructure(t, db, projectID, map[string]any{"headerTemplateId": foreign.ID})
	pageID, _, templateID := createPageAndReadStructure(t, svc, projectID, "/slot-cross-project")
	if templateID != foreign.ID {
		t.Fatalf("settings.structure 应保留模板绑定: %s", templateID)
	}

	_, err = svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err == nil {
		t.Fatal("绑定了别的工程的结构模板时发布必须失败")
	}
	if !strings.Contains(err.Error(), "结构槽位 header") || !strings.Contains(err.Error(), foreign.ID) {
		t.Fatalf("失败原因必须能定位到槽位与那份模板，实际: %v", err)
	}

	// 预览不失败：模板拿不到且没有块可回退 → 归因到模板的占位。
	preview := previewOf(t, svc, projectID, pageID, "/slot-cross-project")
	if !containsBytes([]byte(preview), []byte(`data-sky-degrade="ref_template_unavailable"`)) {
		t.Fatalf("预览应输出归因到结构模板的占位: %s", preview[:min(len(preview), 800)])
	}
}

// TestPageStructureSlotEmptyBlockPublishesWithManifestDiagnostic 空块不属于「拿不到」：
// 发布成功，但归因必须进 Manifest；没有降级的产物则不出现 diagnostics 字段。
func TestPageStructureSlotEmptyBlockPublishesWithManifestDiagnostic(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	emptyID := createHeaderBlock(t, db, projectID, emptyHeaderBlockDocument)
	bindThemeStructure(t, db, projectID, map[string]any{"headerBlockId": emptyID})
	pageID, blockID, _ := createPageAndReadStructure(t, svc, projectID, "/slot-empty-block")
	if blockID != emptyID {
		t.Fatalf("settings.structure 应保留块绑定: %s", blockID)
	}

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("「块存在但内容为空」不是配置错误，发布不该失败: %v", err)
	}
	manifest := readBuiltManifest(t, built.StagedHash)
	want := `"diagnostics":[{"slot":"header","nodeId":"__layout_header","nodeType":"core.layoutSlot","kind":"block","refId":"` + emptyID + `","reason":"ref_empty"}]`
	if !strings.Contains(manifest, want) {
		t.Fatalf("空块降级必须进 Manifest 诊断（字段序确定）：缺\n%s\n实际\n%s", want, manifest)
	}
}

// TestPageStructureSlotPlainBlockHasNoDiagnosticsField 没有降级的产物 Manifest 里
// **根本不出现** diagnostics 字段：omitempty 是「加字段不改变历史产物字节」的前提，
// 少了这条断言，字段一加就会让全站产物 hash 变化。
func TestPageStructureSlotPlainBlockHasNoDiagnosticsField(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	headerID := createHeaderBlock(t, db, projectID, fallbackHeaderBlockDocument)
	bindThemeStructure(t, db, projectID, map[string]any{"headerBlockId": headerID})
	pageID, _, _ := createPageAndReadStructure(t, svc, projectID, "/slot-plain-block")

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	manifest := readBuiltManifest(t, built.StagedHash)
	if strings.Contains(manifest, "diagnostics") {
		t.Fatalf("无降级产物不该出现 diagnostics 字段: %s", manifest)
	}
	html := readBuiltHTML(t, built.StagedHash)
	if !containsBytes([]byte(html), []byte("FALLBACK-HEADER-BLOCK")) {
		t.Fatalf("正常块绑定必须进产物: %s", html[:min(len(html), 400)])
	}
}
