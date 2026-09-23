package i18n

// langurl.go — 站点访问路径的语言方案 + 语言码 → URL 短码映射（docs/06-D §5）。
//
// 内部逻辑（sys_i18n / sys_translation / project_locales / BuildContext.lang /
// 产物 Manifest.lang / 数据库 lang 列）**一律使用完整语言码**（zh-CN / en-US）。
// 只有「URL 路径段」使用短码（zh / en）：短码可读、可分享、与常见静态站点惯例一致。
//
// 三种方案（配置 i18n.site_lang_url_mode）：
//   - default_plain（默认，推荐）：默认语言**无前缀**（/about、/index），
//     非默认语言用短码（/en/about、/en/index）；
//   - all_prefix：全语言带短码前缀（/zh/about、/en/about），历史 D1「全前缀」
//     方案的短码化形态；
//   - off：全语言共用逻辑路径（/about），单语言站点兼容形态。
//
// 兼容：旧键 i18n.site_lang_prefix（bool）仍被识别——true → all_prefix、
// false → off；两者同时存在时以 site_lang_url_mode 为准。
//
// 语言码映射：内置表（pipeline.builtinURLCodes）+ 配置覆盖
// （i18n.lang_url_codes，形如 zh-CN: zh）+ 确定性回退（主语言子标签小写，
// 如 fr-CA → fr；无子标签则整码小写）。映射是纯函数，保证确定性构建。

import (
	"fmt"
	"strings"
)

// SiteLangURLMode 站点访问路径的语言方案。
type SiteLangURLMode string

const (
	// SiteLangURLModeOff 全语言共用逻辑路径（单语言兼容）：/about。
	SiteLangURLModeOff SiteLangURLMode = "off"
	// SiteLangURLModeDefaultPlain 默认语言无前缀 + 非默认语言短码（默认方案）。
	SiteLangURLModeDefaultPlain SiteLangURLMode = "default_plain"
	// SiteLangURLModeAllPrefix 全语言带短码前缀（历史全前缀方案的短码化）。
	SiteLangURLModeAllPrefix SiteLangURLMode = "all_prefix"
)

// parseSiteLangURLMode 解析配置值；空串按默认 default_plain。
func parseSiteLangURLMode(raw string) (SiteLangURLMode, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return SiteLangURLModeDefaultPlain, nil
	case string(SiteLangURLModeOff):
		return SiteLangURLModeOff, nil
	case string(SiteLangURLModeDefaultPlain):
		return SiteLangURLModeDefaultPlain, nil
	case string(SiteLangURLModeAllPrefix):
		return SiteLangURLModeAllPrefix, nil
	default:
		return "", fmt.Errorf("i18n.site_lang_url_mode 取值非法: %q（可选 off / default_plain / all_prefix）", raw)
	}
}

// ParseSiteLangURLMode 解析并校验语言 URL 方案取值（站点设置页保存与启动恢复共用同一判据）：
// 空串按默认 default_plain，其余只接受 off / default_plain / all_prefix。
func ParseSiteLangURLMode(raw string) (SiteLangURLMode, error) {
	return parseSiteLangURLMode(raw)
}

// SiteLangURLModeValue 返回当前站点语言 URL 方案。
func SiteLangURLModeValue() SiteLangURLMode {
	initMu.Lock()
	defer initMu.Unlock()
	return siteLangURLMode
}

// SetSiteLangURLMode 运行时设置站点语言 URL 方案（测试与灰度使用）。
func SetSiteLangURLMode(mode SiteLangURLMode) {
	initMu.Lock()
	defer initMu.Unlock()
	siteLangURLMode = mode
}

// SiteLangURLsSeparated 报告站点是否按语言区分访问路径。
//
// off = false（各语言共用逻辑路径，后发布者覆盖线上内容）；
// default_plain / all_prefix = true（各语言有独立可寻址路径，可同时在线）。
func SiteLangURLsSeparated() bool {
	return SiteLangURLModeValue() != SiteLangURLModeOff
}

// SiteLangURLPrefixDefault 报告默认语言是否也带语言前缀（仅 all_prefix 为 true）。
func SiteLangURLPrefixDefault() bool {
	return SiteLangURLModeValue() == SiteLangURLModeAllPrefix
}

// SetURLCodeOverrides 运行时设置语言码 → URL 短码覆盖表（测试与运维灰度使用）。
// 传 nil 清空覆盖，回到「内置表 + 主语言子标签回退」。
func SetURLCodeOverrides(codes map[string]string) {
	initMu.Lock()
	defer initMu.Unlock()
	if len(codes) == 0 {
		langURLCodeOverrides = nil
		return
	}
	cp := make(map[string]string, len(codes))
	for k, v := range codes {
		cp[k] = v
	}
	langURLCodeOverrides = cp
}

// URLCodeOverrides 返回配置覆盖的语言码映射表（i18n.lang_url_codes，可能为空）。
func URLCodeOverrides() map[string]string {
	initMu.Lock()
	defer initMu.Unlock()
	if len(langURLCodeOverrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(langURLCodeOverrides))
	for k, v := range langURLCodeOverrides {
		out[k] = v
	}
	return out
}
