// Package templates — Jet 模板层的多语言接入（多语言 P1 第二步）。
//
// 取翻译路径为「handler 按请求注入 t 函数」，实测依据见 i18n_jet_test.go：
//   - 数据 map 中的函数值可直接调用：{{ .["t"]("shell.brand", "管理后台") }}；
//     在 {{include}} 的片段与 {{import}}+{{yield}} 的块内同样可用（. 仍指向根数据）；
//     缺 key 时返回 fallback（模板内写的中文原文），全程不报错、不 panic。
//   - 不采用「预翻译 map」（gin.H["i18n"] + {{ .["i18n"]["key"] }}）：
//     缺 key 渲染空串；整个 i18n map 缺失时 Jet 直接运行时报错
//     （there is no field or method 'k' in <invalid reflect.Value>）→ 漏注入即 500。
//   - 不采用「Set 级 AddGlobal」：Set 为进程级共享（jet_render.go 全局单例），
//     全局函数拿不到请求语言；按请求切语言必须改共享状态，并发下 -race 报 DATA RACE。
package templates

import (
	"strings"

	"go_wp/pkg/i18n"
)

// TranslateFunc 返回 Jet 模板用的翻译函数 t(key, fallback)。
//
// 兜底链统一实现在 pkg/i18n.Translate（构建期组件文案与后台模板层共用同一套，
// 不维护第二份逻辑），顺序为：
//  1. 命中当前语言 → 返回译文；
//  2. 未命中当前语言 → pkg/i18n 缓存自动回退默认语言（zh-CN）；
//  3. 仍未命中（含 i18n 未初始化、key 为空）→ 返回 fallback（模板里写的中文原文）；
//  4. fallback 也为空 → 返回 key 本身，保证不输出空串。
//
// 返回的函数只读 i18n 内存缓存（RLock），可安全并发调用。
func TranslateFunc(lang string) func(key, fallback string) string {
	return i18n.TranslateFunc(lang)
}

// LanguageOption 语言下拉选项。
type LanguageOption struct {
	Code   string // 规范化语言码（与 pkg/response 白名单一致）
	Label  string // 语言自称（不随界面语言变化）
	Active bool   // 是否为当前语言
}

// LanguageOptions 返回语言下拉选项；current 未匹配任何选项时选中第一项。
//
// 选项来自 sys_i18n 已有词条的语言集合（I18N-004），随词条 seed 自动扩展；
// 非法 lang 由 GET /admin/lang 回退默认语言。
func LanguageOptions(current string) []LanguageOption {
	current = strings.TrimSpace(current)
	langs := i18n.AvailableLangs()
	options := make([]LanguageOption, 0, len(langs))
	matched := false
	for _, code := range langs {
		active := code == current
		matched = matched || active
		options = append(options, LanguageOption{
			Code: code, Label: i18n.LangLabel(code), Active: active,
		})
	}
	if !matched && len(options) > 0 {
		options[0].Active = true
	}
	return options
}
