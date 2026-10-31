package unit

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

const headerDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"站点页眉"}}]}`

const fieldBoundDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"p1","type":"core.product","props":{"titleField":"product.name"}}]}`

func TestStructureTemplateTypeAccepted(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	for _, et := range []string{contenttemplatemodel.EntityTypeHeader, contenttemplatemodel.EntityTypeFooter} {
		res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
			EntityType: et, Name: "结构-" + et, DraftDocument: []byte(headerDoc), ProjectID: projectID,
		})
		if err != nil {
			t.Fatalf("创建 %s 结构模板失败: %v", et, err)
		}
		if res.EntityType != et {
			t.Fatalf("类型回执不符: got %q want %q", res.EntityType, et)
		}
	}

	if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "not-a-type", Name: "x", DraftDocument: []byte(headerDoc), ProjectID: projectID,
	}); err == nil {
		t.Fatal("未注册的实体类型应被拒绝")
	}
}

func TestStructureTemplateRejectsFieldBinding(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	_, err := svc.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "带字段绑定的页眉",
		DraftDocument: []byte(fieldBoundDoc), ProjectID: projectID,
	})
	if err == nil {
		t.Fatal("结构模板带字段绑定应被拒绝")
	}
	if !strings.Contains(err.Error(), contenttemplateenums.ErrFieldBindingInvalid) {
		t.Fatalf("错误应为字段绑定类（%s）: %v", contenttemplateenums.ErrFieldBindingInvalid, err)
	}
}

func TestActivateKeepsSingleActivePerType(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	mk := func(et, name string) *contenttemplatedto.TemplateResp {
		res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
			EntityType: et, Name: name, DraftDocument: []byte(headerDoc), ProjectID: projectID,
		})
		if err != nil {
			t.Fatalf("创建 %s 失败: %v", name, err)
		}
		return res
	}
	headerA, headerB := mk(contenttemplatemodel.EntityTypeHeader, "页眉 A"), mk(contenttemplatemodel.EntityTypeHeader, "页眉 B")
	footer := mk(contenttemplatemodel.EntityTypeFooter, "页脚 A")

	activeOf := func(et string) []string {
		t.Helper()
		list, err := svc.List(ctx, &contenttemplatedto.ListReq{EntityType: et})
		if err != nil {
			t.Fatalf("列 %s 模板失败: %v", et, err)
		}
		var ids []string
		for _, tpl := range list {
			if tpl.IsDefault {
				ids = append(ids, tpl.ID)
			}
		}
		return ids
	}

	if _, err := svc.Activate(ctx, &contenttemplatedto.ActivateReq{ID: headerA.ID}); err != nil {
		t.Fatalf("激活页眉 A 失败: %v", err)
	}
	if got := activeOf(contenttemplatemodel.EntityTypeHeader); len(got) != 1 || got[0] != headerA.ID {
		t.Fatalf("页眉类型生效模板应恰好是 A: %v", got)
	}

	if _, err := svc.Activate(ctx, &contenttemplatedto.ActivateReq{ID: headerB.ID}); err != nil {
		t.Fatalf("激活页眉 B 失败: %v", err)
	}
	if got := activeOf(contenttemplatemodel.EntityTypeHeader); len(got) != 1 || got[0] != headerB.ID {
		t.Fatalf("切换后页眉生效模板应恰好是 B: %v", got)
	}

	if _, err := svc.Activate(ctx, &contenttemplatedto.ActivateReq{ID: footer.ID}); err != nil {
		t.Fatalf("激活页脚失败: %v", err)
	}
	if got := activeOf(contenttemplatemodel.EntityTypeFooter); len(got) != 1 || got[0] != footer.ID {
		t.Fatalf("页脚生效模板应恰好是 A: %v", got)
	}
	if got := activeOf(contenttemplatemodel.EntityTypeHeader); len(got) != 1 || got[0] != headerB.ID {
		t.Fatalf("激活页脚不应影响页眉的生效模板: %v", got)
	}
}
