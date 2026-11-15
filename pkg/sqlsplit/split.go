// Package sqlsplit 按语句边界切分 SQL 文本。
//
// 处理单引号（含 ” 转义）、双引号、$$ 体、行注释与块注释；返回的语句不含结尾分号。
package sqlsplit

import "strings"

// Split 把 SQL 文本切成可逐条执行的语句。
func Split(sqlText string) []string {
	var stmts []string
	var current strings.Builder
	appendCurrent := func() {
		s := strings.TrimSpace(current.String())
		s = strings.TrimSuffix(s, ";")
		if s != "" {
			stmts = append(stmts, s)
		}
		current.Reset()
	}

	i := 0
	for i < len(sqlText) {
		rest := sqlText[i:]
		switch {
		case strings.HasPrefix(rest, "$$"):
			end := strings.Index(rest[2:], "$$")
			if end < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:2+end+2])
			i += 2 + end + 2
		case strings.HasPrefix(rest, "--"):
			nl := strings.IndexByte(rest, '\n')
			if nl < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:nl])
			i += nl
		case rest[0] == '\'':
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:end+2])
			i += end + 2
		default:
			ch := rest[0]
			current.WriteByte(ch)
			i++
			if ch == ';' {
				appendCurrent()
			}
		}
	}
	appendCurrent()
	return stmts
}
