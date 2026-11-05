package publish

import (
	"fmt"
	"sort"
	"strings"
)

const maxLangLen = 35

func NormalizeLang(raw string) (string, error) {
	lang := strings.TrimSpace(raw)
	if lang == "" {
		return "", fmt.Errorf("语言代码不能为空")
	}
	if len(lang) > maxLangLen {
		return "", fmt.Errorf("语言代码过长（最大 %d）: %q", maxLangLen, raw)
	}
	for _, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return "", fmt.Errorf("语言代码含非法字符 %q: %q", r, raw)
		}
	}
	return lang, nil
}

var builtinURLCodes = map[string]string{
	"zh": "zh", "zh-CN": "zh", "zh-Hans": "zh", "zh-SG": "zh",
	"zh-TW": "zh-tw", "zh-HK": "zh-hk", "zh-Hant": "zh-tw",
	"en": "en", "en-US": "en", "en-GB": "en-gb", "en-AU": "en-au", "en-CA": "en-ca",
	"ja": "ja", "ja-JP": "ja",
	"ko": "ko", "ko-KR": "ko",
	"fr": "fr", "fr-FR": "fr", "fr-CA": "fr-ca",
	"de": "de", "de-DE": "de", "de-AT": "de-at", "de-CH": "de-ch",
	"es": "es", "es-ES": "es", "es-MX": "es-mx",
	"pt": "pt", "pt-BR": "pt-br", "pt-PT": "pt-pt",
	"it": "it", "it-IT": "it",
	"ru": "ru", "ru-RU": "ru",
	"nl": "nl", "pl": "pl", "tr": "tr", "ar": "ar",
	"vi": "vi", "th": "th", "id": "id", "ms": "ms",
}

type LangURLRule struct {
	Separated bool

	DefaultLang string

	PrefixDefault bool

	Codes map[string]string
}

func NewLangURLRule(separated, prefixDefault bool, defaultLang string, codes map[string]string) LangURLRule {
	return LangURLRule{
		Separated:     separated,
		DefaultLang:   strings.TrimSpace(defaultLang),
		PrefixDefault: prefixDefault,
		Codes:         codes,
	}
}

func legacyAllPrefixRule() LangURLRule {
	return LangURLRule{Separated: true, PrefixDefault: true}
}

func lookupCode(table map[string]string, lang string) (string, bool) {
	if len(table) == 0 {
		return "", false
	}
	if v, ok := table[lang]; ok && strings.TrimSpace(v) != "" {
		return strings.ToLower(strings.TrimSpace(v)), true
	}
	keys := make([]string, 0, len(table))
	for k := range table {
		if strings.EqualFold(k, lang) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	v := strings.TrimSpace(table[keys[0]])
	if v == "" {
		return "", false
	}
	return strings.ToLower(v), true
}

func (r LangURLRule) URLCode(lang string) string {
	code := strings.TrimSpace(lang)
	if code == "" {
		return ""
	}
	if v, ok := lookupCode(r.Codes, code); ok {
		return v
	}
	if v, ok := lookupCode(builtinURLCodes, code); ok {
		return v
	}
	if i := strings.IndexAny(code, "-_"); i > 0 {
		return strings.ToLower(code[:i])
	}
	return strings.ToLower(code)
}

func (r LangURLRule) isDefault(lang string) bool {
	d := strings.TrimSpace(r.DefaultLang)
	return d != "" && strings.EqualFold(d, lang)
}

func normalizeLogical(path string) (string, error) {
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return NormalizeURL(path)
}

func (r LangURLRule) Path(lang, path string) (string, error) {
	base, err := normalizeLogical(path)
	if err != nil {
		return "", err
	}
	if !r.Separated {
		return base, nil
	}
	l, err := NormalizeLang(lang)
	if err != nil {
		return "", err
	}
	if !r.PrefixDefault && r.isDefault(l) {

		if base == "/" {
			return "/index", nil
		}
		return base, nil
	}
	if base == "/" {
		base = "/index"
	}
	return NormalizeURL("/" + r.URLCode(l) + base)
}

func (r LangURLRule) Strip(lang, path string) string {
	base, err := normalizeLogical(path)
	if err != nil {
		return path
	}
	if !r.Separated {
		return base
	}
	l, err := NormalizeLang(lang)
	if err != nil {
		return base
	}
	if !r.PrefixDefault && r.isDefault(l) {
		if base == "/index" {
			return "/"
		}
		return base
	}
	prefix := "/" + r.URLCode(l)
	if base == prefix || base == prefix+"/index" {
		return "/"
	}
	if strings.HasPrefix(base, prefix+"/") {
		return strings.TrimPrefix(base, prefix)
	}
	return base
}

func (r LangURLRule) Locate(path string, langs []string) (lang, logical string, ok bool) {
	base, err := normalizeLogical(path)
	if err != nil || !r.Separated {
		return "", "", false
	}
	for _, l := range langs {
		nl, lerr := NormalizeLang(l)
		if lerr != nil {
			continue
		}
		if !r.PrefixDefault && r.isDefault(nl) {
			continue
		}
		prefix := "/" + r.URLCode(nl)
		if base == prefix || base == prefix+"/index" {
			return nl, "/", true
		}
		if strings.HasPrefix(base, prefix+"/") {
			return nl, strings.TrimPrefix(base, prefix), true
		}
	}
	if !r.PrefixDefault && r.DefaultLang != "" {
		if _, derr := NormalizeLang(r.DefaultLang); derr == nil {
			if base == "/index" {
				return r.DefaultLang, "/", true
			}
			return r.DefaultLang, base, true
		}
	}
	return "", "", false
}

func (r LangURLRule) Validate(langs []string) error {
	if !r.Separated {
		return nil
	}
	seen := map[string]string{}
	for _, raw := range langs {
		l, err := NormalizeLang(raw)
		if err != nil {
			return err
		}
		if !r.PrefixDefault && r.isDefault(l) {
			continue
		}
		code := r.URLCode(l)
		if prev, ok := seen[code]; ok && !strings.EqualFold(prev, l) {
			return fmt.Errorf("语言 %s 与 %s 映射到同一 URL 短码 %q：请在系统配置的 i18n 组 lang_url_codes 里显式区分", prev, l, code)
		}
		seen[code] = l
	}
	return nil
}

func LangPath(lang, path string) (string, error) {
	return legacyAllPrefixRule().Path(lang, path)
}

func StripLangPath(lang, path string) string {
	return legacyAllPrefixRule().Strip(lang, path)
}
