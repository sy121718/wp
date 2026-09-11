// content_dependency_invalidate_test.go — PIPE-3：内容变更的依赖源扇出（真实 PG）。
//
// content 模块只负责「声明自己是哪个依赖源」，本测试用记录型 invalidator 断言
// 声明内容精确：每个实体的写操作都产出且只产出两条键——
//
//	direct_content:{type}:{id}（直接引用该实体）
//	content_collection:collection:content:{type}（渲染该类型集合）
//
// 迁移 080 后内容类型收敛为 article，用例统一用它。
package unit

import (
	"context"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/pipeline"
)

// recordingInvalidator 记录被触发的依赖源键。
type recordingInvalidator struct {
	keys []pipeline.DepKey
}

func (r *recordingInvalidator) Invalidate(_ context.Context, kind, key string) {
	r.keys = append(r.keys, pipeline.DepKey{Kind: kind, Key: key})
}

// TestContentChangeInvalidatesDependencySources 增/改/删都扇出两条键。
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
		if rec.keys[0] != pipeline.DirectContentKey("article", created.ID) {
			t.Fatalf("%s 第一条应为 direct_content 实体键，实际 %+v", stage, rec.keys[0])
		}
		if rec.keys[1] != pipeline.ContentCollectionKey("article") {
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

// TestContentWithoutInvalidatorIsNoop 未注入扇出端口时行为与本轮之前一致（不 panic）。
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

// 编译期断言：记录型实现满足 content 模块的扇出契约。
var _ contentcontract.DependencyInvalidator = (*recordingInvalidator)(nil)
