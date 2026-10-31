package utils

import "strings"

func JsonSafe(s string) string {
	s = strings.ReplaceAll(s, "</", "<\\/")
	s = strings.ReplaceAll(s, "<!--", "<\\!--")
	return s
}
