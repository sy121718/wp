package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	ordermodel "go_wp/internal/module/order/model"
	"go_wp/public/test/support"
)

func insertTestOrder(t *testing.T, db *gorm.DB, projectID, orderNo, status string, userID uint64, total int64, at time.Time) {
	t.Helper()
	err := db.Exec(
		`INSERT INTO orders (project_id, order_no, status, user_id, total, attribution, create_time, update_time)
		 VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, ?)`,
		projectID, orderNo, status, userID, total, at, at,
	).Error
	if err != nil {
		t.Fatalf("插入订单 %s 失败：%v", orderNo, err)
	}
}

func TestSummaryByRangeCountsAllStatusesButSummsOnlyPaidOnes(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "区间摘要用例")

	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	m := ordermodel.NewOrderModel(db)

	insertTestOrder(t, db, projectID, "GORM-RANGE-1", "paid", 101, 5000, from.Add(time.Hour))
	insertTestOrder(t, db, projectID, "GORM-RANGE-2", "shipped", 102, 3000, from.Add(2*time.Hour))

	insertTestOrder(t, db, projectID, "GORM-RANGE-3", "cancelled", 103, 9999, from.Add(3*time.Hour))

	insertTestOrder(t, db, projectID, "GORM-RANGE-4", "paid", 104, 7777, to.Add(time.Hour))

	row, err := m.SummaryByRange(context.Background(), projectID, from, to)
	if err != nil {
		t.Fatalf("SummaryByRange 失败：%v", err)
	}
	if row.OrderCount != 3 {
		t.Errorf("OrderCount = %d，期望 3（含取消单）", row.OrderCount)
	}
	if row.PaidOrderCount != 2 {
		t.Errorf("PaidOrderCount = %d，期望 2（paid + shipped）", row.PaidOrderCount)
	}
	if row.NetSales != 8000 {
		t.Errorf("NetSales = %d，期望 8000（5000+3000，取消单不计）", row.NetSales)
	}

	if _, err := m.SummaryByRange(context.Background(), "", from, to); err == nil {
		t.Error("空 projectID 应当报 ErrProjectRequired")
	}

	if _, err := m.SummaryByRange(context.Background(), projectID, time.Time{}, to); err == nil {
		t.Error("零值 from 应当报 ErrRangeRequired")
	}
}

func TestSummaryByUserWindowAggregatesWholeSetAndKeepsZeroRow(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "客户摘要用例")
	m := ordermodel.NewOrderModel(db)

	base := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	insertTestOrder(t, db, projectID, "GORM-USER-1", "paid", 201, 1200, base)
	insertTestOrder(t, db, projectID, "GORM-USER-2", "completed", 201, 3400, base.Add(time.Hour))
	insertTestOrder(t, db, projectID, "GORM-USER-3", "cancelled", 201, 8888, base.Add(2*time.Hour))
	insertTestOrder(t, db, projectID, "GORM-USER-4", "paid", 202, 500, base.Add(3*time.Hour))

	row, err := m.SummaryByUser(context.Background(), projectID, 201)
	if err != nil {
		t.Fatalf("SummaryByUser 失败：%v", err)
	}
	if row.OrderCount != 3 {
		t.Errorf("OrderCount = %d，期望 3", row.OrderCount)
	}
	if row.PaidOrderCount != 2 {
		t.Errorf("PaidOrderCount = %d，期望 2", row.PaidOrderCount)
	}
	if row.TotalAmount != 4600 {
		t.Errorf("TotalAmount = %d，期望 4600（1200+3400）", row.TotalAmount)
	}

	if row.LastOrderNo == nil || *row.LastOrderNo != "GORM-USER-3" {
		t.Errorf("LastOrderNo = %v，期望 GORM-USER-3", row.LastOrderNo)
	}
	if row.LastOrderStatus == nil || *row.LastOrderStatus != "cancelled" {
		t.Errorf("LastOrderStatus = %v，期望 cancelled", row.LastOrderStatus)
	}

	empty, err := m.SummaryByUser(context.Background(), projectID, 999)
	if err != nil {
		t.Fatalf("零订单客户不该报错：%v", err)
	}
	if empty.OrderCount != 0 || empty.TotalAmount != 0 {
		t.Errorf("零订单客户应全 0，得到 %+v", empty)
	}
	if empty.LastOrderID != nil {
		t.Errorf("零订单客户不该有最近一单，得到 %v", *empty.LastOrderID)
	}
}

func TestHasPurchasedProductScopesByProjectAndIgnoresDuplicates(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	mine := uuid.NewString()
	other := uuid.NewString()
	support.SeedProjectRow(t, db, mine, "买过-本工程")
	support.SeedProjectRow(t, db, other, "买过-别工程")

	productID := uuid.NewString()
	now := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	m := ordermodel.NewOrderModel(db)

	insertTestOrder(t, db, mine, "GORM-BUY-1", "paid", 301, 100, now)

	insertTestOrder(t, db, mine, "GORM-BUY-2", "shipped", 301, 100, now.Add(time.Hour))

	insertTestOrder(t, db, mine, "GORM-BUY-3", "cancelled", 301, 100, now.Add(2*time.Hour))

	insertTestOrder(t, db, other, "GORM-BUY-4", "paid", 301, 100, now.Add(3*time.Hour))

	details := []struct {
		orderNo string
		status  string
	}{
		{"GORM-BUY-1", "paid"},
		{"GORM-BUY-2", "shipped"},
		{"GORM-BUY-3", "cancelled"},
		{"GORM-BUY-4", "paid"},
	}
	for _, d := range details {
		var orderID uint64
		if err := db.Raw(`SELECT id FROM orders WHERE order_no = ?`, d.orderNo).Scan(&orderID).Error; err != nil {
			t.Fatalf("取订单 id 失败：%v", err)
		}
		err := db.Exec(
			`INSERT INTO order_items (order_id, product_id, variant_id, product_name, sku, unit_price, quantity, line_total, create_time)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			orderID, productID, uuid.NewString(), "测试商品", "GORM-SKU", 100, 1, 100, now,
		).Error
		if err != nil {
			t.Fatalf("插入明细失败：%v", err)
		}
	}

	purchased, err := m.HasPurchasedProduct(context.Background(), mine, 301, productID)
	if err != nil {
		t.Fatalf("HasPurchasedProduct 失败：%v", err)
	}
	if !purchased {
		t.Error("本工程有已付单包含该商品，应判为买过")
	}

	notBought, err := m.HasPurchasedProduct(context.Background(), mine, 302, productID)
	if err != nil {
		t.Fatalf("HasPurchasedProduct 失败：%v", err)
	}
	if notBought {
		t.Error("该用户在本工程没有已付单，不该判为买过")
	}
}
