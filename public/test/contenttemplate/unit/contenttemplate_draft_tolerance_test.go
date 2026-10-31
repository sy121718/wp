package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

const halfBakedDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sld1","type":"core.slider","props":{},"children":[]}]}`

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

	if _, err = svc.ResolveTemplateByID(ctx, resp.ID); err == nil {
		t.Fatalf("发布取用半成品模板应被拒绝")
	} else if !strings.Contains(err.Error(), contenttemplateenums.ErrDataInvalid) {
		t.Fatalf("拒绝原因应是文档非法（%s），实际: %v", contenttemplateenums.ErrDataInvalid, err)
	}
}

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
