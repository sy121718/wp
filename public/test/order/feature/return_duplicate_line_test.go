package feature

// return_duplicate_line_test.go — 退货申请不得被「重复明细」放大（B04 / DATA-06）。
//
// 缺陷形态：创建退货时逐行比对 `line.Quantity > returnable[orderItemID]`，
// 但**没有把同一 orderItem 的多行累加**。于是同一 orderItemID 提交两行、每行都不超过
// 可退数量时，整批通过 —— 退款数量与回仓数量一起翻倍。
//
// 这是「校验和累加用了两个不同口径」的典型：校验看单行，落库看总和。
// 判据必须断在**服务端拒绝**上，而不是断界面。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/facing"
)

// TestCreateReturnRejectsDuplicateLinesForSameItem 同一 orderItem 的多行合计超过可退数量必须被拒。
func TestCreateReturnRejectsDuplicateLinesForSameItem(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "退货重复行商品", 30, 20)

	// 建一个访客账号并挂到订单上：退货申请要求「订单归属 = 申请者」
	// （order.UserID 为空时一律 ErrOrderNotFound，见 RequestReturn 的归属校验）。
	const buyerID uint64 = 77001
	var realUserID uint64
	if err := f.db.Raw(`INSERT INTO users (username, password, email, status, create_time, update_time)
		VALUES (?, '', ?, 1, now(), now()) RETURNING id`,
		"returnbuyer", "returnbuyer@example.test").Scan(&realUserID).Error; err != nil {
		t.Fatalf("造买家账号失败: %v", err)
	}
	base := f.createBaseReq(vid, 2)
	base.UserID = &realUserID
	created, err := f.orders.CreateOrder(ctx, base)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// 直接查表拿明细行 id：order service 没有公开的详情读取口，
	// 而本用例要的只是"一个真实的 orderItem id"，不必为此绕一层。
	var itemID uint64
	if err := f.db.Raw(`SELECT id FROM order_items WHERE order_id = ? ORDER BY id LIMIT 1`,
		created.ID).Scan(&itemID).Error; err != nil {
		t.Fatalf("取订单明细失败: %v", err)
	}
	if itemID == 0 {
		t.Fatal("订单没有明细行")
	}
	// 只有 paid / shipped / completed 可申请退货（pending 没付钱，没有可退的钱）。
	if cerr := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: created.ID, ToStatus: ordermodel.OrderStatusPaid, OperatorName: "tester",
	}); cerr != nil {
		t.Fatalf("订单转 paid 失败: %v", cerr)
	}

	// 两行各 2 件、指向同一个 orderItem：单行都 ≤ 可退(2)，合计 4 > 2。
	_, rerr := f.orders.RequestReturn(ctx, &orderdto.ReturnRequestReq{
		ProjectID: f.projectID,
		OrderID:   created.ID,
		UserID:    realUserID,
		Reason:    "重复行放大测试",
		Items: []orderdto.ReturnItemReq{
			{OrderItemID: itemID, Quantity: 2},
			{OrderItemID: itemID, Quantity: 2},
		},
	})
	if rerr == nil {
		t.Fatal("同一 orderItem 的两行合计 4 件（可退只有 2 件）却被接受了 —— 退款会被放大一倍")
	}
	// 这条错误是复合形态（`key：名称`，全角冒号）。
	//
	// 用 FactOf 而不是 IsBusinessError：后者只认单段 key（pkg/facing 的注释写明
	// "不把复合形态放宽进 IsBusinessError"），而出口展示走的正是 FactOf。
	// 这也顺带钉住了一个真实缺陷：FactOf 原先只认 ASCII 冒号，而 service 侧
	// 写复合错误的既有形态是全角 —— 那种情况下用户看到的是 500「系统内部错误」。
	fact, ok := facing.FactOf(rerr)
	if !ok || !fact.Business {
		t.Fatalf("应识别为业务错误（出口据此给可行动文案，而不是 500），实际 ok=%v err=%v", ok, rerr)
	}
	if fact.Key != orderenums.ErrReturnQuantityExceeded {
		t.Fatalf("应报数量超限（%s），实际 key=%q", orderenums.ErrReturnQuantityExceeded, fact.Key)
	}
	if !strings.Contains(fact.Tail, "退货重复行商品") {
		t.Fatalf("明细应带上商品名（用户据此知道是哪一件），实际 %q", fact.Tail)
	}

	// 反证：合法的一行 2 件仍然能通过（证明上面的拒绝不是"全都拒"）。
	if _, ok := f.orders.RequestReturn(ctx, &orderdto.ReturnRequestReq{
		ProjectID: f.projectID,
		OrderID:   created.ID,
		UserID:    realUserID,
		Reason:    "合法申请",
		Items:     []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}},
	}); ok != nil {
		t.Fatalf("合法退货申请应通过，实际 %v", ok)
	}
}
