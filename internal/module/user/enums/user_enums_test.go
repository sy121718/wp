package userenums

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

	if !strings.Contains(LabelVerified, "邮箱") || !strings.Contains(LabelUnverified, "邮箱") {
		t.Fatalf("邮箱验证的兜底文案应带主语，实际 %q / %q", LabelVerified, LabelUnverified)
	}
}

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
