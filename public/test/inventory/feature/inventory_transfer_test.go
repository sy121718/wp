package feature

import (
	"context"
	"errors"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

type stubAttachments struct{ known map[int64]bool }

func (s *stubAttachments) ResolveAttachment(_ context.Context, _ string, id int64) (*inventorycontract.AttachmentBrief, error) {
	if s.known[id] {
		return &inventorycontract.AttachmentBrief{ID: id, FileName: "voucher.pdf"}, nil
	}
	return nil, inventoryenums.Err(inventoryenums.ErrAttachmentNotFound)
}

func (s *stubAttachments) ResolveAttachments(ctx context.Context, projectID string, ids []int64) (
	[]*inventorycontract.AttachmentBrief, error) {
	out := make([]*inventorycontract.AttachmentBrief, 0, len(ids))
	for _, id := range ids {
		one, err := s.ResolveAttachment(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

func mustApproveTransfer(t *testing.T, f *invFixture, id string) {
	t.Helper()
	if _, err := f.inventory.ApproveTransfer(context.Background(), &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: id,
	}); err != nil {
		t.Fatalf("同意调拨失败: %v", err)
	}
}

func (f *invFixture) transferStockID(t *testing.T, variantID, warehouseID string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT id FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&id).Error; err != nil {
		t.Fatalf("读库存行 id 失败: %v", err)
	}
	if id == "" {
		t.Fatalf("该变体在仓 %s 没有库存行", warehouseID)
	}
	return id
}

func (f *invFixture) unclaimedStockID(t *testing.T, warehouseID, sku string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT id FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ? AND variant_id IS NULL",
		warehouseID, sku).Scan(&id).Error; err != nil {
		t.Fatalf("读未认领行 id 失败: %v", err)
	}
	if id == "" {
		t.Fatalf("仓 %s 里没有未认领行 %s", warehouseID, sku)
	}
	return id
}

func (f *invFixture) attachmentsOf(t *testing.T, transferID, usage string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_transfer_attachments WHERE transfer_id = ? AND usage_type = ?",
		transferID, usage).Scan(&n).Error; err != nil {
		t.Fatalf("读凭证条数失败: %v", err)
	}
	return n
}

func newTransferFixture(t *testing.T) (*invFixture, *inventorydto.WarehouseResp, *inventorydto.WarehouseResp) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil, nil
	}

	f.createWarehouse(t, "DF", "默认仓", true)
	from := f.createWarehouse(t, "SZ", "苏州仓", false)
	to := f.createWarehouse(t, "SH", "上海仓", false)
	return f, from, to
}

func TestTransferSameWarehouseRejected(t *testing.T) {
	f, from, _ := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨同仓")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)

	_, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: from.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)) {
		t.Fatalf("源仓 = 目标仓应报 ErrTransferSameWarehouse，实际 %v", err)
	}
}

func TestTransferItemMustBelongToSourceWarehouse(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨跨仓明细")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	changeIn(t, f, p, v, to.ID, 5, "purchase_in")

	toStockID := f.transferStockID(t, v.ID, to.ID)

	_, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: toStockID, Quantity: 1}},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)) {
		t.Fatalf("明细里塞别的仓的 stock_id 应报 ErrItemNotInSourceWarehouse，实际 %v", err)
	}
}

func TestTransferRejectsInsufficientAvailable(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨可售")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 10, "purchase_in")

	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-transfer",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 8, WarehouseID: from.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	stockID := f.transferStockID(t, v.ID, from.ID)

	_, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 5}},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockNotEnough)) {
		t.Fatalf("可售不足（10−8=2，要调 5）应报 ErrStockNotEnough，实际 %v", err)
	}
}

