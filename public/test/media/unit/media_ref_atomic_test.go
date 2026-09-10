package unit

// media_ref_atomic_test.go — 引用缓存原子性 + 分类改名查重基准的回归测试。
//
// 两处都属于「并发 / 组合输入下才暴露」的缺陷：
//  1. AddRef 原先先查 HasRef 再 UPDATE（check-then-act），两个页面并发构建引用同一张图时
//     双方都查到 false、各自追加一次 → refs 出现重复项；现已把判重并入 UPDATE 守卫，
//     由 RowsAffected 判定「本次是否真的新增」。
//  2. UpdateCategory 同时改名 + 移动时按**旧**父级查重，会漏检新父级下的同名分类
//     （无 DB 唯一约束兜底），查重基准必须换成移动后的父级。

import (
	"context"
	"sync"
	"testing"

	mediadto "go_wp/internal/module/media/dto"
	mediamodel "go_wp/internal/module/media/model"
)

// TestAttachmentAddRefIdempotent 同一 kind+id 重复追加只保留一条，第二次返回 false。
func TestAttachmentAddRefIdempotent(t *testing.T) {
	db, _ := newMediaUnitService(t)
	am := mediamodel.NewAttachmentModel(db)
	ctx := context.Background()
	id := seedAttachment(t, db, nil, "atomic.png", "image", "")

	added, err := am.AddRef(ctx, id, mediamodel.AttachmentRef{Kind: "page", ID: "p-1", Title: "首页"})
	if err != nil {
		t.Fatalf("AddRef: %v", err)
	}
	if !added {
		t.Errorf("首次追加应返回 true（本次确实新增）")
	}
	// extra_info 初始为 SQL NULL 时 `@>` 返回 NULL、NOT NULL 仍为 NULL，
	// 守卫里必须显式放行 IS NULL —— 这一步同时覆盖该分支。
	added, err = am.AddRef(ctx, id, mediamodel.AttachmentRef{Kind: "page", ID: "p-1", Title: "首页"})
	if err != nil {
		t.Fatalf("AddRef(重复): %v", err)
	}
	if added {
		t.Errorf("重复追加应返回 false（不得写入第二份条目）")
	}
	refs, err := am.ListRefs(ctx, id)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("refs 应只有一条，got %d：%+v", len(refs), refs)
	}
}

// TestAttachmentAddRefConcurrentSingleEntry 并发追加同一引用只落一条。
// 原来的 check-then-act 在这里会写出多条（各方都查到 false 后各追加一次）。
func TestAttachmentAddRefConcurrentSingleEntry(t *testing.T) {
	db, _ := newMediaUnitService(t)
	am := mediamodel.NewAttachmentModel(db)
	ctx := context.Background()
	id := seedAttachment(t, db, nil, "concurrent.png", "image", "")

	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, errs[slot] = am.AddRef(ctx, id, mediamodel.AttachmentRef{Kind: "page", ID: "p-1"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发 AddRef #%d: %v", i, err)
		}
	}
	refs, err := am.ListRefs(ctx, id)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("并发追加同一引用应只落一条，got %d：%+v", len(refs), refs)
	}
}

// TestUpdateCategoryRenameChecksTargetParent 改名 + 移动同时提交时，查重基准必须是新父级。
func TestUpdateCategoryRenameChecksTargetParent(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	parentA := seedCategory(t, db, "父级A", 0, 1)
	parentB := seedCategory(t, db, "父级B", 0, 1)
	seedCategory(t, db, "同名分类", parentB.ID, 1)
	src := seedCategory(t, db, "待改分类", parentA.ID, 1)

	name := "同名分类"
	newParent := parentB.ID
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{
		ID: src.ID, CategoryName: &name, ParentID: &newParent,
	}); err == nil {
		t.Errorf("移动到 B 并改名为 B 下已有的名称，必须拒绝（按新父级查重）")
	}

	// 移到没有同名分类的父级应放行 —— 确认不是「一律拒绝」。
	var emptyParent uint64
	if err := db.Raw(`INSERT INTO sys_file_category
		(category_name, category_code, parent_id, sort_order, status, create_time, update_time)
		VALUES ('空父级', 'seed_empty_parent', 0, 0, 1, NOW(), NOW()) RETURNING id`).Scan(&emptyParent).Error; err != nil {
		t.Fatalf("插入空父级分类失败: %v", err)
	}
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{
		ID: src.ID, CategoryName: &name, ParentID: &emptyParent,
	}); err != nil {
		t.Errorf("新父级下没有同名分类，应放行，got %v", err)
	}
}
