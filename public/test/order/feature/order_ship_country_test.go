package feature

// order_ship_country_test.go — 订单地址「国家/地区」的落库链路（迁移 501）。
//
// 覆盖三件事，每一件都会**静默**出错：
//   · 列在不在（迁移没生效时，model 里多一列只在真实写库那一刻才炸）；
//   · 建单映射有没有把请求里的国家带进快照（漏了就是「访客填了、没地方承接」）；
//   · 没收集时落的是空串而不是别的哨兵值 —— 列是 VARCHAR(2)，"N/A" 这类占位值装不进去，
//     而空串正是 135 那一批地址列的既有口径（「空 = 未收集」，不是 NULL）。

import (
	"context"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/public/test/support"
)

// TestOrderShipCountryColumnsFromMigration 两个国家列由生产迁移建出来，不是测试自建表。
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

// TestOrderShipCountrySnapshot 建单把收货 / 账单国家写进订单快照，并可原样回读。
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

// TestOrderShipCountryUncollectedIsEmpty 没收集国家时落空串（非中国站点与存量数据的合法状态）。
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
