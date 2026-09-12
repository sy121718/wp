package feature

// order_visitor_test.go — 访客自助查询的**归属**测试（BIZ-1 访问面）。
//
// 这一组测试守的是一条不能出错的性质：访客只能看到自己的订单。
// 它是「片段层把身份传下来」与「订单层按身份收口」两条链路的交界处，
// 任何一侧松动都会变成「改一个 id 就能看别人的订单」——那种缺陷不会有任何报错。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

// TestVisitorOrdersScopedToOwnUser 访客查询只返回自己名下的订单。
func TestVisitorOrdersScopedToOwnUser(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "帆布袋", 60.00, 10)
	ctx := context.Background()

	// 两个不同邮箱下单 → 订单域会各自开一个访客账号（迁移 137 的初始密码邮件模板）。
	reqA := f.createBaseReq(vid, 1)
	reqA.CustomerEmail = "a@example.com"
	orderA, err := f.orders.CreateOrder(ctx, reqA)
	if err != nil {
		t.Fatalf("A 建单失败: %v", err)
	}
	reqB := f.createBaseReq(vid, 1)
	reqB.CustomerEmail = "b@example.com"
	orderB, err := f.orders.CreateOrder(ctx, reqB)
	if err != nil {
		t.Fatalf("B 建单失败: %v", err)
	}

	detailA, err := f.orders.GetOrder(ctx, orderA.ID)
	if err != nil || detailA.Head.UserID == nil {
		t.Fatalf("取 A 的订单失败或未开号: %v / %+v", err, detailA)
	}
	uidA := *detailA.Head.UserID
	detailB, _ := f.orders.GetOrder(ctx, orderB.ID)
	if detailB == nil || detailB.Head.UserID == nil {
		t.Fatal("B 的订单未开号")
	}
	uidB := *detailB.Head.UserID
	if uidA == uidB {
		t.Fatalf("两个邮箱应各自开号，实际都落到 %d", uidA)
	}

	// A 的列表里只有 A 的单，且**不含**状态计数（那是全站口径）。
	listA, err := f.orders.ListVisitorOrders(ctx, &orderdto.VisitorOrderListReq{
		ProjectID: f.projectID, UserID: uidA,
	})
	if err != nil {
		t.Fatalf("A 查列表失败: %v", err)
	}
	if listA.Total != 1 || len(listA.List) != 1 || listA.List[0].ID != orderA.ID {
		t.Fatalf("A 只应看到自己的 1 单，实际 %+v", listA)
	}

	// A 用 B 的订单 id 取详情：必须失败，而且失败原因与「不存在」**完全相同** ——
	// 区分开来就是一个订单号探测器。
	_, err = f.orders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{
		OrderID: orderB.ID, ProjectID: f.projectID, UserID: uidA,
	})
	if err == nil {
		t.Fatal("访客取他人订单详情必须失败")
	}
	if !strings.Contains(err.Error(), orderenums.ErrOrderNotFound) {
		t.Fatalf("越权应表现为「订单不存在」，实际: %v", err)
	}

	// 自己的那单能取到，且带订单项。
	got, err := f.orders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{
		OrderID: orderA.ID, ProjectID: f.projectID, UserID: uidA,
	})
	if err != nil || got == nil || got.Head == nil || len(got.Items) != 1 {
		t.Fatalf("取自己的订单失败: %v / %+v", err, got)
	}
}

// TestVisitorOrdersRequireUserID 没有身份时**报错**而不是「不过滤」。
//
// 少传一次归属条件就等于把全站订单列表发给某个访客 ——
// 所以 UserID 是必填的，不能有「缺省 = 全部」这种便利。
func TestVisitorOrdersRequireUserID(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.orders.ListVisitorOrders(ctx, &orderdto.VisitorOrderListReq{ProjectID: f.projectID}); err == nil {
		t.Fatal("缺少 userID 的访客列表查询必须报错")
	}
	if _, err := f.orders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{OrderID: 1, ProjectID: f.projectID}); err == nil {
		t.Fatal("缺少 userID 的访客详情查询必须报错")
	}
	if _, err := f.orders.ListVisitorOrders(ctx, &orderdto.VisitorOrderListReq{UserID: 7}); err == nil {
		t.Fatal("缺少工程 id 的访客列表查询必须报错")
	}
}
