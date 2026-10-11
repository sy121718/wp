package utils

import "strings"

// JsonSafe escapes script closing sequences and HTML comment openings in JSON
// data islands. It preserves the legacy byte substitutions, including the
// comment escape; it does not validate or re-encode JSON.
func JsonSafe(s string) string {
	s = strings.ReplaceAll(s, "</", "<\\/")
	s = strings.ReplaceAll(s, "<!--", "<\\!--")
	return s
}
