package migrations

import (
	"regexp"
	"strings"
	"testing"
)

var deletedPermCodesRe = regexp.MustCompile(`(?is)permission_code\s+IN\s*\(([^)]*)\)`)

var permCodeLiteralRe = regexp.MustCompile(`'([a-z0-9_]+:[a-z0-9_]+)'`)

var sqlLineCommentRe = regexp.MustCompile(`(?m)--[^\n]*`)

func stripSQLLineComments(sql string) string {
	return sqlLineCommentRe.ReplaceAllString(sql, "")
}

type retiredPermViolation struct {
	SeedVersion string
	TableName   string
	Code        string
	DeletedBy   string
}

func collectRetiredPermCodes(migs []Migration) map[string]string {
	retired := map[string]string{}
	for _, m := range migs {
		sql := stripSQLLineComments(m.SQL)
		if !strings.Contains(strings.ToUpper(sql), "DELETE FROM SYS_PERMISSION") {
			continue
		}
		for _, group := range deletedPermCodesRe.FindAllStringSubmatch(sql, -1) {
			for _, lit := range permCodeLiteralRe.FindAllStringSubmatch(group[1], -1) {
				retired[lit[1]] = m.Version
			}
		}
	}
	return retired
}

func findReseededRetiredCodes(seeds []Seed, retired map[string]string) (out []retiredPermViolation) {
	for _, s := range seeds {
		sql := stripSQLLineComments(s.SQL)
		for code, deletedBy := range retired {
			if !strings.Contains(sql, "'"+code+"'") {
				continue
			}
			out = append(out, retiredPermViolation{
				SeedVersion: s.Version, TableName: s.TableName, Code: code, DeletedBy: deletedBy,
			})
		}
	}
	return out
}

func TestRetiredPermissionCodesAreNotReseeded(t *testing.T) {
	retired := collectRetiredPermCodes(All())
	if len(retired) == 0 {

		t.Fatalf("没有从任何迁移里解析出被删除的权限点 —— 判据（正则或台账）已失效，需复核")
	}

	for _, v := range findReseededRetiredCodes(AllSeeds(), retired) {
		t.Errorf("seed %s（表 %s）仍在写入权限点 %q —— 它已被迁移 %s 删除；\n"+
			"    在 Seeds 台账里重建它会让那次删除每轮启动都被撤回（Migrations 先跑、Seeds 后跑）。\n"+
			"    删能力时要连 seed 一起收口（条件计数与 SQL 同批改）。",
			v.SeedVersion, v.TableName, v.Code, v.DeletedBy)
	}
}

func TestRetiredPermCodeDetector(t *testing.T) {
	t.Run("抓到 seed 重建已删除的权限点", func(t *testing.T) {

		migs := []Migration{{
			Version: "122-drop-cache-permissions",
			SQL: "DELETE FROM sys_casbin_rule WHERE v1 = '/api/inventory/cache/sync';\n" +
				"DELETE FROM sys_permission\n" +
				" WHERE permission_code IN ('inventory:cache_sync', 'inventory:cache_reconcile');\n",
		}}
		retired := collectRetiredPermCodes(migs)
		if len(retired) != 2 {
			t.Fatalf("应从 IN 列表里解析出 2 个码，实际 %v（api_path 字面量不该被误认成权限点码）", retired)
		}
		if retired["inventory:cache_sync"] != "122-drop-cache-permissions" {
			t.Fatalf("应记下删除它的迁移版本，实际 %q", retired["inventory:cache_sync"])
		}
		seeds := []Seed{{
			Version: "104-inventory-change-permissions", TableName: "sys_permission",
			SQL: "INSERT INTO sys_permission (permission_code) SELECT v.code FROM (VALUES\n" +
				"    ('inventory:bom_get'), ('inventory:cache_sync')\n" +
				") AS v(code);\n",
		}}
		got := findReseededRetiredCodes(seeds, retired)
		if len(got) != 1 || got[0].Code != "inventory:cache_sync" || got[0].SeedVersion != "104-inventory-change-permissions" {
			t.Fatalf("应抓到 104 重建了 inventory:cache_sync，实际 %+v", got)
		}
	})

	t.Run("注释里提一个已退役的码不算违规", func(t *testing.T) {

		retired := map[string]string{"inventory:cache_sync": "122-drop-cache-permissions"}
		seeds := []Seed{{
			Version: "104-inventory-change-permissions",
			SQL:     "-- 原先还有 inventory:cache_sync，随缓存下线已移除\nINSERT INTO t (c) VALUES (1);\n",
		}}
		if got := findReseededRetiredCodes(seeds, retired); len(got) != 0 {
			t.Fatalf("注释里的提及不该算违规，实际 %+v", got)
		}
	})

	t.Run("只删不建的迁移不产生退役码", func(t *testing.T) {

		migs := []Migration{{
			Version: "x",
			SQL:     "DELETE FROM sys_casbin_rule WHERE v1 = '/api/inventory/cache/sync' AND v2 = 'POST';\n",
		}}
		if got := collectRetiredPermCodes(migs); len(got) != 0 {
			t.Fatalf("不含 DELETE FROM sys_permission 的迁移不该产生退役码，实际 %v", got)
		}
	})
}
