// product_related_delete_cleanup_test.go — DEC-04 裁决（同事务清理）的链路判据。
//
// related_ids 是 products 表的 JSONB 自引用，没有外键兜底：删掉 A 之后，
// B.related_ids 里仍会留着 A。三种策略里选**同事务清理**：
//
//	· 阻止删除会把「后台看不见的引用」变成删不掉的硬墙；
//	· 读取剔除会让脏 id 永远留在库里，产物里的相关商品卡指向 404；
//	· 清理把「引用一个被删商品」的唯一正确结果写进同一事务，失败整体回滚。
//
// 判据：
//  1. 单删后引用者的 related_ids 不再含被删 id，其余引用原样保留；
//  2. 批删（整成全败的路径）同样清理；
//  3. 被删集合内部的互相引用不报错（两边都消失，无需清理）；
//  4. 引用者被清理时发产物失效事件（outbox 有该实体的键）。
package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// createRelated 建商品并把 given 的 related_ids 写进去（走真实写路径的校验）。
func (f *fixture) createRelated(t *testing.T, name, slug string, related ...string) *productdto.ProductResp {
	t.Helper()
	res := f.mkRelatedProduct(t, f.projectID, name, slug)
	if len(related) == 0 {
		return res
	}
	if _, err := f.svc.Update(context.Background(), &productdto.UpdateReq{
		ProjectID: f.projectID, ID: res.ID, RelatedIDs: related,
	}); err != nil {
		t.Fatalf("给 %s 写相关商品失败: %v", name, err)
	}
	return res
}

// requireOutboxFor 断言引用者有一行产物失效事件（清理不能悄悄改数据）。
func (f *fixture) requireOutboxFor(t *testing.T, productID string) {
	t.Helper()
	var n int64
	if err := f.db.Raw(
		"SELECT COUNT(*) FROM product_outbox_events WHERE dependency_key LIKE ?", "%"+productID+"%").
		Scan(&n).Error; err != nil {
		t.Fatalf("查失效事件失败: %v", err)
	}
	if n == 0 {
		t.Fatalf("引用者 %s 被清理后应发产物失效事件，实际 0 行", productID)
	}
}

// TestDeleteProductCleansRelatedRefs 单删：引用者只剩自己保留的那条。
func TestDeleteProductCleansRelatedRefs(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	a := f.mkRelatedProduct(t, f.projectID, "被删甲", "del-a")
	keep := f.mkRelatedProduct(t, f.projectID, "保留乙", "del-b")
	c := f.createRelated(t, "引用者丙", "del-c", a.ID, keep.ID)

	if err := f.svc.Delete(ctx, &productdto.DeleteReq{ProjectID: f.projectID, ID: a.ID}); err != nil {
		t.Fatalf("删除商品失败: %v", err)
	}
	got := f.relatedIDsOf(t, f.projectID, c.ID)
	if len(got) != 1 || got[0] != keep.ID {
		t.Fatalf("删除后引用者应只剩 %s，实际 %v", keep.ID, got)
	}
	f.requireOutboxFor(t, c.ID)
	// 引用者自身仍可读（清理不是把行改坏）。
	if res, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: c.ID}); err != nil || res.ID != c.ID {
		t.Fatalf("引用者应仍可读：res=%v err=%v", res, err)
	}
}

// TestBulkDeleteProductsCleansRelatedRefs 批删：引用者里的两条都被摘掉。
func TestBulkDeleteProductsCleansRelatedRefs(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	a := f.mkRelatedProduct(t, f.projectID, "批删甲", "bulk-del-a")
	b := f.mkRelatedProduct(t, f.projectID, "批删乙", "bulk-del-b")
	keep := f.mkRelatedProduct(t, f.projectID, "批删保留", "bulk-del-keep")
	c := f.createRelated(t, "批删引用者", "bulk-del-c", a.ID, keep.ID, b.ID)

	res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{ProjectID: f.projectID, IDs: []string{a.ID, b.ID}})
	if err != nil {
		t.Fatalf("批量删除失败: %v", err)
	}
	if res == nil || res.Done != 2 {
		t.Fatalf("批删应删掉 2 条，实际 %+v", res)
	}
	got := f.relatedIDsOf(t, f.projectID, c.ID)
	if len(got) != 1 || got[0] != keep.ID {
		t.Fatalf("批删后引用者应只剩 %s，实际 %v", keep.ID, got)
	}
}

// TestBulkDeleteProductsToleratesMutualRelated 被删集合内部互引：整体成功。
func TestBulkDeleteProductsToleratesMutualRelated(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	a := f.mkRelatedProduct(t, f.projectID, "互引甲", "mutual-a")
	b := f.createRelated(t, "互引乙", "mutual-b", a.ID)
	if _, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ProjectID: f.projectID, ID: a.ID, RelatedIDs: []string{b.ID},
	}); err != nil {
		t.Fatalf("写互引失败: %v", err)
	}
	res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{ProjectID: f.projectID, IDs: []string{a.ID, b.ID}})
	if err != nil {
		t.Fatalf("互相引用的两个商品应能一起删除: %v", err)
	}
	if res == nil || res.Done != 2 {
		t.Fatalf("应删掉 2 条，实际 %+v", res)
	}
	var left int64
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE id IN ?", []string{a.ID, b.ID}).Scan(&left).Error; err != nil {
		t.Fatalf("查残留商品失败: %v", err)
	}
	if left != 0 {
		t.Fatalf("两个商品都该被删掉，实际残留 %d", left)
	}
}
