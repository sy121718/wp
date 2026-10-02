package i18n

// langurl.go — 站点访问路径的语言方案 + 语言码 → URL 短码映射（docs/06-D §5）。
//
// 内部逻辑（sys_i18n / sys_translation / project_locales / BuildContext.lang /
// 产物 Manifest.lang / 数据库 lang 列）**一律使用完整语言码**（zh-CN / en-US）。
// 只有「URL 路径段」使用短码（zh / en）：短码可读、可分享、与常见静态站点惯例一致。
//
// 三种方案：
//   - default_plain（默认，推荐）：默认语言**无前缀**（/about、/index），
//     非默认语言用短码（/en/about、/en/index）；
//   - all_prefix：全语言带短码前缀（/zh/about、/en/about），历史 D1「全前缀」
//     方案的短码化形态；
//   - off：全语言共用逻辑路径（/about），单语言站点兼容形态。
//
// **取值分两层，不要混**：
//   - 工程级：projects.settings.langURLMode（多工程下各站点可以不同）——
//     按工程解析的唯一入口是 pipeline.SiteLangURLModeOf；
//   - 全局默认：sys_config 的 i18n 组 site_lang_url_mode（所有工程都没配时用什么）——
//     本包只持有这一份，见 DefaultSiteLangURLMode。
//
// 本包**不持有**「当前站点在用什么方案」这种进程级可变状态：那会让一个工程的设置决定
// 另一个工程的判定（数据污染），且叠加全局值的定时刷新后会被周期打回。原先的
// config.yaml 键（i18n.site_lang_url_mode 与更早的兼容键 i18n.site_lang_prefix）已删除，
// **不做兼容层** —— 项目开发阶段不留第二处可改的地方，改了也没人知道哪一处生效。
//
// 语言码映射：内置表（pipeline.builtinURLCodes）+ 配置覆盖
// （sys_config 的 i18n.lang_url_codes，形如 zh-CN: zh）+ 确定性回退（主语言子标签小写，
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
		return "", fmt.Errorf("站点语言 URL 方案取值非法: %q（可选 off / default_plain / all_prefix；配置位置：系统配置的 i18n 组 site_lang_url_mode）", raw)
	}
}

// ParseSiteLangURLMode 解析并校验语言 URL 方案取值（站点设置页保存与启动恢复共用同一判据）：
// 空串按默认 default_plain，其余只接受 off / default_plain / all_prefix。
func ParseSiteLangURLMode(raw string) (SiteLangURLMode, error) {
	return parseSiteLangURLMode(raw)
}

// DefaultSiteLangURLMode 返回**全局默认**站点语言 URL 方案（sys_config 的 i18n 组）。
//
// 语义是「所有工程都没配时用什么」，**不是**「当前站点在用什么」：方案是**工程级**的值
// （projects.settings.langURLMode），按工程解析的唯一入口是 pipeline.SiteLangURLModeOf
// （工程值非空用它，否则回退本函数）。
//
// 本包刻意**不提供**运行时 setter：曾经有一个 `SetSiteLangURLMode` 让站点设置页把某个
// 工程的值写进进程级变量，于是 A 工程的保存会改变 B 工程的判定（多工程数据污染），
// 叠加全局默认值的定时刷新后还会被周期打回。这个值只由 sys_config 的 loader 写。
func DefaultSiteLangURLMode() SiteLangURLMode {
	initMu.Lock()
	defer initMu.Unlock()
	return siteLangURLMode
}

// SiteLangURLsSeparated 报告**给定**方案是否按语言区分访问路径（纯函数）。
//
// off = false（各语言共用逻辑路径，后发布者覆盖线上内容）；
// default_plain / all_prefix = true（各语言有独立可寻址路径，可同时在线）。
//
// 入参而非常量读全局：调用方必须传「按工程解析出来的那一份」（pipeline 侧的规则构造
// 已经把它冻结在 LangURLRule 里）。无参版本会让每个读点自己现读一次全局值 —— 那正是
// 「同一个工程在不同读点上拿到不同方案」的来源。
func SiteLangURLsSeparated(mode SiteLangURLMode) bool {
	return mode != SiteLangURLModeOff
}

// SiteLangURLPrefixDefault 报告**给定**方案下默认语言是否也带语言前缀（仅 all_prefix）。
func SiteLangURLPrefixDefault(mode SiteLangURLMode) bool {
	return mode == SiteLangURLModeAllPrefix
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

// URLCodeOverrides 返回配置覆盖的语言码映射表（sys_config 的 i18n 组 lang_url_codes，可能为空）。
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
