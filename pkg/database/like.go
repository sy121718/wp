package database

import "strings"

// EscapeLikePattern 转义 LIKE / ILIKE 通配符（\ % _），使关键字按字面匹配。
//
// 与 SQL 的 ESCAPE '\' 子句配套使用，例如：
//
//	pattern := "%" + database.EscapeLikePattern(keyword) + "%"
//	db.Where("name LIKE ? ESCAPE '\\'", pattern)
//
// 转义顺序不能反：必须先转义反斜杠本身，否则后两步插入的反斜杠会被再次转义。
func EscapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