func TestTransferRequiresVoucherOnShipAndReceive(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true, 2: true}}
	f.inventory.SetAttachmentResolver(atts)

	p := mustProduct(t, f, "调拨凭证")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)) {
		t.Fatalf("无凭证出货应报 ErrAttachmentRequired，实际 %v", err)
	}

	detail, gerr := f.inventory.GetTransfer(ctx, &inventorydto.GetTransferReq{ProjectID: f.projectID, ID: created.ID})
	if gerr != nil {
		t.Fatalf("读调拨单失败: %v", gerr)
	}
	if detail.Transfer.Status != "pending" {
		t.Fatalf("被拒的出货不该改状态，实际 %q", detail.Transfer.Status)
	}
	if got := f.stockQty(t, v.ID, from.ID); got != 5 {
		t.Fatalf("被拒的出货不该动源仓数量，实际 %d", got)
	}

	shipped, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	})
	if err != nil {
		t.Fatalf("带凭证出货失败: %v", err)
	}
	if shipped.Status != "in_transit" {
		t.Fatalf("出货后状态应为 in_transit，实际 %q", shipped.Status)
	}

	if _, err := f.inventory.ReceiveTransfer(ctx, &inventorydto.TransferReceiveReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)) {
		t.Fatalf("无凭证签收应报 ErrAttachmentRequired，实际 %v", err)
	}
	detail, _ = f.inventory.GetTransfer(ctx, &inventorydto.GetTransferReq{ProjectID: f.projectID, ID: created.ID})
	if detail.Transfer.Status != "in_transit" {
		t.Fatalf("被拒的签收不该改状态，实际 %q", detail.Transfer.Status)
	}
}

func TestTransferUnknownAttachmentRollsBack(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true}}
	f.inventory.SetAttachmentResolver(atts)

	p := mustProduct(t, f, "调拨坏凭证")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)
	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1, 999},
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrAttachmentNotFound)) {
		t.Fatalf("凭证不存在应报 ErrAttachmentNotFound，实际 %v", err)
	}
	if got := f.attachmentsOf(t, created.ID, "ship"); got != 0 {
		t.Fatalf("被拒的出货不该留下凭证行，实际 %d 条", got)
	}
	if got := f.stockQty(t, v.ID, from.ID); got != 5 {
		t.Fatalf("被拒的出货不该动源仓数量，实际 %d", got)
	}
}

func TestTransferFullChain(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true, 2: true}}
	f.inventory.SetAttachmentResolver(atts)

	p := mustProduct(t, f, "调拨全链路")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 10, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)
	before := f.movementCount(t)

	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 5}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); err != nil {
		t.Fatalf("出货失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, from.ID); got != 5 {
		t.Fatalf("出货后源仓应剩 5，实际 %d", got)
	}
	if got := f.movementCount(t); got != before+1 {
		t.Fatalf("出货应写 1 条流水，期望 %d，实际 %d", before+1, got)
	}
	var sourceType, sourceRef, reason string
	if qerr := f.db.Raw("SELECT source_type, source_ref, reason_code FROM inventory_stock_movements "+
		"WHERE project_id = ? ORDER BY create_time DESC LIMIT 1", f.projectID).
		Row().Scan(&sourceType, &sourceRef, &reason); qerr != nil {
		t.Fatalf("读流水失败: %v", qerr)
	}
	if sourceType != "transfer" || reason != "transfer_out" {
		t.Fatalf("出货流水的来源/原因不对：%q / %q", sourceType, reason)
	}

	received, err := f.inventory.ReceiveTransfer(ctx, &inventorydto.TransferReceiveReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{2},
	})
	if err != nil {
		t.Fatalf("签收失败: %v", err)
	}
	if received.Status != "completed" {
		t.Fatalf("签收后状态应为 completed，实际 %q", received.Status)
	}
	var toQty int
	if qerr := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, to.ID).Row().Scan(&toQty); qerr != nil {
		t.Fatalf("读目标仓库存失败: %v", qerr)
	}
	if toQty != 5 {
		t.Fatalf("签收后目标仓应有 5 件，实际 %d", toQty)
	}

	if got := f.attachmentsOf(t, created.ID, "ship"); got != 1 {
		t.Fatalf("出货凭证应有 1 条，实际 %d", got)
	}
	if got := f.attachmentsOf(t, created.ID, "receive"); got != 1 {
		t.Fatalf("签收凭证应有 1 条，实际 %d", got)
	}
}

