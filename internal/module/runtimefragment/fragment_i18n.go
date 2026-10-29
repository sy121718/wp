package runtimefragment

// fragment_i18n.go — 片段访客文案取词（I18N-012，site.fragment.*）。

// tr 按请求语言取 sys_i18n 词条；缺 key 时回退 fallback（与构建期组件同口径）。
func (r *Request) tr(key, fallback string) string {
	if r != nil && r.T != nil {
		return r.T(key, fallback)
	}
	return fallback
}
