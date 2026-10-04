package feature

// order_customer_rfm_test.go — 客户 RFM 分层。
//
// 钉住四件事：
//
//	1. 三段的计数之和 == 参与分层的客户数（分段是同一批人的切分，不是三个独立指标）；
//	2. 每个维度的得分都落在 1-5，且总分 == R + F + M；
//	3. R 取**全历史**最后下单时刻（窗口外下过单的老客户 R 分照样高）——
//	   这条最容易写成「只算窗口内的单」，那样所有窗口内下单的人 R 都会一样；
//	4. 分段筛选只返回那一段，且不改变分段计数（计数是全量的，不受筛选影响）。

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
	// 窗口是 2026-09-01 ~ 2026-09-05。
	//
	// 造 10 个人，让 R / F / M 三个维度都有分布：
	//   9401-9405：窗口内多单、金额高 → 高价值
	//   9406-9408：窗口内 1-2 单、金额中等
	//   9409-9410：窗口内 1 单、金额很低
	// 另有两个「窗口外下过单、窗口内也下过」的老客户，用来验证 R 取全历史。
	amounts := []int64{9000, 8000, 7000, 6000, 5000, 4000, 3000, 2000, 1000, 500}
	for i, uid := range []uint64{9401, 9402, 9403, 9404, 9405, 9406, 9407, 9408, 9409, 9410} {
		// 越靠前的人下单越近、单数越多。
		n := 3
		if i >= 5 {
			n = 1
		}
		for k := 0; k < n; k++ {
			mkGrowthOrder(t, m, summaryProjectA, fmtSegOrderNo(i, k), "paid",
				amounts[i], rangeTime(4-k), growthUser(uid))
		}
	}
	// 老客户：8 月下过一单，窗口内也下过（R 应比同期但只算窗口的人更近）。
	mkGrowthOrder(t, m, summaryProjectA, "R-OLD1", "paid", 3000, rangeTime(-30), growthUser(9411))
	mkGrowthOrder(t, m, summaryProjectA, "R-OLD2", "paid", 3000, rangeTime(2), growthUser(9411))

	// 干扰项：取消单不计入。
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

	// 老客户（窗口内下过单、窗口外也下过）必须出现在列表里 —— R 取全历史。
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

// TestCustomerRfmSegmentFilterKeepsSummary 按分段筛选时，头部的三段计数**不变**
// （那是全量统计）：否则页面上会出现「高价值 3 人，列表里 5 行」这种自相矛盾的画面。
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

// TestCustomerRfmRejectsUnknownSegment 认不出的分段当场拒，不回落成「全部」。
// 回落会让一个写错的筛选显示成完整报表，而用户以为自己看的是「高价值客户」。
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

// fmtSegOrderNo 生成稳定的订单号（同一客户不同单、不同客户互不相同）。
func fmtSegOrderNo(user, seq int) string {
	return "RFM-" + strconv.Itoa(user) + "-" + strconv.Itoa(seq)
}

// TestCustomerRfmSegmentIDsShareScoring 分段取 id 与 RFM 页的计数必须同源。
//
// 两处各写一条打分 SQL 会在数据变动的边界上给出不同分档 —— 表现是
// 「RFM 页说他是 vip，用 vip 筛客户列表却查不到他」，而两边各自的页面看起来都对。
// 这条断言的就是那个等式：分段取 id 的 Total == RFM 页该分段的计数。
func TestCustomerRfmSegmentIDsShareScoring(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	// 五个客户，保证五个分位每一档都有人（NTILE(5) 在样本太小时会退化）。
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
	// 认不出的分段当场拒（不静默回落成「全部」）。
	for _, bad := range []string{"", "gold", "VIP"} {
		if _, err := svc.CustomerRfmSegmentIDsByRange(ctx, &orderdto.CustomerRfmSegmentIDsReq{
			ProjectID: pid, From: "2026-09-01", To: "2026-09-05", Segment: bad, Limit: 10,
		}); err == nil {
			t.Errorf("分段 %q 不在白名单里，应报错", bad)
		}
	}
}
