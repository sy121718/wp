package orderenums

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 展示标签（枚举 → 展示名）的取值覆盖：key 与中文兜底都必须逐字稳定。
//
// 判据是「两样都给」：只给中文 → 英文界面恒中文；只给 key → 词条缺失时页面
// 显示裸 key（admin.orders.status.paid）。任一项漂了，页面上都不会报错，
// 只会静默显示成另一种东西 —— 所以这里逐字钉住。
func TestOrderStatusLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		status, key, fallbackOut string
	}{
		{"pending", "site.fragment.order.status.pending", "待付款"},
		{"paid", "site.fragment.order.status.paid", "已付款"},
		{"shipped", "site.fragment.order.status.shipped", "已发货"},
		{"completed", "site.fragment.order.status.completed", "已完成"},
		{"cancelled", "site.fragment.order.status.cancelled", "已取消"},
		{"refunded", "site.fragment.order.status.refunded", "已退款"},
		// 空状态不是「一个状态」，而是「没有最近一单」：不该被当状态去取词。
		{"", "", "—"},
		// 认不出的值原样回显：宁可显示生值，也不显示空白。
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		key, fallback := OrderStatusLabel(tc.status)
		if key != tc.key || fallback != tc.fallbackOut {
			t.Errorf("OrderStatusLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.status, key, fallback, tc.key, tc.fallbackOut)
		}
	}
}

func TestReturnStatusLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		status, key, fallbackOut string
	}{
		{"requested", ReturnStatusKeyRequested, "待审核"},
		{"approved", ReturnStatusKeyApproved, "待收货"},
		{"received", ReturnStatusKeyReceived, "待退款"},
		{"completed", ReturnStatusKeyCompleted, "已完成"},
		{"rejected", ReturnStatusKeyRejected, "已拒绝"},
		{"cancelled", ReturnStatusKeyCancelled, "已撤销"},
		{"", "", "—"},
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		key, fallback := ReturnStatusLabel(tc.status)
		if key != tc.key || fallback != tc.fallbackOut {
			t.Errorf("ReturnStatusLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.status, key, fallback, tc.key, tc.fallbackOut)
		}
	}
}

// TestFulfillmentLabelKeyAndFallback 履约状态文案（迁移 654 seed 的 12 条）。
//
// 与订单状态一样逐字钉：key 与中文兜底都必须稳定 —— 只给中文会让英文界面恒中文，
// 只给 key 会让词条缺失时露出裸 key，两边都不报错。
func TestFulfillmentLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		status, key, fallbackOut string
	}{
		{"pending", FulfillmentKeyPrefix + "pending", "待履约"},
		{"paid", FulfillmentKeyPrefix + "paid", "待发货"},
		{"partial", FulfillmentKeyPrefix + "partial", "部分发货"},
		{"shipped", FulfillmentKeyPrefix + "shipped", "已发货"},
		{"received", FulfillmentKeyPrefix + "received", "已收货"},
		{"returned", FulfillmentKeyPrefix + "returned", "已退货"},
		{"return_done", FulfillmentKeyPrefix + "return_done", "退货完成"},
		{"reship", FulfillmentKeyPrefix + "reship", "换货补发"},
		{"exception", FulfillmentKeyPrefix + "exception", "履约异常"},
		{"failed", FulfillmentKeyPrefix + "failed", "履约失败"},
		{"cancelled", FulfillmentKeyPrefix + "cancelled", "已取消"},
		{"refunded", FulfillmentKeyPrefix + "refunded", "已退款"},
		{"", "", "—"},
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		key, fallback := FulfillmentLabel(tc.status)
		if key != tc.key || fallback != tc.fallbackOut {
			t.Errorf("FulfillmentLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.status, key, fallback, tc.key, tc.fallbackOut)
		}
	}
}

// TestFulfillmentStatusesMatchLabels 有序清单与文案表必须同集合。
//
// 清单在 enums、判据在 model 的常量、文案在 FulfillmentLabel —— 三处各写一份，
// 少一个的症状是筛选行少一个徽章（不报错，只是那个状态永远筛不到）。
func TestFulfillmentStatusesMatchLabels(t *testing.T) {
	if len(FulfillmentStatuses) != 12 {
		t.Fatalf("履约状态应有 12 个，实际 %d 个：%v", len(FulfillmentStatuses), FulfillmentStatuses)
	}
	seen := make(map[string]bool, len(FulfillmentStatuses))
	for _, v := range FulfillmentStatuses {
		if seen[v] {
			t.Errorf("履约状态 %q 在清单里重复了", v)
		}
		seen[v] = true
		if key, _ := FulfillmentLabel(v); key == "" {
			t.Errorf("履约状态 %q 在清单里，但 FulfillmentLabel 给不出 key", v)
		}
	}
}

