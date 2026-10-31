package feature

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

func TestVisitorOrdersScopedToOwnUser(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "帆布袋", 60.00, 10)
	ctx := context.Background()

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

	detailA, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderA.ID})
	if err != nil || detailA.Head.UserID == nil {
		t.Fatalf("取 A 的订单失败或未开号: %v / %+v", err, detailA)
	}
	uidA := *detailA.Head.UserID
	detailB, _ := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderB.ID})
	if detailB == nil || detailB.Head.UserID == nil {
		t.Fatal("B 的订单未开号")
	}
	uidB := *detailB.Head.UserID
	if uidA == uidB {
		t.Fatalf("两个邮箱应各自开号，实际都落到 %d", uidA)
	}

	listA, err := f.orders.ListVisitorOrders(ctx, &orderdto.VisitorOrderListReq{
		ProjectID: f.projectID, UserID: uidA,
	})
	if err != nil {
		t.Fatalf("A 查列表失败: %v", err)
	}
	if listA.Total != 1 || len(listA.List) != 1 || listA.List[0].ID != orderA.ID {
		t.Fatalf("A 只应看到自己的 1 单，实际 %+v", listA)
	}

	_, err = f.orders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{
		OrderID: orderB.ID, ProjectID: f.projectID, UserID: uidA,
	})
	if err == nil {
		t.Fatal("访客取他人订单详情必须失败")
	}
	if !strings.Contains(err.Error(), orderenums.ErrOrderNotFound) {
		t.Fatalf("越权应表现为「订单不存在」，实际: %v", err)
	}

	got, err := f.orders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{
		OrderID: orderA.ID, ProjectID: f.projectID, UserID: uidA,
	})
	if err != nil || got == nil || got.Head == nil || len(got.Items) != 1 {
		t.Fatalf("取自己的订单失败: %v / %+v", err, got)
	}
}

func TestGetOrderCrossProject(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "跨工程隔离", 88.00, 5)
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	otherProject := "00000000-0000-4000-8000-000000000099"
	_, err = f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: otherProject, OrderID: res.ID})
	if err == nil || !strings.Contains(err.Error(), orderenums.ErrOrderNotFound) {
		t.Fatalf("跨工程访问应返回 %q: %v", orderenums.ErrOrderNotFound, err)
	}
	_, err = f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("同工程应能取到订单: %v", err)
	}
}

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
