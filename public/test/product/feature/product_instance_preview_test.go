package feature

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectdto "go_wp/internal/module/project/dto"
	json "go_wp/pkg/jsonx"
)

func TestProductInstancePreviewUsesRealCurrentProductAndRemainsReadOnly(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	document := json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"title","type":"core.heading","props":{"text":"真实商品实例","tag":"h1"}}]}`)
	template, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{EntityType: "product", Name: "实例预览底稿", ProjectID: f.projectID, DraftDocument: document})
	if err != nil {
		t.Fatal(err)
	}
	productID := f.createProduct(t, "实例预览杯", "instance-preview-cup", "当前商品", 88, 99)
	instance := f.publishWithTemplate(t, productID, "/instance-preview-cup", template.ID)
	beforeInstances := countRows(t, f.db, "presentation_instances", "")
	beforeArtifacts := countRows(t, f.db, "presentation_artifacts", "")
	beforeSnapshots := countRows(t, f.db, "document_snapshots", "")
	preview, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{InstanceID: instance.ID, ProjectID: f.projectID})
	if err != nil || !strings.Contains(preview.HTML, "真实商品实例") {
		t.Fatalf("实例初始预览不应要求作者传模板或实体: %v", err)
	}
	draft := json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"buy","type":"core.addToCart","props":{"label":"当前加购","showQuantity":true}}]}`)
	preview, err = f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{InstanceID: instance.ID, ProjectID: f.projectID, EntityType: "article", EntityID: "spoofed", TemplateID: "spoofed", DraftDocument: draft})
	if err != nil {
		t.Fatal(err)
	}
	if preview.EntityType != "product" || preview.EntityID != productID || !strings.Contains(preview.HTML, `name="variantId"`) || !strings.Contains(preview.HTML, "当前加购") || !strings.Contains(preview.HTML, `name="site-preview-mode" content="readonly"`) {
		t.Fatal("实例草稿应只消费真实当前商品，保留只读预览")
	}
	if countRows(t, f.db, "presentation_instances", "") != beforeInstances || countRows(t, f.db, "presentation_artifacts", "") != beforeArtifacts || countRows(t, f.db, "document_snapshots", "") != beforeSnapshots {
		t.Fatal("实例预览不能写入实例、产物或快照")
	}
	otherProject, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "预览隔离工程"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{InstanceID: instance.ID, ProjectID: otherProject.ID}); err == nil {
		t.Fatal("实例预览不得跨工程读取")
	}
}
