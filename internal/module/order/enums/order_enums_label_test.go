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
