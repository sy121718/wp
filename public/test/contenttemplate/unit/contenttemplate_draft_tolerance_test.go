package unit

// contenttemplate_draft_tolerance_test.go — 草稿宽容 / 发布严格的校验口径（审计 EDT-013）。
//
// 问题原本是**按目标类型**分口径：手工 Page 用 ValidatePageTolerant（允许编辑中间态），
// ContentTemplate 用严格 ValidatePage —— 同一个编辑器里保存两种目标，容忍度却不一样。
// 修法是按**时机**分：草稿保存一律宽松，发布取用一律严格。
//
// 半成品用「轮播未拖入 slide」构造：它是唯一被 ValidatePageTolerant 显式放行的类别
//（core.ErrIncompleteNode），其他错误（非法 props / 未知组件）在两种口径下都必须拒绝。
//
// 注意本仓库的实现细节：ContentTemplate 保存时**同时**写入不可变版本快照，也就是
// 「保存即出版本」，因此严格校验的位置在**取用版本**（ResolvedTemplate）而不是生成快照。
// 效果与审计预期一致：半成品永远进不了产物，但也不会被挡在草稿阶段之外。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

// halfBakedDocument 半成品：轮播没有子节点。
const halfBakedDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sld1","type":"core.slider","props":{},"children":[]}]}`

// TestContentTemplateDraftIsTolerantButResolveIsStrict 同一份半成品：草稿收下，取用拒绝。
func TestContentTemplateDraftIsTolerantButResolveIsStrict(t *testing.T) {
	svc, _, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	resp, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "article",
		Name:          "半成品模板",
		DraftDocument: json.RawMessage(halfBakedDocument),
	})
	if err != nil {
		t.Fatalf("草稿保存应接受编辑中间态（轮播未拖入 slide）: %v", err)
	}
	if resp == nil || resp.ID == "" {
		t.Fatalf("草稿保存应返回模板 id")
	}

	// 取用（构建/发布口径）必须拒绝：半成品进产物会渲染出缺内容的组件。
	if _, err = svc.ResolveTemplateByID(ctx, resp.ID); err == nil {
		t.Fatalf("发布取用半成品模板应被拒绝")
	} else if !strings.Contains(err.Error(), contenttemplateenums.ErrDataInvalid) {
		t.Fatalf("拒绝原因应是文档非法（%s），实际: %v", contenttemplateenums.ErrDataInvalid, err)
	}
}

// TestContentTemplateRejectsBrokenDocumentEvenInDraft 宽容不等于放行：真正的错误照拒。
//
// 这条与上一条是配对的反向断言 —— 只测「半成品能存」会把「什么都存得下」也判定为通过。
func TestContentTemplateRejectsBrokenDocumentEvenInDraft(t *testing.T) {
	svc, _, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	cases := map[string]string{
		"JSON 断裂": `{"settings":`,
		"未知组件类型":  `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"x1","type":"core.notARealComponent","props":{}}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
				EntityType:    "article",
				Name:          "坏文档模板",
				DraftDocument: json.RawMessage(doc),
			}); err == nil {
				t.Fatalf("草稿口径也不应接受真正非法的文档（%s）", name)
			}
		})
	}
}
