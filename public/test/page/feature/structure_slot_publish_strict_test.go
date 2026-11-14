package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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

const emptyHeaderBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

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

func TestPageStructureSlotTemplateCrossProjectFailsPublish(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	tplSvc := injectStructureTemplatePort(t, db, svc)

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
	// 跨工程模板在写入事务内按工程作用域复核：本工程看不到它，页面创建必须拒绝，
	// 而不是先落一份跨工程引用、等到发布才失败。
	_, createErr := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/slot-cross-project", DraftDocument: json.RawMessage(pageDocument),
	})
	if createErr == nil {
		t.Fatal("绑定别的工程的结构模板时页面创建必须失败")
	}
}

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
