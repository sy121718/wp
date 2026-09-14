package migrations

import (
	"sort"
	"strings"
	"testing"
)

func TestMigrationRegistryHasUniqueVersions(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
}

func TestCustomMigrationChecksAcceptTableParameter(t *testing.T) {
	for _, m := range allMigrations {
		if m.CheckSQL != "" && !strings.Contains(m.CheckSQL, "?") {
			t.Errorf("迁移 %s 的 CheckSQL 未接收迁移器传入的表名参数", m.Version)
		}
	}
}

func TestCompareVersionNumericSort(t *testing.T) {
	versions := []string{"999_foo", "1000_bar", "154_baz", "001_init"}
	sort.Slice(versions, func(i, j int) bool { return compareVersion(versions[i], versions[j]) })
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
	// 数字与名字之间必须有分隔符："001init" 不是合法版本号。
	if _, _, err := parseVersionPrefix("001init"); err == nil {
		t.Fatal("缺少分隔符的版本名应报错")
	}
}

// TestCompareVersionPatchSuffix 字母后缀是补丁位：同主编号下空后缀在前，字母按字典序。
func TestCompareVersionPatchSuffix(t *testing.T) {
	versions := []string{"087b_product_variant_options", "086b_product_attribute_menu", "087_product_variant_generate", "086_product_attribute", "086a_product_attribute_permissions", "088_product_taxonomy"}
	sort.Slice(versions, func(i, j int) bool { return compareVersion(versions[i], versions[j]) })
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
