package pipeline

// lang.go — 多语言访问路径映射（docs/06-D §5 方案 A'）。
//
// 语言是 BuildContext 的维度：同一 Page Document 在 N 个语言下做 N 次独立构建，
// 各自独立 hash、独立激活、独立回滚（docs/06-D §2.2）。因此「逻辑路径 → 访问路径」
// 的映射必须单点实现，禁止调用方各自拼 "/" + lang + path。
//
// 方案 A'（默认语言无前缀 + 非默认语言短码）：
//   - 默认语言 → 无前缀：/about、/index（首页）；
//   - 非默认语言 → /{短码}/about、/{短码}/index。
//
// 内部逻辑一律使用完整语言码（zh-CN / en-US），URL 路径段使用短码（zh / en）：
// 映射表内置（builtinURLCodes）+ 配置覆盖（LangURLRule.Codes，来自
// i18n.lang_url_codes）+ 确定性回退（主语言子标签小写，如 fr-CA → fr）。
//
// 为什么语言根不占 /{code}（§4.4 硬坑，实测确认）：page_routes 只做精确路径唯一、
// 无父子前缀互斥；LocalPublicationStore.Activate 对 /{code}/about 会先 MkdirAll
// 父目录，若 /{code} 已是「指向产物目录的符号链接」，MkdirAll 会跟随该链接在
// 不可变产物目录内部建目录，随后落地的符号链接进入 artifacts/{hash}，既污染
// 不可变产物又因上溯层数错算成为悬空链接。改为 /{code}/index 后语言根与同语言
// 子路径是兄弟节点，冲突面彻底消失；默认语言首页同理映射为 /index。

import (
	"fmt"
	"sort"
	"strings"
)

// maxLangLen 语言代码长度上限（BCP 47 常见形如 zh-CN / en-US / zh-Hans-CN）。
const maxLangLen = 35

// NormalizeLang 规范化并校验语言代码。
//
// 语言码会作为数据库列值与（映射后的）URL 路径段使用，因此必须是严格白名单：
// 字母、数字与连字符，长度 ≤ 35；空串、含 "." / "/" / "" / 空格 / 控制字符
// 一律拒绝（防路径穿越与保留路径污染）。
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

// builtinURLCodes 内置「完整语言码 → URL 短码」映射表。
//
// 只收录存在业界共识短码的常见语言；未收录的语言走确定性回退（主语言子标签）。
// 表内不出现同一短码对应两个语言码（zh-CN → zh，zh-TW → zh-tw），
// 否则需由 i18n.lang_url_codes 显式区分。
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

// LangURLRule 站点语言访问路径规则——「逻辑路径 ↔ 访问路径」的唯一映射点。
//
// Separated=false 时全部语言共用逻辑路径（单语言兼容，后发布者覆盖线上内容）；
// Separated=true 时按 PrefixDefault 决定默认语言是否也带前缀。
type LangURLRule struct {
	// Separated 是否按语言区分访问路径。
	Separated bool
	// DefaultLang 站点默认语言（完整码）；空 = 不识别默认语言（则所有语言都带前缀）。
	DefaultLang string
	// PrefixDefault 默认语言是否也带语言前缀。
	PrefixDefault bool
	// Codes 语言码 → URL 短码的配置覆盖表（可空）。
	Codes map[string]string
}

// NewLangURLRule 构造规则（配置层调用；纯数据，不做 IO）。
func NewLangURLRule(separated, prefixDefault bool, defaultLang string, codes map[string]string) LangURLRule {
	return LangURLRule{
		Separated:     separated,
		DefaultLang:   strings.TrimSpace(defaultLang),
		PrefixDefault: prefixDefault,
		Codes:         codes,
	}
}

// legacyAllPrefixRule 历史「全语言带前缀」规则（LangPath / StripLangPath 的语义）。
func legacyAllPrefixRule() LangURLRule {
	return LangURLRule{Separated: true, PrefixDefault: true}
}

// lookupCode 在映射表中查短码：先精确匹配，再大小写不敏感匹配（键排序取首个，
// 保证同一配置的映射结果确定）。
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

// URLCode 完整语言码 → URL 短码（配置覆盖 → 内置表 → 主语言子标签 → 整码小写）。
//
// 回退是纯函数：fr-CA 未收录 → "fr"；单个 "en" → "en"；"xx-YY" → "xx"。
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

// isDefault 判断是否站点默认语言（大小写不敏感；DefaultLang 为空时恒 false）。
func (r LangURLRule) isDefault(lang string) bool {
	d := strings.TrimSpace(r.DefaultLang)
	return d != "" && strings.EqualFold(d, lang)
}

// normalizeLogical 逻辑路径规范化（补前导斜杠；保留 "/"）。
func normalizeLogical(path string) (string, error) {
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return NormalizeURL(path)
}

// Path 逻辑访问路径 → 该语言的访问路径（本包唯一映射点）。
//
//	rule{Separated:true, DefaultLang:"zh-CN"}:
//	  Path("zh-CN", "/")      → "/index"
//	  Path("zh-CN", "/about") → "/about"
//	  Path("en-US", "/")      → "/en/index"
//	  Path("en-US", "/about") → "/en/about"
//	rule{Separated:true, PrefixDefault:true}:
//	  Path("zh-CN", "/about") → "/zh/about"
//	rule{Separated:false}:
//	  Path("en-US", "/about") → "/about"
//
// 返回值经 NormalizeURL 校验（拒绝保留路径、路径穿越、超长、含空格）。
// 语言或路径非法时返回 error——构建期即失败，绝不产出半截前缀的路径。
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
		// 默认语言无前缀；首页语言根映射 /index（与带前缀语言同形，见文件头）。
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

// Strip Path 的逆运算：该语言的访问路径 → 逻辑路径。
//
// 用于「访问路径 → 草稿逻辑路径」的反查（路由行、诊断信息展示、导航高亮）；
// 未带本语言前缀时原样返回（规范化后），语言或路径非法时返回原输入。
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

// Locate 判断访问路径属于哪个启用语言，并返回其逻辑路径（sitemap 分组 / 导航反查）。
//
// 仅 Separated 时可用；未带任何已知语言前缀的路径在「默认语言无前缀」方案下
// 归属默认语言（这正是 /about 与 /en/about 能互相分组的原因）。返回 ok=false
// 表示该路径不参与语言分组。
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
			continue // 默认语言不带前缀，不参与前缀匹配
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

// Validate 校验启用语言在 URL 层是否互斥：两种语言映射到同一短码时必须显式
// 配置 i18n.lang_url_codes（否则 page_routes 的 (project_id, path) 唯一键会撞车，
// 或后构建者静默覆盖前者）。默认语言在无前缀方案下不占短码，跳过。
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
			return fmt.Errorf("语言 %s 与 %s 映射到同一 URL 短码 %q：请在 i18n.lang_url_codes 显式区分", prev, l, code)
		}
		seen[code] = l
	}
	return nil
}

// LangPath 历史兼容入口：全语言带短码前缀的 Path（等价 legacyAllPrefixRule）。
//
//	LangPath("zh-CN", "/")      → "/zh/index"
//	LangPath("zh-CN", "/about") → "/zh/about"
//
// 新代码请按站点方案构造 LangURLRule 后调用 rule.Path（见装配层 sitePath）。
func LangPath(lang, path string) (string, error) {
	return legacyAllPrefixRule().Path(lang, path)
}

// StripLangPath 历史兼容入口：全语言带短码前缀的 Strip（见 LangPath）。
func StripLangPath(lang, path string) string {
	return legacyAllPrefixRule().Strip(lang, path)
}
