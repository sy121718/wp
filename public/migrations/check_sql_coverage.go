package migrations

import (
	"fmt"
	"strings"
)

// coverObjects 生成「SQL 里每个对象都已存在」的 CheckSQL。
//
// 迁移的 CheckSQL 是整条迁移的已应用判定：只查其中一个对象时，存量库只要恰好有
// 那个对象就会跳过整条迁移，同一份 SQL 里其它对象（尤其索引）永远不会补建。
// 实测：wp 库有 idx_pages_blockref，068 被判已应用，pages 的 structure_header /
// structure_footer 两个表达式索引从未创建。这里按对象名逐个 to_regclass 计数
// （索引也是 relation），全部存在才算已应用。
func coverObjects(names ...string) string {
	names = compactNames(names)
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("(CASE WHEN to_regclass('public.%s') IS NOT NULL THEN 1 ELSE 0 END)", name))
	}
	return fmt.Sprintf("SELECT CASE WHEN (%s) = %d THEN 1 ELSE 0 END", strings.Join(parts, " + "), len(names))
}

// allChecks 把多条「返回 1/0 的 SELECT」合成一条 CheckSQL。
// 用于「原判定条件只覆盖特征、还需要对象齐全」的迁移：任一条件不成立即视为未应用，
// 迁移会重跑（SQL 本身幂等）。
func allChecks(checks ...string) string {
	parts := make([]string, 0, len(checks))
	for _, check := range checks {
		check = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(check), ";"))
		if check == "" {
			continue
		}
		parts = append(parts, "("+check+")")
	}
	if len(parts) == 0 {
		return ""
	}
	return "SELECT CASE WHEN " + strings.Join(parts, " = 1 AND ") + " = 1 THEN 1 ELSE 0 END"
}

func compactNames(names []string) []string {
	out := names[:0]
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}
