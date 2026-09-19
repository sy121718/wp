package migrations

// register_retired_permission_test.go — 守「删能力必须连 seed 一起收口」这条线。
//
// 背景（2026-09 实测到的一次真实故障，不是假想）：
//
//   104 的 seed 里有 inventory:cache_sync / inventory:cache_reconcile 两个权限点，
//   库存缓存下线后由迁移 122 负责从存量库删掉它们。结果库里**一直有**这两个死权限点，
//   指向的 /api/inventory/cache/* 路由早就不存在了（后台勾选后毫无作用，误导配置者）。
//
//   原因是两个台账的执行顺序：runAll 先跑 **Migrations**（122 在里面，把两行删掉），
//   然后 RunSeeds 跑 **Seeds**（104 在里面）。而 104 的幂等条件是「本票 10 个权限点齐了
//   才跳过」—— 122 删掉 2 个之后条件立刻不满足，于是每轮启动都把死权限点重新插回来。
//   即：**一个在 Migrations 台账里做的删除，永远赢不过在 Seeds 台账里重建它的 seed。**
//
// 判据：任一迁移 SQL 里删除的权限点代码，都不得再出现在任何 seed 的 SQL 里。
//
// 这是**启发式**：只认 `permission_code IN ('a', 'b')` 这一种写法。子查询、LIKE、
// 按 api_path 删除都会漏 —— 漏掉不代表没问题，只代表这条测试看不见（与仓库其它
// 静态扫描门禁同一取舍：绿不等于没问题，红了则一定有）。
//
// 为什么放在注册表旁边：本测试的判据就是「注册台账的 SQL 文本」，与 register_registry_test.go
// 同源 —— 挤进 public/test 反而要额外把嵌入的 SQL 再读一遍。

import (
	"regexp"
	"strings"
	"testing"
)

// deletedPermCodesRe 匹配 DELETE 语句里的 `permission_code IN (...)`，捕获括号内容。
var deletedPermCodesRe = regexp.MustCompile(`(?is)permission_code\s+IN\s*\(([^)]*)\)`)

// permCodeLiteralRe 从括号内容里取出 'module:action' 形状的字面量。
// 限定两段小写下划线：它同时排除了 api_path 字面量（含斜杠）与普通文案。
var permCodeLiteralRe = regexp.MustCompile(`'([a-z0-9_]+:[a-z0-9_]+)'`)

// sqlLineCommentRe 剥掉 SQL 行注释（`--` 到行尾）。
// 必须剥：注释里提到一个已退役的码是**正当文档**（本包 104 的修正说明就是），
// 不剥会把它误判成「seed 还在写这个码」，那种假红会让人把测试关掉。
// 局限：不处理字符串字面量里的 `--`（本仓库的迁移 SQL 没有这种写法）。
var sqlLineCommentRe = regexp.MustCompile(`(?m)--[^\n]*`)

// stripSQLLineComments 去注释。
func stripSQLLineComments(sql string) string {
	return sqlLineCommentRe.ReplaceAllString(sql, "")
}

// retiredPermViolation 一条违规：某个 seed 仍在写一个已被删除的权限点码。
type retiredPermViolation struct {
	SeedVersion string
	TableName   string
	Code        string
	DeletedBy   string
}

// collectRetiredPermCodes 从迁移列表里收集「被删除的权限点码 → 删除它的迁移版本」。
// 纯函数：输入是迁移列表，不碰全局台账 —— 这样它自己能被测（见本文件下半的合成样本）。
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

// findReseededRetiredCodes 找出「仍在写入已退役权限点」的 seed。
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

// TestRetiredPermissionCodesAreNotReseeded 删除过的权限点不得被任何 seed 重新写入。
func TestRetiredPermissionCodesAreNotReseeded(t *testing.T) {
	retired := collectRetiredPermCodes(All())
	if len(retired) == 0 {
		// 判据失效必须是**失败**而不是跳过：写个永远为空的断言等于没有门禁。
		// 若将来确实不再有任何「删除权限点」的迁移，请把这条测试与它的判据一起删掉，
		// 而不是留着让它静默通过。
		t.Fatalf("没有从任何迁移里解析出被删除的权限点 —— 判据（正则或台账）已失效，需复核")
	}

	// ② 任何 seed 都不许再写入这些码。
	for _, v := range findReseededRetiredCodes(AllSeeds(), retired) {
		t.Errorf("seed %s（表 %s）仍在写入权限点 %q —— 它已被迁移 %s 删除；\n"+
			"    在 Seeds 台账里重建它会让那次删除每轮启动都被撤回（Migrations 先跑、Seeds 后跑）。\n"+
			"    删能力时要连 seed 一起收口（条件计数与 SQL 同批改）。",
			v.SeedVersion, v.TableName, v.Code, v.DeletedBy)
	}
}

// TestRetiredPermCodeDetector 判据自己的「命中分支」自检。
//
// 为什么必须有：本文件另一条测试在**修好之后**是绿的 —— 只靠它，没人知道它是不是
// 恰好什么都没抓到（正则写坏、台账改名、注释剥多了都会让它静默变绿）。
// 这里用合成的坏样本（复刻 122 删 / 104 重建的真实形状）证明它**真的会红**。
// 仓库里 check-no-internal-error-leak.sh 的豁免清单同此取舍：门禁本身也要被测试。
func TestRetiredPermCodeDetector(t *testing.T) {
	t.Run("抓到 seed 重建已删除的权限点", func(t *testing.T) {
		// 完全复刻故障形状：122 删两个码，104 的 seed 又写回去。
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
		// 注释是正当文档（104 的修正说明就写着那两个码）。不剥注释会假红，
		// 而假红的下场是这个门禁被关掉。
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
		// 按 api_path 删除（本仓库 122 的第一段就是这个形状）不该被误读成权限点码。
		migs := []Migration{{
			Version: "x",
			SQL:     "DELETE FROM sys_casbin_rule WHERE v1 = '/api/inventory/cache/sync' AND v2 = 'POST';\n",
		}}
		if got := collectRetiredPermCodes(migs); len(got) != 0 {
			t.Fatalf("不含 DELETE FROM sys_permission 的迁移不该产生退役码，实际 %v", got)
		}
	})
}
