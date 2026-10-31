package feature

import (
	"context"
	"errors"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

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

	if call.EntityType != productcontract.EntityTypeCategory || call.EntityID != cat.ID || call.Slug != "electronics" {
		t.Fatalf("同步入参应带上分类身份（注册表口径的实体类型）与 slug，实际 %+v", call)
	}

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
