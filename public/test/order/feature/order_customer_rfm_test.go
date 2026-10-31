package feature

import (
	"context"
	"strconv"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

func TestCustomerRfmScoresAndSegments(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	_ = db

	amounts := []int64{9000, 8000, 7000, 6000, 5000, 4000, 3000, 2000, 1000, 500}
	for i, uid := range []uint64{9401, 9402, 9403, 9404, 9405, 9406, 9407, 9408, 9409, 9410} {

		n := 3
		if i >= 5 {
			n = 1
		}
		for k := 0; k < n; k++ {
			mkGrowthOrder(t, m, summaryProjectA, fmtSegOrderNo(i, k), "paid",
				amounts[i], rangeTime(4-k), growthUser(uid))
		}
	}

	mkGrowthOrder(t, m, summaryProjectA, "R-OLD1", "paid", 3000, rangeTime(-30), growthUser(9411))
	mkGrowthOrder(t, m, summaryProjectA, "R-OLD2", "paid", 3000, rangeTime(2), growthUser(9411))

	mkGrowthOrder(t, m, summaryProjectA, "R-CANCEL", "cancelled", 9000, rangeTime(1), growthUser(9412))

	res, err := svc.CustomerRfmByRange(context.Background(), &orderdto.CustomerRfmReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取 RFM 失败: %v", err)
	}

	if res.Customers != 11 {
		t.Errorf("参与分层的客户数 = %d，期望 11（10 个 + 1 个老客户；取消单不算）", res.Customers)
	}
	if res.Vip+res.Potential+res.LowValue != res.Customers {
		t.Errorf("三段之和 %d != 客户数 %d —— 分段不是同一批人的切分",
			res.Vip+res.Potential+res.LowValue, res.Customers)
	}
	if res.Total != res.Customers {
		t.Errorf("未按分段筛选时 Total(%d) 应等于 Customers(%d)", res.Total, res.Customers)
	}

	for _, it := range res.Items {
		if it.RScore < 1 || it.RScore > 5 || it.FScore < 1 || it.FScore > 5 || it.MScore < 1 || it.MScore > 5 {
			t.Errorf("客户 %d 的分数越界：R=%d F=%d M=%d", it.UserID, it.RScore, it.FScore, it.MScore)
		}
		if it.TotalScore != it.RScore+it.FScore+it.MScore {
			t.Errorf("客户 %d 总分 %d != R+F+M %d", it.UserID, it.TotalScore, it.RScore+it.FScore+it.MScore)
		}
		if it.SegmentLabel == "" || it.Segment == "" {
			t.Errorf("客户 %d 缺少分段或分段文案", it.UserID)
		}
		if it.RecencyDays < 0 {
			t.Errorf("客户 %d 的 RecencyDays 为负（%d）", it.UserID, it.RecencyDays)
		}
		if it.MonetaryLabel == "" {
			t.Errorf("客户 %d 缺金额展示串", it.UserID)
		}
	}

	found := false
	for _, it := range res.Items {
		if it.UserID == 9411 {
			found = true
		}
	}
	if !found {
		t.Error("窗口内下过单的老客户 9411 没出现在 RFM 里")
	}
}

func TestCustomerRfmSegmentFilterKeepsSummary(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	for i, uid := range []uint64{9501, 9502, 9503, 9504, 9505} {
		mkGrowthOrder(t, m, summaryProjectA, fmtSegOrderNo(i, 0), "paid",
			int64(1000*(i+1)), rangeTime(4-i), growthUser(uid))
	}
	all, err := svc.CustomerRfmByRange(context.Background(), &orderdto.CustomerRfmReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取 RFM 失败: %v", err)
	}
	vip, err := svc.CustomerRfmByRange(context.Background(), &orderdto.CustomerRfmReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "vip",
	})
	if err != nil {
		t.Fatalf("按 vip 取 RFM 失败: %v", err)
	}
	if vip.Vip != all.Vip || vip.Customers != all.Customers {
		t.Errorf("分段计数不该受筛选影响：all=%d/%d vipView=%d/%d",
			all.Vip, all.Customers, vip.Vip, vip.Customers)
	}
	if vip.Total != all.Vip {
		t.Errorf("按 vip 筛选后 Total=%d，应等于全量的 vip 计数 %d", vip.Total, all.Vip)
	}
	for _, it := range vip.Items {
		if it.Segment != "vip" {
			t.Errorf("按 vip 筛选却返回了 %s", it.Segment)
		}
	}
}

func TestCustomerRfmRejectsUnknownSegment(t *testing.T) {
	_, _, svc := newRangeFixture(t)
	for _, req := range []*orderdto.CustomerRfmReq{
		nil,
		{ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "gold"},
		{From: "2026-09-01", To: "2026-09-05"},
		{ProjectID: summaryProjectA, Segment: "vip"},
	} {
		if _, err := svc.CustomerRfmByRange(context.Background(), req); err == nil {
			t.Errorf("期望报错，实得 nil：%+v", req)
		}
	}
}

func fmtSegOrderNo(user, seq int) string {
	return "RFM-" + strconv.Itoa(user) + "-" + strconv.Itoa(seq)
}

func TestCustomerRfmSegmentIDsShareScoring(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	for i, u := range []uint64{5101, 5102, 5103, 5104, 5105} {
		for k := 0; k <= i; k++ {
			mkGrowthOrder(t, m, pid, fmtSegOrderNo(int(u), k), "paid", int64(100*(i+1)), rangeTime(2), growthUser(u))
		}
	}

	page, err := svc.CustomerRfmByRange(ctx, &orderdto.CustomerRfmReq{
		ProjectID: pid, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取 RFM 失败: %v", err)
	}
	for seg, want := range map[string]int64{
		"vip": page.Vip, "potential": page.Potential, "low_value": page.LowValue,
	} {
		res, err := svc.CustomerRfmSegmentIDsByRange(ctx, &orderdto.CustomerRfmSegmentIDsReq{
			ProjectID: pid, From: "2026-09-01", To: "2026-09-05", Segment: seg, Limit: 200,
		})
		if err != nil {
			t.Fatalf("分段 %s 取 id 失败: %v", seg, err)
		}
		if res.Total != want {
			t.Errorf("分段 %s：取 id 的总数 %d != RFM 页计数 %d（两处打分必须同源）",
				seg, res.Total, want)
		}
		if int64(len(res.UserIDs)) != res.Total {
			t.Errorf("分段 %s：返回 %d 个 id 但总数是 %d", seg, len(res.UserIDs), res.Total)
		}
	}

	for _, bad := range []string{"", "gold", "VIP"} {
		if _, err := svc.CustomerRfmSegmentIDsByRange(ctx, &orderdto.CustomerRfmSegmentIDsReq{
			ProjectID: pid, From: "2026-09-01", To: "2026-09-05", Segment: bad, Limit: 10,
		}); err == nil {
			t.Errorf("分段 %q 不在白名单里，应报错", bad)
		}
	}
}
