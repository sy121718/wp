package safepath

import "strings"

const MaxReturnPathBytes = 512

func IsSameOriginPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.Contains(p, "//") || strings.Contains(p, "\\") {
		return false
	}
	return !HasControlRune(p)
}

func HasControlRune(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func LangRedirectPath(raw string) string {
	if !IsSameOriginPath(raw) || len(raw) > MaxReturnPathBytes {
		return ""
	}
	return raw
}

func LocalReturnPath(raw string) string { return LangRedirectPath(raw) }
