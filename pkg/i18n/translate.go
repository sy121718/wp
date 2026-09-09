package i18n

import "strings"

// translate.go — 取词与兜底链的唯一实现（多语言 P1/P4 共用）。
//
// 后台模板层（internal/templates.TranslateFunc，P1 已验证）与构建期组件文案
// （internal/builder，P4）都经本文件取词，避免出现第二套兜底逻辑。
// 本文件只调用既有 facade（GetText → cache.Get，内部 RLock），
// 不改动缓存结构、加载逻辑与刷新机制。

// Translate 按语言取词，兜底链（用户最高优先级要求：绝不报错、绝不 panic、绝不输出空串）：
//
//  1. 命中当前语言 → 返回译文；
//  2. 未命中当前语言 → cache 自动回退默认语言（i18n.GetDefaultLang()，默认 zh-CN）；
//  3. 仍未命中（key 不存在 / i18n 未初始化 / key 为空）→ 返回 fallback（调用方写的中文原文）；
//  4. fallback 也为空 → 返回 key 本身（保证调用方拿到非空串）。
//
// 参数顺序与 GetText(key, lang) 对齐：Translate(key, fallback, lang)。
func Translate(key, fallback, lang string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		if fallback != "" {
			return fallback
		}
		return key
	}

	// 命中（含默认语言回退）时 GetText 返回词条值；未命中返回 key 本身。
	if text := GetText(key, strings.TrimSpace(lang)); text != "" && text != key {
		return text
	}
	if fallback != "" {
		return fallback
	}
	return key
}

// TranslateFunc 返回绑定语言的取词函数（签名 func(key, fallback string) string）。
//
// 语言为空表示「用默认语言」（GetText 内部按默认语言解析）；返回的函数只读内存缓存，
// 可安全并发调用（供 Jet 模板数据与构建期编译上下文注入）。
func TranslateFunc(lang string) func(key, fallback string) string {
	lang = strings.TrimSpace(lang)
	return func(key, fallback string) string {
		return Translate(key, fallback, lang)
	}
}
