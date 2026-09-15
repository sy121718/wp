package orderlist

// i18n.go — 订单列表外壳的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.orderList.<语义>。
const (
	TextKeyLoginHint = "site.component.orderList.loginHint"
	TextKeyLoginText = "site.component.orderList.loginText"
	TextKeyPagesText = "site.component.orderList.pagesText"
	TextKeyNotice    = "site.component.orderList.notice"
)

// 中文兜底（与抽 key 前的产物逐字一致）。
const (
	fallbackLoginHint = "登录后可以查看你的订单。"
	fallbackLoginText = "去登录"
	fallbackPagesText = "订单页"
	fallbackNotice    = "订单列表暂不可用（未取到站点工程）"
)

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 这四句里前三句是**无 JS 时访客唯一看得到的内容**：容器默认内容就是未登录引导，
// 空容器在无 JS 下等于这个组件不存在。所以它们必须随语言变，而不是只在有 JS 时可见。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.LoginHint = fallbackLoginHint
		v.LoginText = fallbackLoginText
		v.PagesText = fallbackPagesText
		if v.Notice != "" {
			v.Notice = fallbackNotice
		}
		return
	}
	v.LoginHint = text(TextKeyLoginHint, fallbackLoginHint)
	v.LoginText = text(TextKeyLoginText, fallbackLoginText)
	v.PagesText = text(TextKeyPagesText, fallbackPagesText)
	// Notice 只在「缺站点工程」这条错误路径上有值：空值时不填，
	// 否则正常渲染的容器会凭空多出一行提示。
	if v.Notice != "" {
		v.Notice = text(TextKeyNotice, fallbackNotice)
	}
}
