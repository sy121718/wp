// inventory_transfer_test.go — W3（迁移 630）的验收：调拨单 + 明细 + 凭证附件。
//
// 覆盖任务书 §9.9 的会红断言：
//
//	1  建单源仓 = 目标仓 → 业务错误（DDL CHECK 兜底，应用层先给可读错）
//	2  明细里塞一条别的仓的 stock_id → ErrItemNotInSourceWarehouse
//	3  可售不足（quantity=10, reserved=8，调拨 5）→ ErrStockNotEnough
//	4  无凭证不能出货：AttachmentIDs=[] → ErrAttachmentRequired，单据仍 pending、源仓 quantity 未变
//	5  无凭证不能签收：同上，单据仍 in_transit
//	6  凭证 id 不存在 → 业务错误，事务回滚（附件行 0 条）
//	7  完整链路：建 5 件 → 出货（1 张凭证）→ 源仓 quantity -5、流水 1 条 transfer_out、
//	   状态 in_transit → 签收（1 张凭证）→ 目标仓 quantity +5、流水 1 条 transfer_in、
//	   状态 completed
//	8  未认领行可调拨：源行 variant_id IS NULL → 目标仓建出同样 variant_id IS NULL 的行
//	9  幂等：同一 request_id 建两次 → 一张单
//	10 附件唯一约束：同 (transfer_id, attachment_id, usage_type) 插两次撞索引
package feature

import (
	"context"
	"errors"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// stubAttachments 附件读口的替身：只认预置的若干 id（其余按「不存在」处理）。
//
// 用替身而不是真实 media：本用例测的是**调拨侧的凭证校验**（缺凭证拒绝、凭证不存在拒绝），
// 而不是 media 的上传链路 —— 后者已有自己的 feature 测试。
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

// mustApproveTransfer 同意一张刚建的调拨申请（迁移 651 起建单落 requested，出货前必须先同意）。
//
// 单独一个 helper 而不是在每个用例里写三行：调拨的**被测行为**不是审批本身
// （审批有自己的用例），其余用例只需要「这张单已经批过了」这个前置。
func mustApproveTransfer(t *testing.T, f *invFixture, id string) {
	t.Helper()
	if _, err := f.inventory.ApproveTransfer(context.Background(), &inventorydto.TransferApproveReq{
		ProjectID: f.projectID, ID: id,
	}); err != nil {
		t.Fatalf("同意调拨失败: %v", err)
	}
}

// transferStockID 该变体在某仓的库存行 id（调拨明细要用它）。
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

// unclaimedStockID 某仓某编码的**未认领**库存行 id。
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

// attachmentsOf 某调拨单某用途的凭证条数。
func (f *invFixture) attachmentsOf(t *testing.T, transferID, usage string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_transfer_attachments WHERE transfer_id = ? AND usage_type = ?",
		transferID, usage).Scan(&n).Error; err != nil {
		t.Fatalf("读凭证条数失败: %v", err)
	}
	return n
}

// newTransferFixture 建两个仓 + 一个带库存的商品，返回 (源仓, 目标仓, 商品, 变体)。
func newTransferFixture(t *testing.T) (*invFixture, *inventorydto.WarehouseResp, *inventorydto.WarehouseResp) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil, nil
	}
	// 默认仓单独建，源/目标各一个（都不设默认，避免「未指定仓」的兜底干扰断言）。
	f.createWarehouse(t, "DF", "默认仓", true)
	from := f.createWarehouse(t, "SZ", "苏州仓", false)
	to := f.createWarehouse(t, "SH", "上海仓", false)
	return f, from, to
}

// TestTransferSameWarehouseRejected 验收 1：源仓 = 目标仓被拒。
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

