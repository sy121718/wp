package feature

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

func TestCouponWindowParsedInUTC(t *testing.T) {
	f := newOrderFixture(t)
	res := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "TZ-UTC", Name: "时区用例", DiscountType: "fixed", DiscountValue: 1000,
		StartsAt: "2026-12-01 10:00", EndsAt: "2026-12-31 23:59",
	})

	if res.StartsAt == nil || res.EndsAt == nil {
		t.Fatalf("时间窗应被解析落库: %+v", res)
	}

	wantStart := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	if !res.StartsAt.Time().UTC().Equal(wantStart) {
		t.Fatalf("生效时间应解释为 UTC: 期望 %s，实际 %s", wantStart.Format(time.RFC3339), res.StartsAt.Time().UTC().Format(time.RFC3339))
	}
	wantEnd := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	if !res.EndsAt.Time().UTC().Equal(wantEnd) {
		t.Fatalf("结束时间应解释为 UTC: 期望 %s，实际 %s", wantEnd.Format(time.RFC3339), res.EndsAt.Time().UTC().Format(time.RFC3339))
	}

	if res.TimeZone != "UTC" {
		t.Fatalf("响应应带出时间窗口径，实际 %q", res.TimeZone)
	}

	var stored time.Time
	if err := f.db.Raw("SELECT starts_at FROM coupons WHERE id = ?", res.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("读取库中时间窗失败: %v", err)
	}
	if !stored.UTC().Equal(wantStart) {
		t.Fatalf("库中生效时刻应为 UTC %s，实际 %s", wantStart.Format(time.RFC3339), stored.UTC().Format(time.RFC3339))
	}
}

func TestCouponWindowStatusIndependentOfProcessTimeZone(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()

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

func TestCouponWindowAcceptsDateTimePickerForm(t *testing.T) {
	f := newOrderFixture(t)
	ctx := context.Background()

	raw := &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "DTL-RAW", Name: "控件原始形态",
		DiscountType: "fixed", DiscountValue: 1000, Status: 1,
		StartsAt: "2026-01-01T09:00",
	}
	if _, err := f.orders.CreateCoupon(ctx, raw); err == nil {
		t.Fatal("带 T 的控件形态不该被服务端直接接受：若这里通过了，说明解析布局已覆盖它，" +
			"写路径的归口就该删掉（两处都能收 = 两处都在定义格式）")
	}

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
