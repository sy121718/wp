package unit

// structure_template_trigger_test.go — 结构模板换代 → 引用实例失效的端到端链路。
//
// 三半，缺任何一半这条链都是静默失效（改了模板，站点上仍旧页眉，且不报错）：
//   1) 登记：实例产物声明 content_template:{结构模板ID}（构建期 buildArtifact 的结构槽位依赖）；
//   2) 接线：contenttemplate 的写操作经 pipeline.Fanout 反查依赖表（装配期 SetInvalidator）；
//   3) 结果：模板产生新版本后，引用它的实例 stale=true（等待重建）。
//
// 第 1 半同时是「结构模板真的渲染进了实例产物」的断言 —— 否则依赖登记得再对也没意义。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	presentationdto "go_wp/internal/module/presentation/dto"

	"go_wp/internal/pipeline"
)

// structureHeaderTemplateDocument 结构模板（页眉）文档：无字段绑定，纯结构。
const structureHeaderTemplateDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"结构模板页眉"}}]}`

// structureHeaderTemplateDocumentV2 换代后的同款文档（文字变化 = 产物字节变化）。
const structureHeaderTemplateDocumentV2 = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"结构模板页眉 v2"}}]}`

// TestStructureTemplateVersionChangeStalesInstance 结构模板产生新版本 → 引用它的实例 stale。
func TestStructureTemplateVersionChangeStalesInstance(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 1) 结构模板（页眉）+ 绑定它的内容模板。
	headerTpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "站点页眉模板",
		DraftDocument: []byte(structureHeaderTemplateDocument), ProjectID: f.projectID,
	})
	if err != nil {
		t.Fatalf("创建结构模板失败: %v", err)
	}
	boundDoc := `{"settings":{"layout":{"mode":"full"},"structure":{"headerTemplateId":"` + headerTpl.ID + `"}},` +
		`"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "文章模板（带结构模板页眉）",
		DraftDocument: json.RawMessage(boundDoc), ProjectID: f.projectID,
	}); err != nil {
		t.Fatalf("创建内容模板失败: %v", err)
	}

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "structure-header-instance",
		Data: map[string]any{"title": "结构模板实例"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/structure-header-instance",
	})
	if err != nil {
		t.Fatalf("创建实例失败: %v", err)
	}

	// 2) 登记：实例产物里出现结构模板内容，且依赖表里有 content_template:{结构模板ID}。
	html := activeHTML(t, "/structure-header-instance")
	if !strings.Contains(html, "结构模板页眉") {
		t.Fatalf("实例产物应内联结构模板（页眉）内容，实际片段: %s", html[:min(len(html), 400)])
	}
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM presentation_dependencies WHERE presentation_id = ?
		AND dependency_kind = ? AND dependency_key = ?`,
		inst.ID, pipeline.DepKindContentTemplate, "content_template:"+headerTpl.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计结构模板依赖失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("实例应声明恰好 1 条 content_template:%s 依赖，实际 %d 条", headerTpl.ID, n)
	}

	// 3) 触发：接线（装配期做的事）——扇出注册 presentation 来源 + contenttemplate 注入失效端口。
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePresentation, f.pres)
	// 夹具字段是契约类型，SetInvalidator 是装配期用的具体实现方法（同装配层用类型断言取）。
	tplImpl, ok := f.templates.(*contenttemplateservice.Service)
	if !ok {
		t.Fatalf("夹具的模板服务应为具体实现，实际 %T", f.templates)
	}
	tplImpl.SetInvalidator(fanout)

	if err := f.db.Exec(`UPDATE presentation_instances SET stale = false WHERE id = ?`, inst.ID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	if _, err := f.templates.Update(ctx, &contenttemplatedto.UpdateReq{
		ID: headerTpl.ID, DraftDocument: json.RawMessage(structureHeaderTemplateDocumentV2),
	}); err != nil {
		t.Fatalf("模板产生新版本失败: %v", err)
	}

	var stale bool
	if err := f.db.Raw(`SELECT stale FROM presentation_instances WHERE id = ?`, inst.ID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取实例 stale 失败: %v", err)
	}
	if !stale {
		t.Fatal("引用了该结构模板的实例应被标记待重建（模板换代）")
	}
}
