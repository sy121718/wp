package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type rawSQLEntry struct {
	File   string
	Max    int
	Reason string
}

var rawSQLAllowedList = []rawSQLEntry{
	{
		File: "internal/module/plugin/model/plugin_model.go",
		Max:  3,
		Reason: "两处插件 schema DDL（CREATE / DROP SCHEMA）加上一处 PG 目录查询" +
			"（pg_namespace + pg_class，用途限定为「注册表里没有、库里还在」的孤儿 schema 对账）：" +
			"DDL 与系统表都没有 Entity 可映射",
	},
	{
		File: "internal/module/page/model/page_model.go",
		Max:  1,
		Reason: "PruneRevisions 的批量 DELETE 同时用到 ctid（物理行位置）、" +
			"row_number() OVER (PARTITION BY …) 与 DELETE … LIMIT，GORM 三者都没有对应能力",
	},
	{
		File: "internal/module/product/model/product_outbox_model.go",
		Max:  1,
		Reason: "NextOutboxRevisionTx 的 pg_advisory_xact_lock(hashtext(?))：" +
			"无表、无 Entity 可映射的 PG 顾问锁函数调用，GORM 没有对应表达能力",
	},
	{
		File: "internal/module/page/model/page_tx.go",
		Max:  2,
		Reason: "两处 pg_advisory_xact_lock(hashtext(?))：LockPageEntityAdvisoryTx（键 \"page:\"）" +
			"与 LockProjectEntityAdvisoryTx（键 \"project:\"）。无表、无 Entity 可映射的 PG 事务级" +
			"顾问锁，GORM 没有对应表达能力。" +
			"它补的是行锁取不到的那一段：页面被软删之后 LockPageForActivationTx 永远" +
			"返回 ErrRecordNotFound，而删除/退役遗留链接的撤销仍必须与正在进行的那次删除互斥；" +
			"工程级下线则根本没有实体行可锁（redirect 行的 page_id / presentation_id 都是 NULL）。" +
			"（与 presentation / product 的同类调用同一先例，各自带类型前缀的键）",
	},
	{
		File: "internal/module/presentation/model/presentation_model.go",
		Max:  1,
		Reason: "LockPresentationEntityAdvisoryTx 的 pg_advisory_xact_lock(hashtext(?))（键 \"presentation:\"）：" +
			"无表、无 Entity 可映射的 PG 事务级顾问锁，GORM 没有对应表达能力（GORM 的 Locking 只能挂在" +
			"表查询上，表达不了 SELECT 一个只返回 void 的函数）。锁随事务提交/回滚自动释放，" +
			"所以不需要、也不能用 Unlock 配对。" +
			"它补的是行锁取不到的那一段：实例行被 DeleteInstanceTx 物理删除后无行可锁，" +
			"而撤销其遗留访问面链接仍必须与正在进行的那次删除互斥（与 page / product 同类调用同一先例）",
	},
	{
		File: "internal/module/mail/model/mail_marketing_model.go",
		Max:  1,
		Reason: "BatchUpdateContactImportFields 的 UPDATE mail_contacts AS c SET … FROM (SELECT … FROM jsonb_array_elements(?::jsonb) AS r) AS v WHERE lower(c.email) = lower(v.email) RETURNING lower(c.email)：" +
			"GORM 没有 UPDATE … FROM 支持，而这条语句的三点语义都不能丢 —— " +
			"① 一条语句原子更新整批（方法注释里写明「为什么不再逐条」：N 次往返比 INSERT 更贵）；" +
			"② name / tags 用 COALESCE 只在本行给了非空值时覆盖；" +
			"③ 只更新**已存在**的行、不新增（改用 INSERT … ON CONFLICT 会把未命中的邮箱插进联系人表，语义就变了）",
	},
	{
		File: "internal/module/build/model/build_model.go",
		Max:  1,
		Reason: "ReclaimStale 的回收语句：CTE 里的候选 SELECT 带 FOR UPDATE SKIP LOCKED（锁的是候选行），" +
			"外层 UPDATE … FROM stale s 再按 CTE 的 has_pending 列**逐行**决定 status / started_at / completed_at / error_message。" +
			"GORM 无 CTE API，且 Updates 只能写统一值；把 CASE 表达式塞进值位置能表达分支，" +
			"但 UPDATE 上无法保留 SKIP LOCKED（PG 的 FOR UPDATE 只对 SELECT 有效）—— 去掉它就从「跳过被锁行」退化成「等锁」，语义不等价",
	},
}

var rawSQLDebtList = []rawSQLEntry{}

func TestNoNewRawSQL(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "internal", "module")

	got := map[string][]int{}
	werr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		lines, perr := rawSQLLines(path)
		if perr != nil {
			return perr
		}
		if len(lines) == 0 {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		got[filepath.ToSlash(rel)] = lines
		return nil
	})
	if werr != nil {
		t.Fatalf("扫描 %s 失败：%v", base, werr)
	}

	want := map[string]rawSQLEntry{}
	for _, e := range rawSQLAllowedList {
		if _, dup := want[e.File]; dup {
			t.Fatalf("登记表里有重复条目：%s", e.File)
		}
		want[e.File] = e
	}
	for _, e := range rawSQLDebtList {
		if _, dup := want[e.File]; dup {
			t.Fatalf("%s 同时出现在 allowed 与 debt 两张表里", e.File)
		}
		want[e.File] = e
	}

	var problems []string
	for file, lines := range got {
		e, ok := want[file]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"%s:%d 有 %d 处裸 SQL，但不在登记表里 —— 新写的裸 SQL 请改走 GORM 链式；"+
					"确实无法 GORM 化的，加进 rawSQLAllowedList 并写明理由", file, lines[0], len(lines)))
			continue
		}
		if len(lines) != e.Max {
			problems = append(problems, fmt.Sprintf(
				"%s 登记 %d 处、实际 %d 处（行 %v）：改少了下调 Max 并从表里删条目，改多了说明新写了裸 SQL",
				file, e.Max, len(lines), lines))
		}
	}
	for file, e := range want {
		if _, ok := got[file]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s 登记了 %d 处但已经一处都没有 —— 请删掉这条登记（%s）", file, e.Max, e.Reason))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("裸 SQL 登记表与实际不符（共 %d 条）：\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

func rawSQLLines(path string) ([]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败：%w", path, err)
	}
	var lines []int
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Raw" && sel.Sel.Name != "Exec" {
			return true
		}
		if lastSelectorIs(sel.X, "m") {
			return true
		}
		lines = append(lines, fset.Position(sel.Pos()).Line)
		return true
	})
	return lines, nil
}

func lastSelectorIs(expr ast.Expr, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}
