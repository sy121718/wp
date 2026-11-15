package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"testing"

	baseline "go_wp/schema/baseline"
)

// manifest 冻结的是「基线覆盖的集合」，两段都必须是注册表的**前缀**：
// 段内顺序即执行顺序，新增版本排在段尾、不在基线里，照常执行。
// 要求与注册表全等，会让每次新增迁移都必须重生成基线，而重生成又改变内容摘要，
// 已接入的库随即回落全量 —— 那就不是「基线 + 基线之后的增量」。
func TestBaselineManifestIsRegistryPrefix(t *testing.T) {
	m := baseline.ParseManifest()
	if len(m.Migrations) == 0 {
		t.Fatal("manifest 迁移段为空：先跑 bash scripts/baseline-generate.sh")
	}
	if len(m.Seeds) == 0 {
		t.Fatal("manifest seed 段为空：先跑 bash scripts/baseline-generate.sh")
	}
	assertPrefix(t, "迁移", m.Migrations, migrationVersions())
	assertPrefix(t, "seed", m.Seeds, seedVersions())
}

// 同一段里重复登记会让「跳过集合」掩盖重复执行，也让哈希口径含混。
func TestBaselineManifestHasNoDuplicates(t *testing.T) {
	m := baseline.ParseManifest()
	for _, seg := range []struct {
		name     string
		versions []string
	}{{"迁移", m.Migrations}, {"seed", m.Seeds}} {
		seen := make(map[string]bool, len(seg.versions))
		for _, v := range seg.versions {
			if seen[v] {
				t.Fatalf("%s 段重复登记 %q", seg.name, v)
			}
			seen[v] = true
		}
	}
}

func migrationVersions() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Version)
	}
	return out
}

func seedVersions() []string {
	all := AllSeeds()
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Version)
	}
	return out
}

func assertPrefix(t *testing.T, label string, got, all []string) {
	t.Helper()
	if len(got) > len(all) {
		t.Fatalf("%s段有 %d 条，注册表只有 %d 条：manifest 已过期，重跑 bash scripts/baseline-generate.sh", label, len(got), len(all))
	}
	for i := range got {
		if got[i] != all[i] {
			t.Fatalf("%s段第 %d 条是 %q，注册表是 %q：段内必须与注册表同序（新增版本只会追加在段尾）", label, i+1, got[i], all[i])
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

func TestBaselineContentSHA256Shape(t *testing.T) {
	sum := baseline.ContentSHA256()
	if len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
		t.Fatalf("内容摘要形状不对：%q", sum)
	}
}

// 摘要必须覆盖结构/系统数据内容，而不是只覆盖 manifest：
// 只比清单的话，基线 SQL 内容变了仍会被判定「已覆盖」，跳过的迁移永远不会补上。
func TestBaselineContentSHA256CoversArtifacts(t *testing.T) {
	manifestSum := sha256.Sum256([]byte(baseline.ManifestText()))
	if baseline.ContentSHA256() == hex.EncodeToString(manifestSum[:]) {
		t.Fatal("内容摘要等于 manifest 摘要：结构/系统数据没有参与计算")
	}
}

// init.sql 里记的内容摘要必须与 ContentSHA256 一致：生成器（shell）与迁移器（Go）各算一遍，
// 口径一旦漂移，接入后的库就永远匹配不上、每次回落全量。这条测试把两侧钉在一起。
func TestBaselineInitMatchesContentDigest(t *testing.T) {
	raw, err := os.ReadFile("../../schema/baseline/init.sql")
	if err != nil {
		t.Fatalf("读 init.sql：%v", err)
	}
	re := regexp.MustCompile(`VALUES \('(baseline-[0-9a-f]{8})', '([0-9a-f]{64})'`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("init.sql 里没有 baseline-<8hex> / <64hex> 水位行")
	}
	want := baseline.ContentSHA256()
	if m[2] != want {
		t.Fatalf("init.sql 记录的内容摘要 %s 与 ContentSHA256() %s 不一致：生成器与迁移器的摘要口径已漂移", m[2], want)
	}
	if m[1] != "baseline-"+want[:8] {
		t.Fatalf("init.sql 版本号 %s 与摘要前 8 位 %s 不一致", m[1], want[:8])
	}
}

// 时间列的值是生成时刻的 now()，必须在摘要前归一，否则每次重生成摘要都变。
func TestBaselineNormalizeTimestamps(t *testing.T) {
	cases := map[string]string{
		"2026-11-15 08:00:00.123456+08":     "<TS>",
		"2026-11-15 08:00:00+08":            "<TS>",
		"2026-11-15T08:00:00+08:00":         "2026-11-15T08:00:00+08:00",
		"COPY 行里的 2026-11-15 08:00:00+08 值": "COPY 行里的 <TS> 值",
	}
	for in, want := range cases {
		if got := baseline.NormalizeTimestamps(in); got != want {
			t.Fatalf("归一 %q 得到 %q，期望 %q", in, got, want)
		}
	}
}
