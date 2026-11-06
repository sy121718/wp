package unit

import (
	"context"
	"strings"
	"testing"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/pkg/facing"
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

	// 删除保护走事务内反查端口；页面引用以外部实体事实进入拒绝明细。
	withRefStub(svc, &stubTemplateRefs{rows: []contenttemplatecontract.TemplateReferenceRow{{
		TemplateID: header.ID, Kind: contenttemplatecontract.TemplateReferenceKindPage,
		EntityID: "page-1", Label: "关于我们", Detail: "header",
	}}})
	err = svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID})
	if err == nil {
		t.Fatal("被页面引用的模板不该删除成功")
	}
	if !strings.Contains(err.Error(), contenttemplateenums.ErrTemplateInUse) {
		t.Fatalf("错误应带 %s 前缀（handler 的三件套按它取文案）: %v", contenttemplateenums.ErrTemplateInUse, err)
	}

	// 可定位明细走结构化 BlockedItems（对外只进 data.items），不拼进主消息。
	blocked, ok := err.(interface{ BlockedItems() []facing.BlockedItem })
	if !ok || len(blocked.BlockedItems()) != 1 {
		t.Fatalf("拒绝事实应带一条可定位明细: %v", err)
	}
	item := blocked.BlockedItems()[0]
	// 被拒绝的是模板本身；明细里给出引用方名称与槽位。
	if !strings.Contains(item.Fallback, "关于我们") || !strings.Contains(item.Fallback, "header") {
		t.Fatalf("明细应给出页面与槽位: %+v", item)
	}

	if _, err = svc.Get(ctx, &contenttemplatedto.GetReq{ID: header.ID}); err != nil {
		t.Fatalf("删除被拒绝后模板应仍然存在: %v", err)
	}

	withRefStub(svc, &stubTemplateRefs{})
	if err = svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
		t.Fatalf("解绑后应可删除: %v", err)
	}
}
