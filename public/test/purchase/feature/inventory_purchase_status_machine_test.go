// Package feature 采购单状态机（迁移 670）的七态与动作矩阵。
//
// 状态是**推导值**（已收到多少），人工动作只负责在推导射程之外落一次决定：
//
//	draft → place → ordered
//	ordered / partial / received → exception → resume → 推导向
//	received / partial → finish → finished
//	draft / ordered / partial / exception → cancel → cancelled
//
// 这里逐条钉住「谁能进、谁不能进、错误是什么」，因为前置状态写错的表现不是报错，
// 而是脏数据：已到货的单被取消（货在仓库里、单据说没这回事）、草稿单被收货。
package feature

import (
	"context"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

// createDraftOrder 建一张草稿采购单（AsDraft=true），并要求状态真的是 draft。
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

// statusAction 对一张采购单做一个状态动作（返回原始错误，供拒绝路径断言）。
func statusAction(t *testing.T, f *purchaseFixture, orderID string,
	action func(context.Context, *purchasedto.PurchaseStatusReq) (*purchasedto.PurchaseOrderResp, error)) (
	*purchasedto.PurchaseOrderResp, error) {
	t.Helper()
	return action(context.Background(), &purchasedto.PurchaseStatusReq{ProjectID: f.projectID, ID: orderID})
}

// TestPurchaseDraftLifecycle 草稿不可收货，下单后可收货。
//
// 草稿的语义是「还没正式下单，没人按它备货」—— 它必须同时满足两件事：
// 收货入口拒绝（否则「草稿」只是备注）、下单是唯一出口。
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

	// ① 草稿收货被拒（状态守卫先于任何行写入）。
	_, err := receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "DRAFT-RECV-1")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 0 {
		t.Fatalf("被拒绝的收货不该改行数据，实际已收 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("被拒绝的收货不该动库存，实际 %d", got)
	}

	// ② 下单：draft → ordered，状态从此交给明细行推导。
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

	// ③ 重复下单：只有草稿能下单，已下单再下单不改变任何东西但必须报错。
	_, err = statusAction(t, f, order.ID, f.purchase.PlacePurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotPlaceable)

	// ④ 下单后可收（部分到货 → partial）。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "DRAFT-RECV-2")
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("下单后收货库存应为 2，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("部分到货后状态应为 partial，实际 %q", got)
	}
}

// TestPurchaseExceptionPausesReceipt 异常态暂停收货，解除后恢复并回到推导向。
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

	// 先收 1 件 → partial，再标异常。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 1, "EXC-RECV-1")
	marked, err := statusAction(t, f, order.ID, f.purchase.MarkPurchaseOrderException)
	if err != nil {
		t.Fatalf("标记异常失败: %v", err)
	}
	if marked.Status != purchaseenums.PurchaseStatusException {
		t.Fatalf("标记后状态应为 exception，实际 %q", marked.Status)
	}

	// 异常中收货被拒（这是「暂停」的全部意义：它必须挡在服务层的唯一入口上）。
	_, err = receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "EXC-RECV-2")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 1 {
		t.Fatalf("异常期间收货不该改行数据，实际已收 %d", got)
	}

	// 解除异常：目标态按明细行重算 —— 已收 1 / 订 4，所以回到 partial 而不是 ordered。
	resumed, err := statusAction(t, f, order.ID, f.purchase.ResumePurchaseOrder)
	if err != nil {
		t.Fatalf("解除异常失败: %v", err)
	}
	if resumed.Status != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("解除异常后应按行推导为 partial，实际 %q", resumed.Status)
	}

	// 恢复后可继续收。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "EXC-RECV-3")
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("收满后状态应为 received，实际 %q", got)
	}

	// 已到货不可取消 —— 货已经进仓，要退只能走退货。
	_, err = statusAction(t, f, order.ID, f.purchase.CancelPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotCancelable)

	// 但已到货仍可标异常（货不对版要暂停后续处理）；异常态本身是可取消的
	// （assertPurchaseTransition 的 cancelled 前置含 exception：问题无法解决时得能收尾）。
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

// TestPurchaseCancelMatrix 取消的前置状态矩阵：草稿 / 已下单 / 部分到货 / 异常可取消，
// 已到货与已完结不可取消（货已进仓或单据已收尾）。
func TestPurchaseCancelMatrix(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 草稿可取消。
	draft := createDraftOrder(t, f, "PO-CXL-DRAFT", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	got, err := statusAction(t, f, draft.ID, f.purchase.CancelPurchaseOrder)
	if err != nil {
		t.Fatalf("草稿取消应允许: %v", err)
	}
	if got.Status != purchaseenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应为 cancelled，实际 %q", got.Status)
	}

	// 已下单可取消。
	ordered := mustPurchaseOrder(t, f, "PO-CXL-ORDERED", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	if _, err = statusAction(t, f, ordered.ID, f.purchase.CancelPurchaseOrder); err != nil {
		t.Fatalf("已下单取消应允许: %v", err)
	}

	// 部分到货可取消（剩下的不再等）。
	partial := mustPurchaseOrder(t, f, "PO-CXL-PARTIAL", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 1)})
	mustReceiveLine(t, f, partial.ID, partial.Lines[0].ID, 1, "CXL-PARTIAL-1")
	if _, err = statusAction(t, f, partial.ID, f.purchase.CancelPurchaseOrder); err != nil {
		t.Fatalf("部分到货取消应允许: %v", err)
	}

	// 已到货不可取消。
	full := mustPurchaseOrder(t, f, "PO-CXL-FULL", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 2, 1)})
	mustReceiveLine(t, f, full.ID, full.Lines[0].ID, 2, "CXL-FULL-1")
	_, err = statusAction(t, f, full.ID, f.purchase.CancelPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotCancelable)

	// 已完结不可取消：先提前关单（部分到货）再取消。
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
	// 已完结也不再受理收货：剩下的货不再等。
	_, err = receiveLine(t, f, closed.ID, closed.Lines[0].ID, 1, "CXL-CLOSED-2")
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotReceivable)
}

// TestPurchaseFinishRequiresReceived 提前关单只在「已经到过货」时有意义。
func TestPurchaseFinishRequiresReceived(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 已下单但一件没到：关单没有意义（要收尾应该是取消）。
	order := mustPurchaseOrder(t, f, "PO-FIN-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 1)})
	_, err := statusAction(t, f, order.ID, f.purchase.FinishPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotFinishable)

	// 草稿同样不可关单，也不可标异常（还没下单，谈不上「货出问题」）。
	draft := createDraftOrder(t, f, "PO-FIN-DRAFT", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 1)})
	_, err = statusAction(t, f, draft.ID, f.purchase.FinishPurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotFinishable)
	_, err = statusAction(t, f, draft.ID, f.purchase.MarkPurchaseOrderException)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotExceptionable)

	// 非异常态不可「解除异常」。
	_, err = statusAction(t, f, order.ID, f.purchase.ResumePurchaseOrder)
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusNotResumable)
}
