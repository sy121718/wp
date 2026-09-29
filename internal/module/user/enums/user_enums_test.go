package userenums

// user_enums_test.go — 枚举 → 展示名的映射（key + 中文兜底）与词条 key 的离线核对。
//
// 这一层的错误**不会报错、也不会 panic**：key 拼错时取词链路静默回落到中文兜底，
// 页面看起来完全正常，只有英文界面永远中文（本次重构要修的正是这个）。
// 所以「key 与兜底分别是什么」必须被断言钉住，而不是靠读代码。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusLabelKeyAndFallback(t *testing.T) {
	cases := []struct {
		name             string
		status           int
		key, fallbackOut string
	}{
		{"正常", StatusActive, LabelKeyStatusActive, "正常"},
		{"已停用", StatusDisabled, LabelKeyStatusDisabled, "已停用"},
		{"待激活", StatusPending, LabelKeyStatusPending, "待激活"},
		{"全部（筛选口径）", StatusAll, LabelKeyStatusAll, "全部"},
		// 认不出的取值：不假装认识（key 留空 → 取词回落兜底），但保留原始数字便于查数据。
		{"未知取值", 7, "", "未知状态(7)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, fallback := StatusLabel(tc.status)
			if key != tc.key || fallback != tc.fallbackOut {
				t.Fatalf("StatusLabel(%d) = (%q, %q)，期望 (%q, %q)",
					tc.status, key, fallback, tc.key, tc.fallbackOut)
			}
		})
	}
}

func TestEmailVerifiedLabelKeyAndFallback(t *testing.T) {
	key, fallback := EmailVerifiedLabel(true)
	if key != LabelKeyVerified || fallback != LabelVerified {
		t.Fatalf("已验证：(%q, %q)", key, fallback)
	}
	key, fallback = EmailVerifiedLabel(false)
	if key != LabelKeyUnverified || fallback != LabelUnverified {
		t.Fatalf("未验证：(%q, %q)", key, fallback)
	}
	// 兜底必须带主语：这个徽章与状态徽章并排，「已验证」会被读成「账号已验证」。
	if !strings.Contains(LabelVerified, "邮箱") || !strings.Contains(LabelUnverified, "邮箱") {
		t.Fatalf("邮箱验证的兜底文案应带主语，实际 %q / %q", LabelVerified, LabelUnverified)
	}
}

// 订单状态标签的覆盖已随真源迁到 order 模块：order/enums/order_enums_label_test.go
// （user 侧不再持有那份映射，跨模块经 ordercontract.OrderStatusLabel 引用）。

// TestLabelKeysExistInSeeds 每个词条 key 都必须能在 seed 文本里找到。
//
// 判据不是「数据库里有没有」（那要起库），而是「seed 里有没有」：本文件不新增词条，
// 所以用到的 key 必然来自既有 seed。key 拼错时取词静默回落中文兜底、页面看不出异常，
// 只有这条断言能拦下来。真源仍是 sys_i18n —— 起库的 feature 测试里另有存在性覆盖。
func TestLabelKeysExistInSeeds(t *testing.T) {
	seedDir := filepath.Join("..", "..", "..", "..", "public", "migrations")
	files, err := filepath.Glob(filepath.Join(seedDir, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到 seed 目录 %s：%v", seedDir, err)
	}
	blob := strings.Builder{}
	for _, f := range files {
		if info, serr := os.Stat(f); serr != nil || info.IsDir() {
			continue
		}
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("读取 %s 失败：%v", f, rerr)
		}
		blob.Write(data)
	}
	all := blob.String()

	keys := []string{
		LabelKeyStatusAll, LabelKeyStatusActive, LabelKeyStatusDisabled, LabelKeyStatusPending,
		LabelKeyStatusLocked, LabelKeyVerified, LabelKeyUnverified,
	}
	// 订单状态 key 是拼出来的（前缀 + 状态值），逐个列全。
	keys = append(keys,
		"site.fragment.order.status.pending", "site.fragment.order.status.paid",
		"site.fragment.order.status.shipped", "site.fragment.order.status.completed",
		"site.fragment.order.status.cancelled", "site.fragment.order.status.refunded",
	)
	for _, key := range keys {
		if !strings.Contains(all, key) {
			t.Errorf("词条 key %q 在任何 seed 文本里都找不到 —— 取词会静默回落中文兜底", key)
		}
	}
}
