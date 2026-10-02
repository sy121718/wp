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

// TestCustomMigrationChecksParameterConvention 自定义 CheckSQL 的参数约定。
//
// **这条断言原先写反了**：它要求「每条自定义 CheckSQL 都必须含 `?`」，而 migrator.apply
// 的实现与注释明说——只在 SQL 里真的出现 `?` 时才把表名传进去，因为按
// `pg_constraint` / `pg_indexes` / `information_schema.columns` 判定对象是否存在的语句
// **根本不接受参数**（无条件传参会直接报 `expected 0 arguments, got 1`）。
// 474 的 CheckSQL 就是按列数判断、不含 `?` 的那种，它是**合法**写法。
//
// 所以真正成立的判据是：
//  1. 含 `?` 时必须**恰好一个** —— 迁移器只传表名一个参数，多一个就是参数个数不符；
//  2. 每条自定义 CheckSQL 都要能被迁移器**那样**执行（含 `?` 传表名、不含则不传）——
//     这条需要真库，见 migrator_check_sql_test.go。
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
