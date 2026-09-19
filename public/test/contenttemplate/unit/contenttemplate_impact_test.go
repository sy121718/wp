package unit

// contenttemplate_impact_test.go — 引用影响面与「页面引用」删除保护（读侧反查）。
//
// 与 structure_template_delete_guard_test.go 互补：那条管「被其它模板绑成结构槽位」
//（引用写在本模块的 content_templates 表里），这条管「被页面文档的 settings.structure 绑定」
//（写在跨模块的 JSONB 里，数据库外键管不到）。两者都必须做到：
//   · 被引用 → 拒绝删除，且错误里给出**可定位**信息（页面名 + 槽位）；
//   · 解绑之后 → 正常删除（拦截不能把正常路径也堵住）；
//   · 端口未装配 → Impact 报 Available=false，而不是「没有引用」。

import (
	"context"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

// fakeImpactPort 引用反查端口的测试替身：端口是接口，删除保护与影响面因此不必起真实页面表。
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

	// 未注入端口：Available=false（「查不出来」），References 必须为空切片而不是「没有引用」的错觉。
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

	// 注入端口：引用按原样透出（本层不重新解释来源）。
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

	// 端口报错：影响面必须上抛（页面据此显示「查不出来」），不能静默成「没有引用」。
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
	// 可定位数据：页面名 + 槽位（否则运营只知道「被引用」，不知道去哪儿解绑）。
	if !strings.Contains(err.Error(), "关于我们") || !strings.Contains(err.Error(), "页眉") {
		t.Fatalf("错误应给出可定位数据（页面名 + 槽位）: %v", err)
	}
	// 拒绝删除必须真的没删。
	if _, err = svc.Get(ctx, &contenttemplatedto.GetReq{ID: header.ID}); err != nil {
		t.Fatalf("删除被拒绝后模板应仍然存在: %v", err)
	}

	// 页面解绑（端口不再返回引用）→ 正常删除。
	svc.SetImpactPort(&fakeImpactPort{})
	if err = svc.Delete(ctx, &contenttemplatedto.DeleteReq{ID: header.ID}); err != nil {
		t.Fatalf("解绑后应可删除: %v", err)
	}
}
