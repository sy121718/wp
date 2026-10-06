// raw_sql_boundary_test.go —— 裸 SQL 边界（AGENTS.md「数据库」节）。
//
// 判据：`internal/module/` 下的**生产代码**（不含 `_test.go`）不得出现手写整句 SQL，
// 也就是 gorm 的 `Raw(` / `Exec(` 调用。列名与表名一旦写成字符串，改一列不会有编译错误 ——
// 它只会在运行时静默读到空值，或报一个离拼串处很远的 PG syntax error；
// `selectExpr` 这类参数守卫也只在链式路径上生效。
//
// 两处例外来源：
//   - rawSQLAllowedList：设计上就不该 GORM 化的位置（PG 系统表、GORM 无对应能力的语句）；
//   - rawSQLDebtList：待还存量。**登记的是此刻的处数**，整改完必须把条目删掉。
//
// 两个方向都会失败：处数变多（新写的裸 SQL），以及处数变少却没同步改表（漏删条目）——
// 「只减不增」的账要能自己对平，否则登记表会退化成一句没人维护的注释。
//
// 不扫 `pkg/`（i18n 内容表、rls 的 set_config 等）：那是基础设施层的数据访问实现，
// 其中 `set_config` 本身就是 RLS 机制的一部分，没有 Entity 可映射。本次整改的范围是
// 业务模块（「不准写裸 SQL，必须走 gorm」）。
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

// rawSQLEntry 一条裸 SQL 登记：文件（仓库根相对路径，斜杠分隔）+ 允许的**累计处数** + 依据。
type rawSQLEntry struct {
	File   string
	Max    int
	Reason string
}

// rawSQLAllowedList 有理由**保留**手写 SQL 的位置（设计上就不该 GORM 化）。
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
}

// rawSQLDebtList 待还的存量：登记「此刻的处数」，改完必须把条目删掉（处数与登记不符即失败）。
//
// 这些不是「允许」，是**还没做**。整改顺序按模块推进；每改完一个文件就删掉它这一条。
var rawSQLDebtList = []rawSQLEntry{
	{File: "internal/module/admin/model/menu_model.go", Max: 2},
	{File: "internal/module/ai/model/ai_call_log_model.go", Max: 2},
	{File: "internal/module/ai/model/ai_session_model.go", Max: 2},
	{File: "internal/module/ai/model/ai_tool_idempotency_model.go", Max: 2},
	{File: "internal/module/analytics/model/analytics_rollup_model.go", Max: 3},
	{File: "internal/module/build/model/build_model.go", Max: 2},
	{File: "internal/module/contenttemplate/model/contenttemplate_model.go", Max: 1},
	{File: "internal/module/mail/model/mail_account_model.go", Max: 2},
	{File: "internal/module/mail/model/mail_marketing_model.go", Max: 5},
	{File: "internal/module/presentation/model/presentation_mode_model.go", Max: 1},
	{File: "internal/module/presentation/model/presentation_model.go", Max: 4},
	{File: "internal/module/presentation/model/presentation_publication_model.go", Max: 1},
}

// TestNoNewRawSQL 守住 internal/module 下的裸 SQL 只减不增。
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

// rawSQLLines 返回文件里 gorm 的 `Raw(` / `Exec(` 调用所在行号（1 起，按出现顺序）。
//
// 用 AST 而不是正则：`RawSQL` `Execute` 这类前缀相同的标识符不会被误判，
// 注释与字符串里的同名字样也不会命中。
//
// 只认 **CallExpr**（真正的调用）：`r.Index.Raw` 这种「字段名恰好叫 Raw」的读法
// 与调用无关，实测在 page/service/page_seo_patrol.go 里出现过，宽判据会把它算进去。
//
// 再排除「接收者最后一段是 `m` 的调用」：本仓的 model 会把 schema DDL 包装成
// `func (m *Model) Exec(ctx, sql string) error`，service 侧写成 `s.m.Exec(ctx, dropSchemaSQL(id))`
// —— 那是**本模块自己的方法**，不是 gorm 的 `Exec`。gorm 路径上的接收者要么是
// 句柄变量（`tx.Exec(sql)`），要么是链式调用（`m.db.WithContext(ctx).Raw(sql)` 的接收者
// 是 WithContext 的调用结果），都不会以 `.m` 结尾。
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

// lastSelectorIs 报告 expr 的最外层是否为 `x.<name>` 形式的字段/方法访问。
func lastSelectorIs(expr ast.Expr, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}
