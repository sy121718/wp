package runtimefragment

import "strings"

// paramOf 取参数值（去空白；参数长度已由端点校验）。
func paramOf(r *Request, key string) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Params[key])
}

// cartSitePages 解析本工程在当前语言下的系统页面槽位路径（BIZ-1）。
//
// 语言取自请求参数 lang，缺省为站点默认语言：页面作者手写的 hx-get 通常只带 projectId，
// 单语言站点下这与本站语义完全一致；多语言站点要在 hx-get 里带上 lang 才能拿到
// 本语言的路径 —— 这一点写进文档，不做「从 Referer 猜语言」那种启发式：
// 猜错时链接会在某个语言下静默指向另一语言，比缺 lang 更难查。
//
// 未接入解析器或解析失败一律返回 nil（模板据此不输出链接）。
func cartSitePages(r *Request, projectID string) map[string]string {
	if r == nil || r.SitePagesOf == nil {
		return nil
	}
	lang := r.Lang
	if lang == "" {
		lang = strings.TrimSpace(paramOf(r, fragmentLangParam))
	}
	return r.SitePagesOf(projectID, lang)
}
