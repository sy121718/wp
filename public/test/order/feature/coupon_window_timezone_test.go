package feature

// coupon_window_timezone_test.go — 券时间窗的时区口径（审计 TX-011）。
//
// 缺陷形态：时间窗按 time.Local 解析，于是「同一张券的生效时刻」取决于部署机器的时区。
// 换一台机器（或改一次服务器时区）所有券整体平移 8 小时，而界面上的文本一个字都没变 ——
// 这种偏差不会报错，只会表现为「券提前生效」或「到点了还是不能用」。
//
// 两条断言对应审计的两条验收：解析不依赖进程时区、设置的时间与生效的时间一致。

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

// TestCouponWindowParsedInUTC 时间窗按固定口径解析，与进程时区无关。
func TestCouponWindowParsedInUTC(t *testing.T) {
	f := newOrderFixture(t)
	res := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "TZ-UTC", Name: "时区用例", DiscountType: "fixed", DiscountValue: 1000,
		StartsAt: "2026-12-01 10:00", EndsAt: "2026-12-31 23:59",
	})

	if res.StartsAt == nil || res.EndsAt == nil {
		t.Fatalf("时间窗应被解析落库: %+v", res)
	}
	// 输入「2026-12-01 10:00」就应当是 UTC 的这一刻。改造前按 time.Local 解析，
	// 在 Asia/Shanghai 下会得到 10:00+08（= 02:00Z），与期望差 8 小时。
	wantStart := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	if !res.StartsAt.Time().UTC().Equal(wantStart) {
		t.Fatalf("生效时间应解释为 UTC: 期望 %s，实际 %s", wantStart.Format(time.RFC3339), res.StartsAt.Time().UTC().Format(time.RFC3339))
	}
	wantEnd := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	if !res.EndsAt.Time().UTC().Equal(wantEnd) {
		t.Fatalf("结束时间应解释为 UTC: 期望 %s，实际 %s", wantEnd.Format(time.RFC3339), res.EndsAt.Time().UTC().Format(time.RFC3339))
	}

	// 口径随券一起返回：后台表单据此提示「填的是哪个时区的时间」。
	if res.TimeZone != "UTC" {
		t.Fatalf("响应应带出时间窗口径，实际 %q", res.TimeZone)
	}

	// 落库值同样不受进程时区影响（列已是 timestamptz，存的是绝对时刻）。
	var stored time.Time
	if err := f.db.Raw("SELECT starts_at FROM coupons WHERE id = ?", res.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("读取库中时间窗失败: %v", err)
	}
	if !stored.UTC().Equal(wantStart) {
		t.Fatalf("库中生效时刻应为 UTC %s，实际 %s", wantStart.Format(time.RFC3339), stored.UTC().Format(time.RFC3339))
	}
}

// TestCouponWindowStatusIndependentOfProcessTimeZone 状态判定只看时刻，不看进程时区。
//
// 用例构造一张「已经结束」的券与一张「尚未开始」的券，断言状态文案稳定 ——
// 时区口径错了的话，这两张券在别的服务器上会变成另一种状态。
func TestCouponWindowStatusIndependentOfProcessTimeZone(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()

	// 故意把「现在」的判定跑在不同时区视图下：券的窗口是绝对时刻，结论必须一样。
	expired := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "TZ-EXPIRED", Name: "已过期", DiscountType: "fixed", DiscountValue: 500,
		StartsAt: "2020-01-01", EndsAt: "2020-12-31",
	})
	if expired.StatusLabel != "已过期" {
		t.Fatalf("已过窗口的券应判为已过期，实际 %q", expired.StatusLabel)
	}
	future := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "TZ-FUTURE", Name: "未开始", DiscountType: "fixed", DiscountValue: 500,
		StartsAt: "2099-01-01", EndsAt: "2099-12-31",
	})
	if future.StatusLabel != "未开始" {
		t.Fatalf("未来窗口的券应判为未开始，实际 %q", future.StatusLabel)
	}

	// 列出券时状态一致（读路径同样不受时区影响）。
	list, err := f.orders.ListCoupons(ctx, &orderdto.CouponListReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("列出优惠码失败: %v", err)
	}
	labels := map[string]string{}
	for _, c := range list.List {
		labels[c.Code] = c.StatusLabel
	}
	if labels["TZ-EXPIRED"] != "已过期" || labels["TZ-FUTURE"] != "未开始" {
		t.Fatalf("列表里的状态应与单条一致: %+v", labels)
	}
}

// TestCouponWindowAcceptsDateTimePickerForm 时间窗控件（datetime-local）的提交形态能落库。
//
// 判据来自「控件换型最容易出的回归」：**换了控件、保存不了**。后台表单里
// <input type="datetime-local"> 提交的是它自己的 value 形态「2026-01-01T09:00」（带 T），
// 而服务端的解析布局只有「2006-01-02」/「2006-01-02 15:04(:05)」—— 两者之间靠表单解析归口
// （orderhttp.couponFormTimeValue）转换一次。
//
// 本用例把这条链的两端都钉住，且**故意先断言未转换的形态会被拒绝**：
//
//	· 若控件形态被直接接受 → 说明服务端已能识别带 T 的写法，归口那层是多余的，该删；
//	· 若归一化后的形态被拒绝 → 就是「表单自己生成的格式自己都不收」，控件换对了也存不了。
//
// 「控件提交 → 归口转换」那一半在 internal/module/order/inbound/http/coupon_page_query_test.go。
func TestCouponWindowAcceptsDateTimePickerForm(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()

	// ① 控件形态（带 T）**不经归口**时会被服务端拒绝 —— 归口因此是必需的，不是装饰。
	raw := &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "DTL-RAW", Name: "控件原始形态",
		DiscountType: "fixed", DiscountValue: 1000, Status: 1,
		StartsAt: "2026-01-01T09:00",
	}
	if _, err := f.orders.CreateCoupon(ctx, raw); err == nil {
		t.Fatal("带 T 的控件形态不该被服务端直接接受：若这里通过了，说明解析布局已覆盖它，" +
			"form 侧的 couponFormTimeValue 就该删掉（两处都能收 = 两处都在定义格式）")
	}

	// ② 归口转换之后的形态（orderhttp.couponFormTimeValue("2026-01-01T09:00") 的输出，
	//    与 coupon_page_query_test.go 里的字面量同值）能被接受，且时刻分毫不差。
	res := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "DTL-0900", Name: "日期时间控件", DiscountType: "fixed", DiscountValue: 1000,
		StartsAt: "2026-01-01 09:00", EndsAt: "2026-01-31 23:59",
	})
	wantStart := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 1, 31, 23, 59, 0, 0, time.UTC)
	if res.StartsAt == nil || !res.StartsAt.Time().UTC().Equal(wantStart) {
		t.Fatalf("开始时刻应为 %s，实际 %v", wantStart.Format(time.RFC3339), res.StartsAt)
	}
	if res.EndsAt == nil || !res.EndsAt.Time().UTC().Equal(wantEnd) {
		t.Fatalf("结束时刻应为 %s，实际 %v", wantEnd.Format(time.RFC3339), res.EndsAt)
	}
}