func TestTransferUnclaimedStock(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true, 2: true}}
	f.inventory.SetAttachmentResolver(atts)

	loose := createStock(t, f, from.ID, "TRANSFER_ORPHAN", 6, true)
	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: loose.ID, Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("未认领行的调拨建单失败: %v", err)
	}
	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); err != nil {
		t.Fatalf("未认领行出货失败: %v", err)
	}
	if _, err := f.inventory.ReceiveTransfer(ctx, &inventorydto.TransferReceiveReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{2},
	}); err != nil {
		t.Fatalf("未认领行签收失败: %v", err)
	}

	var variantID *string
	var qty int
	if qerr := f.db.Raw("SELECT variant_id, quantity FROM inventory_stocks "+
		"WHERE warehouse_id = ? AND sku_code = ?", to.ID, "TRANSFER_ORPHAN").
		Row().Scan(&variantID, &qty); qerr != nil {
		t.Fatalf("读目标仓未认领行失败: %v", qerr)
	}
	if variantID != nil {
		t.Fatalf("未认领的货调拨之后目标仓仍应是未认领（variant_id 为 NULL），实际 %v", *variantID)
	}
	if qty != 4 {
		t.Fatalf("目标仓该行应有 4 件，实际 %d", qty)
	}
}

func TestTransferIdempotentByRequestID(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨幂等")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)

	req := &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		RequestID: "transfer-req-1",
		Lines:     []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	}
	first, err := f.inventory.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("首次建单失败: %v", err)
	}
	second, err := f.inventory.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("重复建单应幂等成功: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同一 request_id 应命中同一张单，实际 %s vs %s", first.ID, second.ID)
	}
	var n int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM inventory_transfers WHERE project_id = ?",
		f.projectID).Scan(&n).Error; qerr != nil {
		t.Fatalf("查调拨单数失败: %v", qerr)
	}
	if n != 1 {
		t.Fatalf("同一 request_id 只该有 1 张单，实际 %d 张", n)
	}
}

func TestTransferAttachmentUniqueIndex(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true}}
	f.inventory.SetAttachmentResolver(atts)
	p := mustProduct(t, f, "调拨凭证唯一")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)
	created, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	}); err != nil {
		t.Fatalf("出货失败: %v", err)
	}

	if err := f.db.Exec("INSERT INTO inventory_transfer_attachments (transfer_id, attachment_id, usage_type) "+
		"VALUES (?, 1, 'ship')", created.ID).Error; err == nil {
		t.Fatalf("同 (transfer_id, attachment_id, usage_type) 插第二次应撞唯一索引")
	}
}

func TestTransferCancelOnlyPending(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true}}
	f.inventory.SetAttachmentResolver(atts)
	p := mustProduct(t, f, "调拨取消")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 5, "purchase_in")
	stockID := f.transferStockID(t, v.ID, from.ID)

	pending, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if _, err := f.inventory.CancelTransfer(ctx, &inventorydto.TransferCancelReq{
		ProjectID: f.projectID, ID: pending.ID, Remark: "计划改了",
	}); err != nil {
		t.Fatalf("pending 的单取消应成功: %v", err)
	}

	shipped, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: stockID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("建第二张单失败: %v", err)
	}
	mustApproveTransfer(t, f, shipped.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: shipped.ID, AttachmentIDs: []int64{1},
	}); err != nil {
		t.Fatalf("出货失败: %v", err)
	}
	if _, err := f.inventory.CancelTransfer(ctx, &inventorydto.TransferCancelReq{
		ProjectID: f.projectID, ID: shipped.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)) {
		t.Fatalf("in_transit 的单取消应报 ErrTransferStatusInvalid，实际 %v", err)
	}
}
