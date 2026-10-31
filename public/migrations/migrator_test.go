package migrations

import (
	"slices"
	"strings"
	"testing"
)

func TestMigrationRegistryHasUniqueVersions(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
}

func TestCustomMigrationChecksParameterConvention(t *testing.T) {
	for _, m := range allMigrations {
		if m.CheckSQL == "" {
			continue
		}
		if n := strings.Count(m.CheckSQL, "?"); n > 1 {
			t.Errorf("迁移 %s 的 CheckSQL 含 %d 个 `?`：迁移器只传表名一个参数，"+
				"其余判定值必须写进 SQL 字面量（178 踩过：判定恒为 0、每次启动重跑）", m.Version, n)
		}
	}
}

func TestCompareVersionNumericSort(t *testing.T) {
	versions := []string{"999_foo", "1000_bar", "154_baz", "001_init"}
	slices.SortFunc(versions, func(a, b string) int { return compareVersionCmp(a, b) })
	want := []string{"001_init", "154_baz", "999_foo", "1000_bar"}
	for i, v := range want {
		if versions[i] != v {
			t.Fatalf("排序[%d]: 期望 %s，得到 %s（完整 %v）", i, v, versions[i], versions)
		}
	}
}

func TestParseVersionPrefixRejectsBadName(t *testing.T) {
	if _, _, err := parseVersionPrefix("abc_name"); err == nil {
		t.Fatal("非法前缀应报错")
	}

	if _, _, err := parseVersionPrefix("001init"); err == nil {
		t.Fatal("缺少分隔符的版本名应报错")
	}
}

func TestCompareVersionPatchSuffix(t *testing.T) {
	versions := []string{"087b_product_variant_options", "086b_product_attribute_menu", "087_product_variant_generate", "086_product_attribute", "086a_product_attribute_permissions", "088_product_taxonomy"}
	slices.SortFunc(versions, func(a, b string) int { return compareVersionCmp(a, b) })
	want := []string{
		"086_product_attribute",
		"086a_product_attribute_permissions",
		"086b_product_attribute_menu",
		"087_product_variant_generate",
		"087b_product_variant_options",
		"088_product_taxonomy",
	}
	for i, v := range want {
		if versions[i] != v {
			t.Fatalf("排序[%d]: 期望 %s，得到 %s（完整 %v）", i, v, versions[i], versions)
		}
	}
}

func TestSplitStatementsStillHandlesDollarQuotedBlocks(t *testing.T) {
	got := SplitStatements("CREATE TABLE x (id int); DO $$ BEGIN PERFORM 1; PERFORM 2; END $$; CREATE INDEX y ON x(id);")
	if len(got) != 3 {
		t.Fatalf("期望 3 条语句，得到 %d: %#v", len(got), got)
	}
}
