package feature

import (
	"context"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

func createDraftOrder(t *testing.T, f *purchaseFixture, code, sourceID, warehouseID string,
	lines []purchasedto.PurchaseLineReq) *purchasedto.PurchaseOrderResp {
	t.Helper()
	res, err := f.purchase.CreatePurchaseOrder(context.Background(), &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: code, SourceID: sourceID, WarehouseID: warehouseID,
		AsDraft: true, Lines: lines,
	})
	if err != nil {
		t.Fatalf("建草稿采购单 %s 失败: %v", code, err)
	}
	if res.Status != purchaseenums.PurchaseStatusDraft {
		t.Fatalf("草稿单的状态应为 draft，实际 %q", res.Status)
	}
	return res
}

func statusAction(t *testing.T, f *purchaseFixture, orderID string,
	action func(context.Context, *purchasedto.PurchaseStatusReq) (*purchasedto.PurchaseOrderResp, error)) (
	*purchasedto.PurchaseOrderResp, error) {
	t.Helper()
	return action(context.Background(), &purchasedto.PurchaseStatusReq{ProjectID: f.projectID, ID: orderID})
}

func TestPurchaseDraftLifecycle(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	order := createDraftOrder(t, f, "PO-DRAFT-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 5, 2)})

	_, err := receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "DRAFT-RECV-1")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 0 {
		t.Fatalf("被拒绝的收货不该改行数据，实际已收 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("被拒绝的收货不该动库存，实际 %d", got)
	}

	placed, err := statusAction(t, f, order.ID, f.purchase.PlacePurchaseOrder)
	if err != nil {
		t.Fatalf("草稿下单失败: %v", err)
	}
	if placed.Status != purchaseenums.PurchaseStatusOrdered {
		t.Fatalf("下单后状态应为 ordered，实际 %q", placed.Status)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusOrdered {
		t.Fatalf("下单后列里应为 ordered，实际 %q", got)
	}

	_, err = statusAction(t, f, order.ID, f.purchase.PlacePurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotPlaceable)

	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "DRAFT-RECV-2")
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("下单后收货库存应为 2，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("部分到货后状态应为 partial，实际 %q", got)
	}
}

func TestPurchaseExceptionPausesReceipt(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-EXC-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 2)})

	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 1, "EXC-RECV-1")
	marked, err := statusAction(t, f, order.ID, f.purchase.MarkPurchaseOrderException)
	if err != nil {
		t.Fatalf("标记异常失败: %v", err)
	}
	if marked.Status != purchaseenums.PurchaseStatusException {
		t.Fatalf("标记后状态应为 exception，实际 %q", marked.Status)
	}

	_, err = receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "EXC-RECV-2")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 1 {
		t.Fatalf("异常期间收货不该改行数据，实际已收 %d", got)
	}

	resumed, err := statusAction(t, f, order.ID, f.purchase.ResumePurchaseOrder)
	if err != nil {
		t.Fatalf("解除异常失败: %v", err)
	}
	if resumed.Status != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("解除异常后应按行推导为 partial，实际 %q", resumed.Status)
	}

	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "EXC-RECV-3")
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("收满后状态应为 received，实际 %q", got)
	}

	_, err = statusAction(t, f, order.ID, f.purchase.CancelPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotCancelable)

	markedFull, e := statusAction(t, f, order.ID, f.purchase.MarkPurchaseOrderException)
	if e != nil {
		t.Fatalf("已到货的单应允许标记异常: %v", e)
	}
	if markedFull.Status != purchaseenums.PurchaseStatusException {
		t.Fatalf("已到货标异常后状态应为 exception，实际 %q", markedFull.Status)
	}
	if _, e = statusAction(t, f, order.ID, f.purchase.CancelPurchaseOrder); e != nil {
		t.Fatalf("异常中的单应允许取消: %v", e)
	}
}

func TestPurchaseCancelMatrix(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	draft := createDraftOrder(t, f, "PO-CXL-DRAFT", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	got, err := statusAction(t, f, draft.ID, f.purchase.CancelPurchaseOrder)
	if err != nil {
		t.Fatalf("草稿取消应允许: %v", err)
	}
	if got.Status != purchaseenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应为 cancelled，实际 %q", got.Status)
	}

	ordered := mustPurchaseOrder(t, f, "PO-CXL-ORDERED", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	if _, err = statusAction(t, f, ordered.ID, f.purchase.CancelPurchaseOrder); err != nil {
		t.Fatalf("已下单取消应允许: %v", err)
	}

	partial := mustPurchaseOrder(t, f, "PO-CXL-PARTIAL", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	mustReceiveLine(t, f, partial.ID, partial.Lines[0].ID, 1, "CXL-PARTIAL-1")
	if _, err = statusAction(t, f, partial.ID, f.purchase.CancelPurchaseOrder); err != nil {
		t.Fatalf("部分到货取消应允许: %v", err)
	}

	full := mustPurchaseOrder(t, f, "PO-CXL-FULL", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 2, 1)})
	mustReceiveLine(t, f, full.ID, full.Lines[0].ID, 2, "CXL-FULL-1")
	_, err = statusAction(t, f, full.ID, f.purchase.CancelPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotCancelable)

	closed := mustPurchaseOrder(t, f, "PO-CXL-CLOSED", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 5, 1)})
	mustReceiveLine(t, f, closed.ID, closed.Lines[0].ID, 1, "CXL-CLOSED-1")
	finished, ferr := statusAction(t, f, closed.ID, f.purchase.FinishPurchaseOrder)
	if ferr != nil {
		t.Fatalf("部分到货提前关单应允许: %v", ferr)
	}
	if finished.Status != purchaseenums.PurchaseStatusFinished {
		t.Fatalf("关单后状态应为 finished，实际 %q", finished.Status)
	}
	_, err = statusAction(t, f, closed.ID, f.purchase.CancelPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotCancelable)

	_, err = receiveLine(t, f, closed.ID, closed.Lines[0].ID, 1, "CXL-CLOSED-2")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
}

func TestPurchaseFinishRequiresReceived(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	order := mustPurchaseOrder(t, f, "PO-FIN-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 1)})
	_, err := statusAction(t, f, order.ID, f.purchase.FinishPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotFinishable)

	draft := createDraftOrder(t, f, "PO-FIN-DRAFT", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 1)})
	_, err = statusAction(t, f, draft.ID, f.purchase.FinishPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotFinishable)
	_, err = statusAction(t, f, draft.ID, f.purchase.MarkPurchaseOrderException)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotExceptionable)

	_, err = statusAction(t, f, order.ID, f.purchase.ResumePurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotResumable)
}
