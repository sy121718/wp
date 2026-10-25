package feature

// template_body_json_test.go — 可视化正文的保存 / 渲染链路（T5）。
//
// 三条判据：
//
//  1. **编译成功才落库**：坏树保存失败，库里那一行一个字都不动（不做半成品）；
//  2. **渲染读产物**：body_json 非空时，发出去的是编译产物而不是 body_html；
//  3. **落库字节确定性**：同一棵树保存两次，body_compiled_html 字节相同。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// simpleTree 一份最小的合法块树（body → section → column → text）。
func simpleTree(text string) map[string]any {
	return map[string]any{
		"schema": 1,
		"root": map[string]any{
			"key": "root", "type": "body",
			"children": []any{
				map[string]any{"key": "s1", "type": "section", "attrs": map[string]any{"padding": "24px"},
					"children": []any{
						map[string]any{"key": "c1", "type": "column", "children": []any{
							map[string]any{"key": "t1", "type": "text", "text": text},
						}},
					}},
			},
		},
	}
}

func treeJSON(t *testing.T, tree map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestSaveTemplateCompilesBodyJSON 保存块树 → 编译产物落库。
func TestSaveTemplateCompilesBodyJSON(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	item, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "visual_tpl", Subject: "主题", Name: "可视化模板",
		BodyJSON: treeJSON(t, simpleTree("你好 {{.name}}")),
	})
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if item.BodyCompiledHTML == "" || item.BodyCompiledText == "" {
		t.Fatal("保存成功却没回带编译产物")
	}
	if !strings.Contains(item.BodyCompiledHTML, "<table role=\"presentation\"") {
		t.Fatalf("编译产物应是表格布局，实际：%s", item.BodyCompiledHTML)
	}
	if !strings.Contains(item.BodyCompiledText, "你好 {{.name}}") {
		t.Fatalf("纯文本应保留占位符，实际：%s", item.BodyCompiledText)
	}
	if len(item.BodyJSON) == 0 {
		t.Fatal("回执应带 body_json（编辑器要靠它重建树）")
	}

	// 库里三列都在。
	row, err := mailmodel.NewMailModel(f.db).GetTemplate(ctx, "visual_tpl", "")
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if len(row.BodyJSON) == 0 || row.BodyCompiledHTML == "" || row.BodyCompiledText == "" {
		t.Fatalf("三列未落库：json=%d html=%d text=%d",
			len(row.BodyJSON), len(row.BodyCompiledHTML), len(row.BodyCompiledText))
	}
}

// TestSaveTemplateBadTreeWritesNothing 坏树保存失败，库里一个字都不动。
func TestSaveTemplateBadTreeWritesNothing(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	good, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "keep_tpl", Subject: "主题", BodyJSON: treeJSON(t, simpleTree("原文")),
	})
	if err != nil {
		t.Fatalf("建模板失败: %v", err)
	}
	before, err := mailmodel.NewMailModel(f.db).GetTemplate(ctx, "keep_tpl", "")
	if err != nil {
		t.Fatal(err)
	}

	// 未知块类型 → 编译失败。
	bad := simpleTree("新文本")
	bad["root"].(map[string]any)["children"] = []any{
		map[string]any{"key": "weird", "type": "teleport"},
	}
	_, serr := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "keep_tpl", Subject: "主题", BodyJSON: treeJSON(t, bad),
	})
	if serr == nil {
		t.Fatal("未知块类型应保存失败")
	}
	if !strings.Contains(serr.Error(), "weird") {
		t.Fatalf("错误应带节点定位（weird），实际 %v", serr)
	}

	after, err := mailmodel.NewMailModel(f.db).GetTemplate(ctx, "keep_tpl", "")
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _ := json.Marshal(after.BodyJSON)
	beforeJSON, _ := json.Marshal(before.BodyJSON)
	if after.BodyCompiledHTML != before.BodyCompiledHTML || string(afterJSON) != string(beforeJSON) {
		t.Fatal("编译失败却改了库里的行 —— 半成品（新树 + 旧产物）比存不下更糟")
	}
	if good.ID == 0 {
		t.Fatal("第一次保存没拿到 id")
	}
}

// TestSaveTemplateIsByteDeterministic 同一棵树保存两次，落库字节相同。
func TestSaveTemplateIsByteDeterministic(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tree := simpleTree("确定性 {{.name}}")
	first, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "deterministic_tpl", Subject: "主题", BodyJSON: treeJSON(t, tree),
	})
	if err != nil {
		t.Fatalf("第一次保存失败: %v", err)
	}
	second, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "deterministic_tpl", Subject: "主题", BodyJSON: treeJSON(t, tree),
	})
	if err != nil {
		t.Fatalf("第二次保存失败: %v", err)
	}
	if first.BodyCompiledHTML != second.BodyCompiledHTML {
		t.Fatal("同一棵树两次编译的 HTML 字节不同 —— 确定性被破了")
	}
	if first.BodyCompiledText != second.BodyCompiledText {
		t.Fatal("同一棵树两次编译的纯文本不同")
	}
}
