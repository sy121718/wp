// currency_rate_test.go — W5（迁移 633）的验收：多币种汇率快照 + 折算。
//
// 覆盖任务书 §11.3 的会红断言：
//
//	1  同币对写两次 → 表里仍 1 行且 rate 是后写的值
//	2  rate = 0 → 拒绝
//	3  下单时无汇率 → orders.fx_rate IS NULL，UnconvertedOrders + 1
//	4  有汇率下单 → 快照值写入；随后改汇率表 → 该订单折算金额不变
//	5  ConvertedSales = Σ(total × fx_rate)（断言等值）
package feature

import (
	"context"
	"strings"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

// TestCurrencyRateUpsertIsOverwrite 验收 1 / 2：覆盖式写 + 拒绝 0。
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
	// 表里仍 1 行（大小写不同的币种代码是同一个币对）。
	var n int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM currency_rates WHERE upper(base_code) = 'USD' AND upper(quote_code) = 'CNY'").
		Scan(&n).Error; qerr != nil {
		t.Fatalf("查汇率行数失败: %v", qerr)
	}
	if n != 1 {
		t.Fatalf("同一币对只该有 1 行，实际 %d 行", n)
	}

	// rate = 0 被拒（DDL 的 CHECK 也拦得住，但 service 先给可读错误）。
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

// TestOrderFXSnapshotIsImmutable 验收 3 / 4：下单快照不随汇率表变化。
func TestOrderFXSnapshotIsImmutable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "汇率商品", 100, 10)

	// ③ 无汇率时下单：fx_rate 留 NULL。
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

	// ④ 配上汇率后下单：快照写入；随后改汇率表，该订单的折算金额不变。
	// 注意：建单时的币种是本工程的默认币种（i18n 的默认货币），快照只在
	// 「订单币 ≠ 基准币」时才写 —— 所以这里直接断言「写快照 API 是覆盖式」与
	// 「折算用的是订单上的快照」两件事，用一个手工设过 fx_rate 的订单验证。
	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.20",
	}); err != nil {
		t.Fatalf("写汇率失败: %v", err)
	}
	// 走**真实建单链路**：请求显式给币种 USD，快照应由 orderFXSnapshot 自动写入。
	// （复审 C：原先靠手工 UPDATE 模拟，掩盖了「两个实参同源导致 fx_rate 恒 NULL」的缺陷。）
	req := f.createBaseReq(vid, 1)
	req.Currency = "USD"
	snapshotOrder, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// ③b 建单就该落下快照：这是 C 那条缺陷的正面断言。
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
	// 改汇率表：快照**不该**跟着变。
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

// TestConvertedOverview 验收 3 / 5：折算金额与未固化订单数。
func TestConvertedOverview(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "折算商品", 100, 20)

	// **先配汇率再建单**：没配汇率时下单不写快照（那是「未固化」这条合法状态）。
	if _, err := f.orders.SetRate(ctx, &orderdto.SetRateReq{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.20",
	}); err != nil {
		t.Fatalf("写汇率失败: %v", err)
	}
	// 一张有快照的单（USD 单，建单链路自动写入 7.20）与一张没有快照的单。
	fxReq := f.createBaseReq(vid, 1)
	fxReq.Currency = "USD"
	withFX, err := f.orders.CreateOrder(ctx, fxReq)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, withFX.ID)
	// 金额改大以便断言（汇率快照仍由建单链路写入，不手工伪造 fx_rate）。
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
	// ⑤ ConvertedSales = Σ(total × fx_rate) = 10000 × 7.20 = 72000 分。
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

// TestConvertedOverviewGroupsByBaseCode 折算块只数「快照基准币 = 当前基准币」的订单。
//
// 快照汇率的语义是「1 单位订单币 = rate 单位**当时的**基准币」。基准币是运行期配置，
// 可以改；改过之后，旧快照折算到的是另一个币。把两种基准币的折算值直接相加，
// 数字算得出来、标签「折算到 <当前基准币>」却是假的。
//
// 会红断言：
//
//	1  基准币对不上的快照订单不进 ConvertedSales（金额为 0，不给假数字）
//	2  它计入 UnconvertedOrders（「无法折算到当前基准币」）
//	3  Converted + Unconverted == 区间内订单总数（两侧互补，页面不出现对不上的缺口）
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
	// 当前基准币（i18n 默认货币）下的快照单：正常折算。
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

	// 旧基准币下的快照单：把 fx_base_code 改成 USD，等价于「基准币配置从 USD 改成 CNY 之前
	// 固化的快照」。第三单把基准币清成空串（未固化的正式表示），一并覆盖。
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
	// fx_base_code 是 NOT NULL DEFAULT ''（迁移 633:38）：空串就是「未固化」的正式表示。
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
	// ① 只有基准币对得上的那单参与折算：10000 × 7.20 = 72000 分。
	if overview.Converted.ConvertedSales != 72000 {
		t.Fatalf("折算销售额只该含当前基准币的快照 = 72000 分，实际 %d"+
			"（基准币不同的旧快照被算进来了，页面标签会变成假话）",
			overview.Converted.ConvertedSales)
	}
	if overview.Converted.ConvertedOrders != 1 {
		t.Fatalf("已折算订单数应为 1，实际 %d", overview.Converted.ConvertedOrders)
	}
	// ② 其余两单（基准币不符 / 快照基准币为空串）都算未折算。
	if overview.Converted.UnconvertedOrders != 2 {
		t.Fatalf("未折算订单数应为 2，实际 %d", overview.Converted.UnconvertedOrders)
	}
	// ③ 两侧互补：等于区间内已付款订单总数。
	if got, want := overview.Converted.ConvertedOrders+overview.Converted.UnconvertedOrders,
		int64(3); got != want {
		t.Fatalf("已折算 + 未折算应等于区间订单总数 %d，实际 %d", want, got)
	}
}

