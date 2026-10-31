package feature

import (
	"context"
	"errors"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

func mustTransferStock(t *testing.T, f *invFixture, fromID, title string, qty int) (string, string) {
	t.Helper()
	p := mustProduct(t, f, title)
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, fromID, qty, "purchase_in")
	return v.ID, f.transferStockID(t, v.ID, fromID)
}

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

	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("未审批出货应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

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

	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("重复审批应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

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

	if got := f.stockQty(t, variantID, from.ID); got != 5 {
		t.Fatalf("驳回不该动源仓数量，实际 %d", got)
	}

	if _, err := f.inventory.ApproveTransfer(ctx, &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("驳回后同意应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}

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