// TestTransferItemMustBelongToSourceWarehouse 验收 2：别的仓的 stock_id 被拒。
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
	// 拿**目标仓**的库存行 id 当源仓的明细。
	toStockID := f.transferStockID(t, v.ID, to.ID)

	_, err := f.inventory.CreateTransfer(ctx, &inventorydto.CreateTransferReq{
		ProjectID: f.projectID, FromWarehouseID: from.ID, ToWarehouseID: to.ID,
		Lines: []inventorydto.TransferLineReq{{StockID: toStockID, Quantity: 1}},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)) {
		t.Fatalf("明细里塞别的仓的 stock_id 应报 ErrItemNotInSourceWarehouse，实际 %v", err)
	}
}

// TestTransferRejectsInsufficientAvailable 验收 3：可售不足（预占留给订单）。
func TestTransferRejectsInsufficientAvailable(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := mustProduct(t, f, "调拨可售")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, from.ID, 10, "purchase_in")
	// 预占 8（可售只剩 2）。
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

// TestTransferRequiresVoucherOnShipAndReceive 验收 4/5：
// 无凭证不能出货、也不能签收（且状态与库存都不变）。
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

	// ④ 无凭证出货被拒。
	mustApproveTransfer(t, f, created.ID)
	if _, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)) {
		t.Fatalf("无凭证出货应报 ErrAttachmentRequired，实际 %v", err)
	}
	// 单据仍 pending、源仓数量未变。
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

	// 带凭证出货成功。
	shipped, err := f.inventory.ShipTransfer(ctx, &inventorydto.TransferShipReq{
		ProjectID: f.projectID, ID: created.ID, AttachmentIDs: []int64{1},
	})
	if err != nil {
		t.Fatalf("带凭证出货失败: %v", err)
	}
	if shipped.Status != "in_transit" {
		t.Fatalf("出货后状态应为 in_transit，实际 %q", shipped.Status)
	}

	// ⑤ 无凭证签收被拒。
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

// TestTransferUnknownAttachmentRollsBack 验收 6：凭证 id 不存在 → 事务回滚（附件行 0 条）。
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

	// 一张合法 + 一张不存在：整体拒绝。
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

// TestTransferFullChain 验收 7：建单 → 出货 → 签收的完整链路。
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

	// 出货：源仓 −5、流水 +1（transfer_out）、状态 in_transit。
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

	// 签收：目标仓 +5、状态 completed。
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
	// 凭证按用途分开落库。
	if got := f.attachmentsOf(t, created.ID, "ship"); got != 1 {
		t.Fatalf("出货凭证应有 1 条，实际 %d", got)
	}
	if got := f.attachmentsOf(t, created.ID, "receive"); got != 1 {
		t.Fatalf("签收凭证应有 1 条，实际 %d", got)
	}
}

// TestTransferUnclaimedStock 验收 8：未认领行可调拨，且目标仓建出的行同样是未认领。
func TestTransferUnclaimedStock(t *testing.T) {
	f, from, to := newTransferFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	atts := &stubAttachments{known: map[int64]bool{1: true, 2: true}}
	f.inventory.SetAttachmentResolver(atts)

	// 建一条未认领的货。
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
	// 目标仓应有一条同样 variant_id IS NULL、同编码的行，数量 4。
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

// TestTransferIdempotentByRequestID 验收 9：同一 request_id 只建一张单。
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

// TestTransferAttachmentUniqueIndex 验收 10：同 (transfer_id, attachment_id, usage_type) 插两次撞索引。
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
	// 手动再插一条同样的 (transfer, attachment, usage)：必须撞唯一索引。
	if err := f.db.Exec("INSERT INTO inventory_transfer_attachments (transfer_id, attachment_id, usage_type) "+
		"VALUES (?, 1, 'ship')", created.ID).Error; err == nil {
		t.Fatalf("同 (transfer_id, attachment_id, usage_type) 插第二次应撞唯一索引")
	}
}

// TestTransferCancelOnlyPending 取消只允许 pending（in_transit 不行）。
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

	// pending 可以取消。
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

	// in_transit 不能取消。
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
