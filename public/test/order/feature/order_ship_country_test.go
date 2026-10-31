package feature

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/public/test/support"
)

func TestOrderShipCountryColumnsFromMigration(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	cols := support.DDLColumns(t, f.db, "orders")
	for _, name := range []string{"ship_country", "bill_country"} {
		if !cols[name] {
			t.Fatalf("orders.%s 不存在：迁移 501 没有在真实迁移链上生效", name)
		}
	}
}

func TestOrderShipCountrySnapshot(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "国家快照", 20, 3)
	ctx := context.Background()

	req := f.createBaseReq(vid, 1)
	req.Shipping.Country = "CN"
	req.Billing.Country = "US"
	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head.ShipCountry != "CN" || detail.Head.BillCountry != "US" {
		t.Fatalf("国家快照应为 CN / US，实际 %q / %q", detail.Head.ShipCountry, detail.Head.BillCountry)
	}
}

func TestOrderShipCountryUncollectedIsEmpty(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "无国家", 20, 3)
	ctx := context.Background()

	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head.ShipCountry != "" || detail.Head.BillCountry != "" {
		t.Fatalf("未收集时国家应为空串，实际 %q / %q", detail.Head.ShipCountry, detail.Head.BillCountry)
	}
}
