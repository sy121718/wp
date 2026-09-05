package unit

import (
	"context"
	"testing"

	pubmodel "go_wp/internal/module/publication/model"
)

// TestPublicationRollbackReceiptsPending 回滚 pending 回执：标记 rolled_back
// 并设置 completed_at，返回处理数量。
func TestPublicationRollbackReceiptsPending(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	if err := svc.Model().ReceiptDB(ctx).Create(&pubmodel.ReceiptEntity{
		ID: "aaaaaaaa-0000-0000-0000-000000000001", SourceType: "page", SourceID: pageID,
		Action: "activate", Path: "/p1", ToArtifact: strPtr(artifactUUID),
		ReceiptState: pubmodel.ReceiptPending, ReceiptData: []byte(`{}`),
	}).Error; err != nil {
		t.Fatalf("制造 pending 回执失败: %v", err)
	}

	count, err := svc.RollbackReceipts(ctx)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("应处理 1 条回执: %d", count)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptPending); n != 0 {
		t.Fatalf("不应残留 pending: %d", n)
	}
	if n := countReceipts(t, svc, "receipt_state = ? AND completed_at IS NOT NULL", pubmodel.ReceiptRolledBack); n != 1 {
		t.Fatalf("rolled_back 回执应带 completed_at: %d", n)
	}
}

// TestPublicationRollbackReceiptsNone 无 pending 回执：返回 0 且无错误。
func TestPublicationRollbackReceiptsNone(t *testing.T) {
	svc := newUnitService(t)
	count, err := svc.RollbackReceipts(context.Background())
	if err != nil {
		t.Fatalf("空库回滚不应报错: %v", err)
	}
	if count != 0 {
		t.Fatalf("应返回 0: %d", count)
	}
}

// TestPublicationRollbackReceiptsMixed 混合状态：只处理 pending，committed 不受影响。
func TestPublicationRollbackReceiptsMixed(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	for i, state := range []string{pubmodel.ReceiptPending, pubmodel.ReceiptCommitted, pubmodel.ReceiptRolledBack} {
		id := "bbbbbbbb-0000-0000-0000-00000000000" + string(rune('1'+i))
		if err := svc.Model().ReceiptDB(ctx).Create(&pubmodel.ReceiptEntity{
			ID: id, SourceType: "page", SourceID: pageID,
			Action: "activate", Path: "/mix", ToArtifact: strPtr(artifactUUID),
			ReceiptState: state, ReceiptData: []byte(`{}`),
		}).Error; err != nil {
			t.Fatalf("制造回执 %s 失败: %v", state, err)
		}
	}

	count, err := svc.RollbackReceipts(ctx)
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("只应处理 1 条 pending: %d", count)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptCommitted); n != 1 {
		t.Fatalf("committed 不应被改动: %d", n)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptRolledBack); n != 2 {
		t.Fatalf("rolled_back 应为 2 条（原 1 + 新 1）: %d", n)
	}
}
