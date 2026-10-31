package feature

import (
	"context"
	"strings"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

func TestCurrencyRateUpsertIsOverwrite(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.10",
	}); err != nil {
		t.Fatalf("首次写汇率失败: %v", err)
	}
	second, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "usd", QuoteCode: "cny", Rate: "7.24",
	})
	if err != nil {
		t.Fatalf("第二次写汇率失败: %v", err)
	}
	if second.Rate != "7.24" {
		t.Fatalf("覆盖式写之后应是后写的 7.24，实际 %q", second.Rate)
	}

	var n int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM currency_rates WHERE upper(base_code) = 'USD' AND upper(quote_code) = 'CNY'").
		Scan(&n).Error; qerr != nil {
		t.Fatalf("查汇率行数失败: %v", qerr)
	}
	if n != 1 {
		t.Fatalf("同一币对只该有 1 行，实际 %d 行", n)
	}

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "0",
	}); err == nil {
		t.Fatalf("汇率为 0 应被拒")
	}
	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "-1",
	}); err == nil {
		t.Fatalf("汇率为负数应被拒")
	}
}

func TestOrderFXSnapshotIsImmutable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "汇率商品", 100, 10)

	noRate, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("无汇率下单失败: %v", err)
	}
	var fxRate *string
	if qerr := f.db.Raw("SELECT CAST(fx_rate AS text) FROM orders WHERE id = ?", noRate.ID).
		Row().Scan(&fxRate); qerr != nil {
		t.Fatalf("读订单汇率失败: %v", qerr)
	}
	if fxRate != nil {
		t.Fatalf("没有配汇率时 fx_rate 应为 NULL，实际 %q", *fxRate)
	}

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.20",
	}); err != nil {
		t.Fatalf("写汇率失败: %v", err)
	}

	req := f.createBaseReq(vid, 1)
	req.Currency = "USD"
	snapshotOrder, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	var snapRate *string
	if qerr := f.db.Raw("SELECT CAST(fx_rate AS text) FROM orders WHERE id = ?", snapshotOrder.ID).
		Row().Scan(&snapRate); qerr != nil {
		t.Fatalf("读订单汇率快照失败: %v", qerr)
	}
	if snapRate == nil {
		t.Fatalf("请求带 currency=USD 且已有 USD→CNY 汇率时，下单应写入 fx_rate 快照（不能恒 NULL）")
	}
	var snapBase string
	if qerr := f.db.Raw("SELECT fx_base_code FROM orders WHERE id = ?", snapshotOrder.ID).
		Row().Scan(&snapBase); qerr != nil {
		t.Fatalf("读订单基准币失败: %v", qerr)
	}
	if snapBase != "CNY" {
		t.Fatalf("快照的基准币应为 CNY，实际 %q", snapBase)
	}

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "9.99",
	}); err != nil {
		t.Fatalf("改汇率失败: %v", err)
	}
	var afterRate *string
	if qerr := f.db.Raw("SELECT CAST(fx_rate AS text) FROM orders WHERE id = ?", snapshotOrder.ID).
		Row().Scan(&afterRate); qerr != nil {
		t.Fatalf("读订单汇率失败: %v", qerr)
	}
	if afterRate == nil || !strings.HasPrefix(*afterRate, "7.2") {
		t.Fatalf("订单快照不该随汇率表变化（应仍是 7.20），实际 %v", afterRate)
	}
}

