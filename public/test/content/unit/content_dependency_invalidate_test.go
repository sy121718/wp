package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
)

type recordingInvalidator struct {
	keys []publish.DepKey
}

func (r *recordingInvalidator) Invalidate(_ context.Context, kind, key string) {
	r.keys = append(r.keys, publish.DepKey{Kind: kind, Key: key})
}

func TestContentChangeInvalidatesDependencySources(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	rec := &recordingInvalidator{}
	svc.SetDependencyInvalidator(rec)

	created, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "fanout-story",
		Data: map[string]any{"title": "扇出故事"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	wantKeys := func(stage string) {
		t.Helper()
		if len(rec.keys) != 2 {
			t.Fatalf("%s 应扇出 2 条依赖键，实际 %d: %+v", stage, len(rec.keys), rec.keys)
		}
		if rec.keys[0] != publish.DirectContentKey("article", created.ID) {
			t.Fatalf("%s 第一条应为 direct_content 实体键，实际 %+v", stage, rec.keys[0])
		}
		if rec.keys[1] != publish.ContentCollectionKey("article") {
			t.Fatalf("%s 第二条应为集合键，实际 %+v", stage, rec.keys[1])
		}
	}
	wantKeys("创建")

	rec.keys = nil
	if _, err = svc.Update(ctx, &contentdto.UpdateReq{
		ID: created.ID, Data: map[string]any{"title": "扇出故事 v2"},
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	wantKeys("更新")

	rec.keys = nil
	if err = svc.Delete(ctx, &contentdto.DeleteReq{ID: created.ID}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	wantKeys("删除")
}

func TestContentWithoutInvalidatorIsNoop(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	svc.SetDependencyInvalidator(nil)
	if _, err := svc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "no-invalidator",
		Data: map[string]any{"title": "无扇出"},
	}); err != nil {
		t.Fatalf("未注入 invalidator 时创建不应失败: %v", err)
	}
}

var _ contentcontract.DependencyInvalidator = (*recordingInvalidator)(nil)
