package feature

// product_category_archive_test.go — 分类变更触发归档页同步（审计 EDT-004 实体侧联动）。
//
// 闭环的最后一段：分类是「谁」，归档页是「它的列表页」。分类侧只负责**通知**，
// 归档页该不该建、路径怎么定，全在 presentation 内部决定（端口只给一条方法）。
//
// 两条断言看似平凡，但都是刻意的设计选择：
//  1. 新建分类会带上分类 id 与 slug 去同步；
//  2. **同步失败不阻断分类保存** —— 归档页是派生视图，不是分类的一部分。
//     反过来会让一个次要问题挡住主流程，而且分类此时已经写进库里了。

import (
	"context"
	"errors"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
)

// stubArchiveEnsurer 记录归档同步调用；err 非空时模拟同步失败。
type stubArchiveEnsurer struct {
	calls []presentationdto.EnsureArchiveReq
	err   error
}

func (s *stubArchiveEnsurer) EnsureArchiveInstance(_ context.Context, req *presentationdto.EnsureArchiveReq) (*presentationdto.EnsureArchiveResp, error) {
	if req != nil {
		s.calls = append(s.calls, *req)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &presentationdto.EnsureArchiveResp{InstanceID: "inst-1", Created: true}, nil
}

func TestCategoryCreateEnsuresArchivePage(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	stub := &stubArchiveEnsurer{}
	f.svc.SetArchiveInstanceEnsurer(stub)

	cat, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "电子设备", Slug: "electronics",
	})
	if err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("新建分类应触发一次归档页同步，实际 %d 次", len(stub.calls))
	}
	call := stub.calls[0]
	if call.EntityType != "category" || call.EntityID != cat.ID || call.Slug != "electronics" {
		t.Fatalf("同步入参应带上分类身份与 slug，实际 %+v", call)
	}

	// 改名：slug 变化同样要同步（presentation 内部比对路径后决定是否重建）。
	newSlug := "electronics-pro"
	if _, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{
		ID: cat.ID, Slug: &newSlug,
	}); err != nil {
		t.Fatalf("更新分类失败: %v", err)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("改名应再同步一次，实际 %d 次", len(stub.calls))
	}
	if stub.calls[1].Slug != newSlug {
		t.Fatalf("改名后应带新 slug 同步，实际 %q", stub.calls[1].Slug)
	}
}

// TestCategoryCreateSurvivesArchiveFailure 归档页同步失败不阻断分类保存。
func TestCategoryCreateSurvivesArchiveFailure(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.svc.SetArchiveInstanceEnsurer(&stubArchiveEnsurer{err: errors.New("presentation 暂时不可用")})

	cat, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "归档失败也不该拦", Slug: "archive-fail",
	})
	if err != nil {
		t.Fatalf("归档页同步失败不应阻断分类创建: %v", err)
	}
	if cat == nil || cat.ID == "" {
		t.Fatalf("分类应已创建成功")
	}
}