func TestConvertedOverview(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "折算商品", 100, 20)

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.20",
	}); err != nil {
		t.Fatalf("写汇率失败: %v", err)
	}

	fxReq := f.createBaseReq(vid, 1)
	fxReq.Currency = "USD"
	withFX, err := f.orders.CreateOrder(ctx, fxReq)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, withFX.ID)

	if err := f.db.Exec("UPDATE orders SET total = 10000, subtotal = 10000 WHERE id = ?", withFX.ID).
		Error; err != nil {
		t.Fatalf("改订单金额失败: %v", err)
	}
	noFX, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建第二单失败: %v", err)
	}
	f.payOrder(t, noFX.ID)

	day := func(offset int) string {
		return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
	}
	overview, err := f.orders.SalesOverview(ctx, &orderdto.SalesOverviewReq{
		ProjectID: f.projectID, From: day(-30), To: day(0), Status: "paid",
	})
	if err != nil {
		t.Fatalf("读销售概览失败: %v", err)
	}
	if overview.Converted == nil {
		t.Fatalf("有汇率快照时应给出折算块")
	}

	if overview.Converted.ConvertedSales != 72000 {
		t.Fatalf("折算销售额应为 10000×7.20=72000 分，实际 %d", overview.Converted.ConvertedSales)
	}
	if overview.Converted.ConvertedOrders != 1 {
		t.Fatalf("已折算订单数应为 1，实际 %d", overview.Converted.ConvertedOrders)
	}
	if overview.Converted.UnconvertedOrders != 1 {
		t.Fatalf("未固化订单数应为 1，实际 %d", overview.Converted.UnconvertedOrders)
	}
}

func TestConvertedOverviewGroupsByBaseCode(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "折算分组商品", 100, 10)

	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.20",
	}); err != nil {
		t.Fatalf("写汇率失败: %v", err)
	}

	fxReq := f.createBaseReq(vid, 1)
	fxReq.Currency = "USD"
	cur, err := f.orders.CreateOrder(ctx, fxReq)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, cur.ID)
	if err := f.db.Exec("UPDATE orders SET total = 10000, subtotal = 10000 WHERE id = ?", cur.ID).
		Error; err != nil {
		t.Fatalf("改订单金额失败: %v", err)
	}

	old, err := f.orders.CreateOrder(ctx, fxReq)
	if err != nil {
		t.Fatalf("建第二单失败: %v", err)
	}
	f.payOrder(t, old.ID)
	if err := f.db.Exec("UPDATE orders SET total = 10000, subtotal = 10000 WHERE id = ?", old.ID).
		Error; err != nil {
		t.Fatalf("改第二单金额失败: %v", err)
	}
	if err := f.db.Exec("UPDATE orders SET fx_base_code = 'USD' WHERE id = ?", old.ID).Error; err != nil {
		t.Fatalf("改基准币失败: %v", err)
	}
	broken, err := f.orders.CreateOrder(ctx, fxReq)
	if err != nil {
		t.Fatalf("建第三单失败: %v", err)
	}
	f.payOrder(t, broken.ID)

	if err := f.db.Exec("UPDATE orders SET fx_base_code = '' WHERE id = ?", broken.ID).Error; err != nil {
		t.Fatalf("清基准币失败: %v", err)
	}

	day := func(offset int) string {
		return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
	}
	req := &orderdto.SalesOverviewReq{
		ProjectID: f.projectID, From: day(-30), To: day(0), Status: "paid",
	}
	overview, err := f.orders.SalesOverview(ctx, req)
	if err != nil {
		t.Fatalf("读销售概览失败: %v", err)
	}
	if overview.Converted == nil {
		t.Fatalf("有汇率快照时应给出折算块")
	}

	if overview.Converted.ConvertedSales != 72000 {
		t.Fatalf("折算销售额只该含当前基准币的快照 = 72000 分，实际 %d"+
			"（基准币不同的旧快照被算进来了，页面标签会变成假话）",
			overview.Converted.ConvertedSales)
	}
	if overview.Converted.ConvertedOrders != 1 {
		t.Fatalf("已折算订单数应为 1，实际 %d", overview.Converted.ConvertedOrders)
	}

	if overview.Converted.UnconvertedOrders != 2 {
		t.Fatalf("未折算订单数应为 2，实际 %d", overview.Converted.UnconvertedOrders)
	}

	if got, want := overview.Converted.ConvertedOrders+overview.Converted.UnconvertedOrders,
		int64(3); got != want {
		t.Fatalf("已折算 + 未折算应等于区间订单总数 %d，实际 %d", want, got)
	}
}
