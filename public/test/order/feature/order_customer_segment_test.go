package feature

// order_customer_segment_test.go — 客户分段 id 列表（客户列表筛选）。
//
// 这里是 docs/17 §P7 的**对账闸门**：客户概览页说「新客 2 人」，客户列表按「新客」
// 筛出来就必须是 2 条。两个数出自两段 SQL（一条 COUNT、一条 SELECT id），
// 共用 orderCustomerCTEs 是它们不会分叉的唯一保证 —— 本测试就是钉住这一点。
//
// 同时钉住分页：翻页拿到的 id 不能重复也不能漏（没有 ORDER BY 时 PostgreSQL
// 每次可以给出不同的前 N 行，翻页会静默漏人）。

import (
	"context"
	"fmt"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

func TestCustomerSegmentIDsReconcileWithGrowth(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	_ = db
	// 与 order_customer_growth_test.go 同一批构造（区间 2026-09-01 ~ 2026-09-05）。
	mkGrowthOrder(t, m, summaryProjectA, "S-A0", "paid", 1000, rangeTime(-32), growthUser(9201))
	mkGrowthOrder(t, m, summaryProjectA, "S-A1", "paid", 1000, rangeTime(2), growthUser(9201))
	mkGrowthOrder(t, m, summaryProjectA, "S-A2", "completed", 2000, rangeTime(3), growthUser(9201))

	mkGrowthOrder(t, m, summaryProjectA, "S-B1", "paid", 3000, rangeTime(1), growthUser(9202))
	mkGrowthOrder(t, m, summaryProjectA, "S-B2", "shipped", 3000, rangeTime(2), growthUser(9202))
	mkGrowthOrder(t, m, summaryProjectA, "S-B3", "paid", 3000, rangeTime(3), growthUser(9202))

	mkGrowthOrder(t, m, summaryProjectA, "S-C1", "paid", 4000, rangeTime(4), growthUser(9203))
	mkGrowthOrder(t, m, summaryProjectA, "S-D0", "paid", 5000, rangeTime(-40), growthUser(9204))
	mkGrowthOrder(t, m, summaryProjectA, "S-D1", "paid", 5000, rangeTime(5), growthUser(9204))

	mkGrowthOrder(t, m, summaryProjectA, "S-E1", "cancelled", 9000, rangeTime(2), growthUser(9205))
	mkGrowthOrder(t, m, summaryProjectA, "S-F1", "paid", 9000, rangeTime(2), nil)
	mkGrowthOrder(t, m, "22222222-2222-2222-2222-222222222222", "S-X1", "paid", 9000, rangeTime(2), growthUser(9206))

	growth, err := svc.CustomerGrowthByRange(context.Background(), &orderdto.CustomerGrowthReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
	})
	if err != nil {
		t.Fatalf("取客户增长失败: %v", err)
	}

	// 对账：每个分段的 id 条数必须等于概览页的对应计数。
	cases := []struct {
		segment string
		want    int64
		what    string
	}{
		{"new", growth.NewCustomers, "新客"},
		{"returning", growth.ReturningCustomers, "回头客"},
		{"repurchasing", growth.Repurchasers, "复购客户"},
	}
	for _, c := range cases {
		res, err := svc.CustomerSegmentIDsByRange(context.Background(), &orderdto.CustomerSegmentIDsReq{
			ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: c.segment,
		})
		if err != nil {
			t.Fatalf("取分段 %s 失败: %v", c.segment, err)
		}
		if res.Total != c.want {
			t.Errorf("%s：列表筛出 %d 条，概览页说 %d 人 —— 两个数字必须相等",
				c.what, res.Total, c.want)
		}
		if int64(len(res.UserIDs)) != c.want {
			t.Errorf("%s：返回 %d 个 id，总数 %d（总数与行数不一致说明分页或计数写错了）",
				c.what, len(res.UserIDs), res.Total)
		}
	}

	// 新客与回头客合起来必须是「区间下单客户」那一批人，且两段不重叠。
	newRes, _ := svc.CustomerSegmentIDsByRange(context.Background(), &orderdto.CustomerSegmentIDsReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "new",
	})
	retRes, _ := svc.CustomerSegmentIDsByRange(context.Background(), &orderdto.CustomerSegmentIDsReq{
		ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "returning",
	})
	if int64(len(newRes.UserIDs)+len(retRes.UserIDs)) != growth.OrderingCustomers {
		t.Errorf("新客 %d + 回头客 %d != 区间下单客户 %d",
			len(newRes.UserIDs), len(retRes.UserIDs), growth.OrderingCustomers)
	}
	seen := map[int64]string{}
	for _, id := range newRes.UserIDs {
		seen[id] = "new"
	}
	for _, id := range retRes.UserIDs {
		if where, dup := seen[id]; dup {
			t.Errorf("客户 %d 同时出现在 %s 与 returning 两段里", id, where)
		}
	}
}

// TestCustomerSegmentIDsPagingIsStable 翻页不重不漏：ORDER BY 缺了的话 PostgreSQL
// 每次可以给出不同的前 N 行，表现是「第二页少了一个人」而不是报错。
func TestCustomerSegmentIDsPagingIsStable(t *testing.T) {
	db, m, svc := newRangeFixture(t)
	if db == nil {
		return
	}
	_ = db
	// 五个新客，每人一单。
	want := []int64{9301, 9302, 9303, 9304, 9305}
	for i, id := range want {
		mkGrowthOrder(t, m, summaryProjectA, fmt.Sprintf("P-%d", i), "paid", 1000, rangeTime(2), growthUser(uint64(id)))
	}

	var got []int64
	for offset := 0; offset < len(want); offset += 2 {
		res, err := svc.CustomerSegmentIDsByRange(context.Background(), &orderdto.CustomerSegmentIDsReq{
			ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05",
			Segment: "new", Limit: 2, Offset: offset,
		})
		if err != nil {
			t.Fatalf("取分段失败: %v", err)
		}
		if res.Total != int64(len(want)) {
			t.Errorf("offset=%d 时总数应为 %d，实得 %d（总数不受 limit 影响）", offset, len(want), res.Total)
		}
		got = append(got, res.UserIDs...)
	}
	if len(got) != len(want) {
		t.Fatalf("翻页共取回 %d 个 id，期望 %d（漏人或重复）", len(got), len(want))
	}
	seen := map[int64]bool{}
	for _, id := range got {
		if seen[id] {
			t.Errorf("id %d 在翻页中重复出现", id)
		}
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("id %d 在翻页中丢失", id)
		}
	}
}

// TestCustomerSegmentRejectsUnknownName 拼错的分段名当场拒，不静默回落成「全部客户」。
// 回落不会报错，只会让一个写错的筛选条件显示成「全部」——看起来是对的。
func TestCustomerSegmentRejectsUnknownName(t *testing.T) {
	_, _, svc := newRangeFixture(t)
	for _, req := range []*orderdto.CustomerSegmentIDsReq{
		nil,
		{ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "everyone"},
		{ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: ""},
		{ProjectID: summaryProjectA, From: "2026-09-01", To: "2026-09-05", Segment: "NEW"},
		{From: "2026-09-01", To: "2026-09-05", Segment: "new"},
		{ProjectID: summaryProjectA, Segment: "new"},
	} {
		if _, err := svc.CustomerSegmentIDsByRange(context.Background(), req); err == nil {
			t.Errorf("期望报错，实得 nil：%+v", req)
		}
	}
}