// TestCreatedViaLabelKeyAndFallback 来源三态（复用迁移 448 的词条）。
func TestCreatedViaLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		via, key, fallbackOut string
	}{
		{"checkout", CreatedViaKeyPrefix + "checkout", "访客结算"},
		{"admin", CreatedViaKeyPrefix + "admin", "后台自建"},
		{"api", CreatedViaKeyPrefix + "api", "接口"},
		{"", "", "—"},
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		key, fallback := CreatedViaLabel(tc.via)
		if key != tc.key || fallback != tc.fallbackOut {
			t.Errorf("CreatedViaLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.via, key, fallback, tc.key, tc.fallbackOut)
		}
	}
}

func TestCouponStateLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		state, key, fallbackOut string
	}{
		{CouponStateEnabled, "admin.coupons.status.enabled", "生效中"},
		{CouponStateDisabled, "admin.coupons.status.disabled", "已停用"},
		{CouponStateExpired, "admin.coupons.status.expired", "已过期"},
		{CouponStateNotStarted, "admin.coupons.status.not_started", "未开始"},
		{CouponStateExhausted, "admin.coupons.status.exhausted", "已用完"},
		{"", "", "—"},
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		key, fallback := CouponStateLabel(tc.state)
		if key != tc.key || fallback != tc.fallbackOut {
			t.Errorf("CouponStateLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.state, key, fallback, tc.key, tc.fallbackOut)
		}
	}
}

// TestLabelKeysAreDistinct 同一批标签里的 key 不能重复：重复即意味着两条语义
// 共用一条词条，改文案时必然有一处被改错。
func TestLabelKeysAreDistinct(t *testing.T) {
	seen := map[string]string{}
	record := func(label, key string) {
		if key == "" {
			return
		}
		if prev, ok := seen[key]; ok {
			t.Errorf("key %q 同时属于 %s 与 %s", key, prev, label)
		}
		seen[key] = label
	}
	for _, s := range []string{"pending", "paid", "shipped", "completed", "cancelled", "refunded"} {
		if k, _ := OrderStatusLabel(s); k != "" {
			record("order:"+s, k)
		}
	}
	for _, s := range []string{"requested", "approved", "received", "completed", "rejected", "cancelled"} {
		if k, _ := ReturnStatusLabel(s); k != "" {
			record("return:"+s, k)
		}
	}
	for _, s := range []string{CouponStateEnabled, CouponStateDisabled, CouponStateExpired, CouponStateNotStarted, CouponStateExhausted} {
		if k, _ := CouponStateLabel(s); k != "" {
			record("coupon:"+s, k)
		}
	}
}

// TestLabelKeysExistInSeeds 每个词条 key 都必须能在 seed 文本里找到。
//
// 判据不是「数据库里有没有」（那要起库），而是「seed 里有没有」：key 拼错时取词静默
// 回落中文兜底、页面看不出异常，只有这条断言能拦下来。真源仍是 sys_i18n ——
// 起库的 feature 测试另有存在性覆盖。
//
// 复用既有词条（site.fragment.order.status.*，迁移 157）与新增词条（本批迁移）都要能查到。
func TestLabelKeysExistInSeeds(t *testing.T) {
	seedDir := filepath.Join("..", "..", "..", "..", "public", "migrations")
	files, err := filepath.Glob(filepath.Join(seedDir, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到 seed 目录 %s：%v", seedDir, err)
	}
	blob := strings.Builder{}
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("读取 %s 失败：%v", f, rerr)
		}
		blob.Write(b)
	}
	all := blob.String()

	keys := make([]string, 0, 20)
	for _, s := range []string{"pending", "paid", "shipped", "completed", "cancelled", "refunded", ""} {
		if k, _ := OrderStatusLabel(s); k != "" {
			keys = append(keys, k)
		}
	}
	for _, s := range []string{"requested", "approved", "received", "completed", "rejected", "cancelled", ""} {
		if k, _ := ReturnStatusLabel(s); k != "" {
			keys = append(keys, k)
		}
	}
	for _, s := range []string{CouponStateEnabled, CouponStateDisabled, CouponStateExpired, CouponStateNotStarted, CouponStateExhausted, ""} {
		if k, _ := CouponStateLabel(s); k != "" {
			keys = append(keys, k)
		}
	}

	for _, k := range keys {
		if !strings.Contains(all, k) {
			t.Errorf("词条 key %q 在 seed 文本里找不到（拼错时取词静默回落中文兜底，页面看不出异常）", k)
		}
	}
}
