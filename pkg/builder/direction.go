package builder

import "strings"

var rtlBaseSubtags = map[string]struct{}{
	"ar":  {},
	"arc": {},
	"ckb": {},
	"dv":  {},
	"fa":  {},
	"he":  {},
	"iw":  {},
	"ji":  {},
	"khw": {},
	"ks":  {},
	"ku":  {},
	"mzn": {},
	"nqo": {},
	"prs": {},
	"ps":  {},
	"sd":  {},
	"ug":  {},
	"ur":  {},
	"yi":  {},
}

var rtlScriptSubtags = map[string]struct{}{
	"adlm": {},
	"arab": {},
	"hebr": {},
	"mend": {},
	"nkoo": {},
	"rohg": {},
	"samr": {},
	"syrc": {},
	"thaa": {},
	"yezi": {},
}

func LocaleDirection(lang string) string {
	if IsRTLLocale(lang) {
		return "rtl"
	}
	return "ltr"
}

func IsRTLLocale(lang string) bool {
	parts := splitLocaleTag(lang)
	if len(parts) == 0 {
		return false
	}
	if _, ok := rtlBaseSubtags[parts[0]]; ok {
		return true
	}

	if len(parts) > 1 {
		if _, ok := rtlScriptSubtags[parts[1]]; ok {
			return true
		}
	}
	return false
}

func splitLocaleTag(lang string) []string {
	tag := strings.TrimSpace(lang)
	if tag == "" {
		return nil
	}
	return strings.FieldsFunc(strings.ToLower(tag), func(r rune) bool {
		return r == '-' || r == '_'
	})
}
