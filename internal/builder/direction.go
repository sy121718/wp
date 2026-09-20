package builder

// direction.go — 语言方向（LTR / RTL）元数据（审计 I18N-02）。
//
// 为什么需要一份「方向元数据」而不是在各处判 `lang == "ar"`：
// 产物要输出 `<html dir>`、Manifest 要记录方向、RTL 基座样式要按它生效，
// 三处判据必须**同源** —— 各写一份的判断迟早分叉，分叉的表现是
// 「Manifest 说 rtl、HTML 没有 dir」（或反过来），而两者都不会报错。
//
// 判据是语言标签本身（BCP-47）：主语言子标签在表内，或脚本子标签是 RTL 脚本。
// 表只收**默认书写方向为 RTL** 的语言：az / ha / ms / pa / so 这些默认 LTR、
// 仅特定脚本下才 RTL 的语言靠脚本子标签命中（如 az-Arab），不靠主标签 ——
// 把 az 整条标成 RTL 会让 az-Latn 的页面无端变成从右到左。

import "strings"

// rtlBaseSubtags 默认书写方向为 RTL 的主语言子标签（ISO 639-1/2/3 混用）。
//
// 收录口径：该语言的主流书写系统是 RTL。绝大多数条目有 RTL-only
// 的官方书写系统（ar 阿拉伯文、he 希伯来文、fa 波斯文…）；
// ku（库尔德语）取索拉尼书写传统，北部库尔德语（kmr-Latn）请显式写脚本子标签。
var rtlBaseSubtags = map[string]struct{}{
	"ar":  {}, // 阿拉伯语
	"arc": {}, // 阿拉米语
	"ckb": {}, // 中库尔德语（索拉尼）
	"dv":  {}, // 迪维希语
	"fa":  {}, // 波斯语
	"he":  {}, // 希伯来语
	"iw":  {}, // 希伯来语（历史标签）
	"ji":  {}, // 意第绪语（历史标签）
	"khw": {}, // 科瓦语
	"ks":  {}, // 克什米尔语
	"ku":  {}, // 库尔德语
	"mzn": {}, // 马赞德兰语
	"nqo": {}, // 西非书面语
	"prs": {}, // 达里语
	"ps":  {}, // 普什图语
	"sd":  {}, // 信德语
	"ug":  {}, // 维吾尔语
	"ur":  {}, // 乌尔都语
	"yi":  {}, // 意第绪语
}

// rtlScriptSubtags RTL 脚本子标签：出现即 RTL，与主语言子标签无关。
//
// 这条是给 LTR 语言下用 RTL 脚本的场景用的（az-Arab / ms-Arab 等），
// 也是给表里没收录的语言留的精确入口。
var rtlScriptSubtags = map[string]struct{}{
	"adlm": {}, // 富拉文
	"arab": {}, // 阿拉伯字母
	"hebr": {}, // 希伯来字母
	"mend": {}, // 门迪字母
	"nkoo": {}, // 西非书面字母
	"rohg": {}, // 哈乃斐罗兴亚字母
	"samr": {}, // 撒玛利亚字母
	"syrc": {}, // 叙利亚字母
	"thaa": {}, // 塔安那字母
	"yezi": {}, // 雅兹迪字母
}

// LocaleDirection 语言书写方向：返回 "rtl" 或 "ltr"。
//
// 空语言标签按 LTR 处理（未知语言不该无端镜像整页版面）。
func LocaleDirection(lang string) string {
	if IsRTLLocale(lang) {
		return "rtl"
	}
	return "ltr"
}

// IsRTLLocale 判定语言标签是否为 RTL（大小写不敏感；- 与 _ 都是合法分隔符）。
func IsRTLLocale(lang string) bool {
	parts := splitLocaleTag(lang)
	if len(parts) == 0 {
		return false
	}
	if _, ok := rtlBaseSubtags[parts[0]]; ok {
		return true
	}
	// 只看**脚本位**（BCP-47 里脚本恒为第二个子标签，如 az-Arab）。
	// 扫遍全部子标签会把扩展段里的 arab 也算进来：en-u-nu-arab（英文 + 阿拉伯数字）
	// 会被误判成整页 RTL —— 那是数字系统，不是书写方向。
	if len(parts) > 1 {
		if _, ok := rtlScriptSubtags[parts[1]]; ok {
			return true
		}
	}
	return false
}

// splitLocaleTag 把语言标签切成小写子标签；空标签返回 nil。
//
// 只切不规范化：这里要的是「哪个子标签命中表」，越境标签（如 "ar-EG-u-nu-arab"）
// 的多余部分命中不了任何表项，无需特殊处理。
func splitLocaleTag(lang string) []string {
	tag := strings.TrimSpace(lang)
	if tag == "" {
		return nil
	}
	return strings.FieldsFunc(strings.ToLower(tag), func(r rune) bool {
		return r == '-' || r == '_'
	})
}
