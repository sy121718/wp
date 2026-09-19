package feature

// product_override_test.go — 实例级文档覆盖链路（迁移 281，docs/04-C-instance-override.md）：
// 自定义保存→产物生效；实体数据更新重建不丢自定义；显式切模板清覆盖。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
)

// replaceFirstString 递归替换文档里第一个足够长的字符串值（与 AST 具体形状无关的
// replaceFirstString 递归替换文档里第一个足够长的叶子字符串值（跳过 type ——
// 那是组件身份，改了会编译失败）。与 AST 具体形状无关的「内容确实变了」标记：
// 产物 HTML 出现标记 = 发布用的是覆盖文档而不是模板文档。
func replaceFirstString(v any, marker string) bool {
	// preferText 优先命中的文案类键；type 键是组件身份，绝不改。
	prefer := map[string]bool{"text": true, "content": true, "title": true, "value": true}
	var walk func(v any, top bool) bool
	walk = func(v any, top bool) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if top && k == "type" {
					continue
				}
				if _, isKey := prefer[k]; !isKey && k == "type" {
					continue
				}
				if s, ok := val.(string); ok && len(s) > 3 {
					x[k] = marker
					return true
				}
			}
			for _, val := range x {
				if walk(val, false) {
					return true
				}
			}
		case []any:
			for i, val := range x {
				if s, ok := val.(string); ok && len(s) > 3 && !top {
					x[i] = marker
					return true
				}
			}
			for _, val := range x {
				if walk(val, false) {
					return true
				}
			}
		}
		return false
	}
	// 顶层的 "type" 是文档根身份不可改；先在整棵树里找文案键，找不到再退回
	// 「第一个非 type 叶子字符串」。
	root := v
	if m, ok := root.(map[string]any); ok {
		if tp, ok := m["type"].(string); ok {
			_ = tp
		}
	}
	return walk(v, true)
}

func TestProductOverrideDocumentLifecycle(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	productID := f.createProduct(t, "覆盖测试商品", "/override-smoke", "覆盖测试描述", 10, 20)
	inst := f.publish(t, productID, "/override-smoke")

	// 底稿 = 当前快照文档（发布后 GetByEntity 返回的 Document）。
	cur, err := f.pres.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: "product", EntityID: productID, ProjectID: f.projectID,
	})
	if err != nil {
		t.Fatalf("读取实例失败: %v", err)
	}
	if len(cur.Document) == 0 {
		t.Fatalf("发布实例未返回快照文档，无法构造覆盖底稿")
	}
	var doc any
	if err := json.Unmarshal(cur.Document, &doc); err != nil {
		t.Fatalf("快照文档反序列化失败: %v", err)
	}
	const marker = "OVERRIDE-MARKER-281"
	if !replaceFirstString(doc, marker) {
		t.Fatalf("文档中未找到可替换的字符串值")
	}
	overridden, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("覆盖文档序列化失败: %v", err)
	}

	// 1) 保存覆盖：重编译发布后产物包含标记（发布用的是覆盖文档）。
	if _, err := f.pres.SaveOverrideDocument(ctx, &presentationdto.SaveOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, Document: overridden,
	}); err != nil {
		t.Fatalf("保存覆盖文档失败: %v", err)
	}
	if html := activeHTML(t, "/override-smoke"); !strings.Contains(html, marker) {
		t.Fatalf("保存覆盖后产物未包含标记，发布仍按模板文档编译")
	}

	// 2) 实体数据更新 → Rebuild：新数据进产物（binding 照常解析），标记仍在（未冲回模板）。
	newName := "覆盖测试商品二"
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: productID, ProjectID: f.projectID, Name: &newName,
	}); err != nil {
		t.Fatalf("更新商品失败: %v", err)
	}
	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID, ProjectID: f.projectID}); err != nil {
		t.Fatalf("数据更新重建失败: %v", err)
	}
	html := activeHTML(t, "/override-smoke")
	if !strings.Contains(html, marker) {
		t.Fatalf("实体数据更新重建后自定义被模板冲掉")
	}
	if !strings.Contains(html, newName) {
		t.Fatalf("重建后产物未包含最新商品数据（binding 未解析）")
	}

	// 3) 放弃自定义：同模板走 ClearOverride（换模板语义由 Rebuild 的 TemplateID 承担，
	// 同模板切换不触发 persistBuild 的模板切换分支，是预期行为）。
	if _, err := f.pres.ClearOverride(ctx, &presentationdto.ClearOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, TemplateID: cur.TemplateID,
	}); err != nil {
		t.Fatalf("清除覆盖重建失败: %v", err)
	}
	var cnt int64
	if err := f.db.Table("presentation_instances").
		Where("id = ? AND override_document IS NOT NULL", inst.ID).
		Count(&cnt).Error; err != nil {
		t.Fatalf("查询覆盖列失败: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("切模板后 override_document 未清除")
	}
	if html := activeHTML(t, "/override-smoke"); strings.Contains(html, marker) {
		t.Fatalf("切模板后产物仍包含自定义标记")
	}
}
