// inventory_transfer_approval_test.go — 调拨审批（迁移 651 / B5）的验收。
//
// 覆盖会红的判据：
//
//	1  建单落 requested（不是 pending）：申请只是「我想搬」，还没人同意
//	2  未审批不能出货 → ErrTransferStatusInvalid（申请单绕不过审批）
//	3  同意 → pending，且记录审批人与时刻；之后才能出货
//	4  驳回 → rejected，理由落库并在详情里回得来；驳回不动库存
//	5  驳回理由必填（空串 / 空白 → ErrTransferRejectReasonRequired）
//	6  同意时重校可售：申请后货被别处搬走 → ErrStockNotEnough（不是等到出货才失败）
//	7  审批只发生在 requested 态：对 pending 单再同意 → ErrTransferStatusInvalid
//	8  同意时可改目标仓；改成源仓 → ErrTransferSameWarehouse
//	9  requested 可取消（不必先审批）
package feature

import (
	"context"
	"errors"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// mustTransferStock 造一件可调拨的货，返回 (商品, 变体, 源仓库存行 id)。
func mustTransferStock(t *testing.T, f *invFixture, fromID, title string, qty int) (string, string) {
	t.Helper()
	p := mustProduct(t, f, title)
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, fromID, qty, "purchase_in")
	return v.ID, f.transferStockID(t, v.ID, fromID)
}

// TestTransferCreateLandsRequested 判据 1 + 2：建单是申请态，未审批不能出货。
func TestTransferCreateLandsRequested(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, stockID := mustTransferStock(t, f, from.ID, "调拨审批建单", 5)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if created.Status != "requested" {
		t.Fatalf("建单应落 requested，实际 %q", created.Status)
	}
	if created.RequestedAt == "" {
		t.Fatal("建单应记申请时刻（requested_at）")
	}
	// 未审批直接出货：拒绝（申请单绕不过审批）。
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("未审批出货应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

// TestTransferApproveThenShip 判据 3：同意后转 pending 且记审批人，之后能出货。
func TestTransferApproveThenShip(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.inventory.SetAttachmentResolver(&stubAttachments{known: map[int64]bool{1: true}})
	_, stockID := mustTransferStock(t, f, from.ID, "调拨审批同意", 5)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	approved, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID, OperatorID: 42, OperatorName: "仓管小王",
	})
	if err != nil {
		t.Fatalf("同意失败: %v", err)
	}
	if approved.Status != "pending" {
		t.Fatalf("同意后应 pending，实际 %q", approved.Status)
	}
	if approved.ApprovedBy != 42 || approved.ApprovedName != "仓管小王" {
		t.Fatalf("应记录审批人与姓名，实际 %d / %q", approved.ApprovedBy, approved.ApprovedName)
	}
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); err != nil {
		t.Fatalf("同意之后应能出货: %v", err)
	}
	// 对已进入 in_transit 的单再同意：状态不允许。
	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("重复审批应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

// TestTransferRejectRecordsReason 判据 4 + 5：驳回落理由、不动库存、理由必填。
func TestTransferRejectRecordsReason(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	variantID, stockID := mustTransferStock(t, f, from.ID, "调拨审批驳回", 5)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// 理由空 / 全空白都拒绝。
	for _, reason := range []string{"", "   "} {
		if _, err := f.inventory.RejectTransfer(ctx, &inventorydto.TransferRejectReq{
			ProjectID: f.projectID, ID: created.ID, Reason: reason,
		}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferRejectReasonRequired)) {
			t.Fatalf("理由 %q 应报 ErrTransferRejectReasonRequired，实际 %v", reason, err)
		}
	}
	rejected, err := f.inventory.RejectTransfer(ctx, &inventorydto.TransferRejectReq{
		ProjectID: f.projectID, ID: created.ID, Reason: "目标仓满了，下周再提",
		OperatorID: 7, OperatorName: "主管老李",
	})
	if err != nil {
		t.Fatalf("驳回失败: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("驳回后应 rejected，实际 %q", rejected.Status)
	}
	if rejected.RejectedReason != "目标仓满了，下周再提" {
		t.Fatalf("驳回理由应原样落库，实际 %q", rejected.RejectedReason)
	}
	if rejected.RejectedName != "主管老李" {
		t.Fatalf("应记录驳回人姓名，实际 %q", rejected.RejectedName)
	}
	// 驳回不动库存。
	if got := f.stockQty(t, variantID, from.ID); got != 5 {
		t.Fatalf("驳回不该动源仓数量，实际 %d", got)
	}
	// 被驳回的单不能再同意。
	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("驳回后同意应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

// TestTransferApproveRechecksAvailable 判据 6：申请后货被搬走，审批时报库存不足。
func TestTransferApproveRechecksAvailable(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨审批可售")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// 申请之后，这批货被订单预占掉 3 件（可售只剩 2）。
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceRef: "ORD-1",
		Lines: []inventorydto.ReserveStockLine{{
			WarehouseID: from.ID, VariantID: v.ID, SKUCode: v.SKUCode, Quantity: 3,
		}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockNotEnough)) {
		t.Fatalf("可售不足时同意应报 ErrStockNotEnough，实际 %v", err)
	}
}

// TestTransferApproveCanChangeTarget 判据 8：同意时可改目标仓；改成源仓被拒。
func TestTransferApproveCanChangeTarget(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other := f.createWarehouse(t, "GZ", "广州仓", false)
	_, stockID := mustTransferStock(t, f, from.ID, "调拨审批改仓", 5)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// 改成源仓：与自己搬给自己同义。
	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID, ToWarehouseID: from.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)) {
		t.Fatalf("改成源仓应报 ErrTransferSameWarehouse，实际 %v", err)
	}
	approved, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID, ToWarehouseID: other.ID,
	})
	if err != nil {
		t.Fatalf("改仓同意失败: %v", err)
	}
	if approved.ToWarehouseID != other.ID {
		t.Fatalf("目标仓应改成广州仓，实际 %q", approved.ToWarehouseID)
	}
	if approved.ToCode != "GZ" {
		t.Fatalf("详情应回显新目标仓短码，实际 %q", approved.ToCode)
	}
}

// TestTransferCancelRequested 判据 9：申请态可以直接取消。
func TestTransferCancelRequested(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, stockID := mustTransferStock(t, f, from.ID, "调拨审批取消", 5)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	cancelled, err := f.inventory.CancelTransfer(ctx, &inventorydto.TransferCancelReq{
		ProjectID: f.projectID, ID: created.ID, Remark: "改主意了",
	})
	if err != nil {
		t.Fatalf("申请态取消失败: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("取消后应 cancelled，实际 %q", cancelled.Status)
	}
}
