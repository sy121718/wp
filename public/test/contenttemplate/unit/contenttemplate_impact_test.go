package unit

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

type fakeImpactPort struct {
	refs []contenttemplatedto.TemplateReference
	err  error
}

func (f *fakeImpactPort) ListTemplateReferences(_ context.Context, _ string, _ []string) (
	refs []contenttemplatedto.TemplateReference, unparsable int, err error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.refs, 0, nil
}

func TestTemplateImpactReportsAvailabilityAndReferences(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Impact(ctx, &contenttemplatedto.ImpactReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("Impact 失败: %v", err)
	}
	if res.Available {
		t.Fatal("未注入端口时 Available 应为 false")
	}
	if res.References == nil {
		t.Fatal("References 应为非 nil 空切片（模板直接 range，nil 会被当成缺键）")
	}

	svc.SetImpactPort(&fakeImpactPort{refs: []contenttemplatedto.TemplateReference{{
		TemplateID: "tpl-1", Kind: contenttemplatedto.ReferenceKindPage,
		PageID: "page-1", PagePath: "/about", PageTitle: "关于我们", Slots: []string{"header"},
	}}})
	res, err = svc.Impact(ctx, &contenttemplatedto.ImpactReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("Impact 失败: %v", err)
	}
	if !res.Available || len(res.References) != 1 {
		t.Fatalf("注入端口后应 Available=true 且 1 条引用，实际 %+v", res)
	}

	svc.SetImpactPort(&fakeImpactPort{err: context.DeadlineExceeded})
	if _, err = svc.Impact(ctx, &contenttemplatedto.ImpactReq{ProjectID: projectID}); err == nil {
		t.Fatal("端口报错时 Impact 应上抛错误")
	}
}

func TestPageReferenceBlocksTemplateDelete(t *testing.T) {
	svc, _, projectID := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	header, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "站点页眉",
		DraftDocument: []byte(headerDoc), ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("创建结构模板失败: %v", err)
	}

	svc.SetImpactPort(&fakeImpactPort{refs: []contenttemplatedto.TemplateReference{{
		TemplateID: header.ID, Kind: contenttemplatedto.ReferenceKindPage,
		PageID: "page-1", PagePath: "/about", PageTitle: "关于我们", Slots: []string{"header"},
	}}})
	err = svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID})
	if err == nil {
		t.Fatal("被页面引用的模板不该删除成功")
	}
	if !strings.Contains(err.Error(), contenttemplateenums.ErrTemplateInUse) {
		t.Fatalf("错误应带 %s 前缀（handler 的三件套按它取文案）: %v", contenttemplateenums.ErrTemplateInUse, err)
	}

	if !strings.Contains(err.Error(), "关于我们") || !strings.Contains(err.Error(), "页眉") {
		t.Fatalf("错误应给出可定位数据（页面名 + 槽位）: %v", err)
	}

	if _, err = svc.Get(ctx, &contenttemplatedto.GetReq{ID: header.ID}); err != nil {
		t.Fatalf("删除被拒绝后模板应仍然存在: %v", err)
	}

	svc.SetImpactPort(&fakeImpactPort{})
	if err = svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
		t.Fatalf("解绑后应可删除: %v", err)
	}
}
