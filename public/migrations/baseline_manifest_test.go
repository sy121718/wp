package migrations

import (
	"regexp"
	"strings"
	"testing"

	baseline "go_wp/schema/baseline"
)

// manifest 是跳过判定的唯一依据：它必须与注册表同序同集，
// 否则基线库会跳过一个其实没在基线里建过的迁移（新库缺对象、存量库正常）。
func TestBaselineManifestMatchesRegistry(t *testing.T) {
	manifest := baseline.Versions()
	if len(manifest) == 0 {
		t.Fatal("manifest 为空：先跑 bash scripts/baseline-generate.sh")
	}
	want := make([]string, 0, len(All()))
	for _, m := range All() {
		want = append(want, m.Version)
	}
	if len(manifest) != len(want) {
		t.Fatalf("manifest 有 %d 条，注册表有 %d 条：改过迁移就重跑 bash scripts/baseline-generate.sh", len(manifest), len(want))
	}
	for i := range want {
		if manifest[i] != want[i] {
			t.Fatalf("manifest 第 %d 条是 %q，注册表是 %q：manifest 必须与注册表同序同集", i+1, manifest[i], want[i])
		}
	}
}

// 月分区不得进基线：它们按生成时刻滚动，固化进基线会在下个月变成历史窗口。
func TestBaselineStructureExcludesMonthPartitions(t *testing.T) {
	re := regexp.MustCompile("(?m)^CREATE TABLE (?:public\\.)?[a-z0-9_]+_[0-9]{4}_[0-9]{2}\\b")
	if loc := re.FindString(baseline.StructureSQL()); loc != "" {
		t.Fatalf("structure.sql 含月分区：%s（月窗口由 partition.EnsureAhead 在建库末尾补齐）", loc)
	}
}

// sys_baseline 的数据行由 init.sql 单独写入，不能混在系统数据 dump 里：
// 否则每次生成的时间戳都不同，产物永远无法逐字节复现。
func TestBaselineSystemDataExcludesBaselineLedger(t *testing.T) {
	if strings.Contains(baseline.SystemDataSQL(), "COPY public.sys_baseline ") {
		t.Fatal("system_data.sql 不得包含 sys_baseline 数据：水位由 init.sql 单独写入")
	}
}

func TestBaselineManifestSHA256Shape(t *testing.T) {
	sum := baseline.ManifestSHA256()
	if len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
		t.Fatalf("manifest SHA-256 形状不对：%q", sum)
	}
}
