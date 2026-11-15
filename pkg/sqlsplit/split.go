// Package sqlsplit 按语句边界切分 SQL 文本。
//
// 处理单引号（含双写单引号转义）、双引号、$$ / $tag$ 体、行注释与块注释；
// 返回的语句不含结尾分号。
package sqlsplit

import "strings"

// Split 把 SQL 文本切成可逐条执行的语句。
func Split(text string) []string {
	var out []string
	var b strings.Builder
	i := 0
	for i < len(text) {
		switch {
		case strings.HasPrefix(text[i:], "--"):
			nl := strings.IndexByte(text[i:], '\n')
			if nl < 0 {
				b.WriteString(text[i:])
				i = len(text)
				continue
			}
			b.WriteString(text[i : i+nl])
			i += nl
		case strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				b.WriteString(text[i:])
				i = len(text)
				continue
			}
			b.WriteString(text[i : i+2+end+2])
			i += 2 + end + 2
		case text[i] == '\'':
			j := i + 1
			for j < len(text) {
				if text[j] == '\'' {
					if j+1 < len(text) && text[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			b.WriteString(text[i:j])
			i = j
		case text[i] == '"':
			j := i + 1
			for j < len(text) {
				if text[j] == '"' {
					if j+1 < len(text) && text[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			b.WriteString(text[i:j])
			i = j
		case text[i] == '$':
			tagEnd := strings.IndexByte(text[i+1:], '$')
			if tagEnd < 0 {
				b.WriteByte(text[i])
				i++
				continue
			}
			tag := text[i : i+tagEnd+2]
			end := strings.Index(text[i+len(tag):], tag)
			if end < 0 {
				b.WriteString(text[i:])
				i = len(text)
				continue
			}
			b.WriteString(text[i : i+len(tag)+end+len(tag)])
			i += len(tag) + end + len(tag)
		case text[i] == ';':
			stmt := strings.TrimSpace(b.String())
			if stmt != "" {
				out = append(out, stmt)
			}
			b.Reset()
			i++
		default:
			b.WriteByte(text[i])
			i++
		}
	}
	if stmt := strings.TrimSpace(b.String()); stmt != "" {
		out = append(out, stmt)
	}
	return out
}
