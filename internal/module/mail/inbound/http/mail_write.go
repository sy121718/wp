package mailhttp

import (
	"strconv"
	"strings"
)

// mailIDOfText 把表单里的数字串转成无符号整数；空串或非数字返回 0。
func mailIDOfText(text string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// mailContactIDs 去掉空白与重复项，保持首次出现顺序。
func mailContactIDs(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
