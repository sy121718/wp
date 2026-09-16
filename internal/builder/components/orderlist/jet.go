package orderlist

// jet.go — Jet 渲染路径（视图组装）。
//
// 片段地址与兜底链接都在这里编好交给模板：让模板自己拼 URL 就等于把
// 「片段端点长什么样」散到模板里，将来端点改名要满世界找。

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// View 订单列表渲染视图。
type View struct {
	// Title 区块标题。
	Title string
	// ShowTitle 是否显示标题。
	ShowTitle bool
	// FragmentURL 列表片段地址（HTMX 加载与刷新都用它）。
	FragmentURL string
	// LoginURL 登录页的线上路径（槽位 login）；空 = 这个站还没指定。
	LoginURL string
	// HasLoginURL LoginURL 是否可用。
	HasLoginURL bool
	// OrderPageURL 订单页的线上路径（槽位 orders）；无 JS 时的兜底落点。
	OrderPageURL string
	// HasOrderPage OrderPageURL 是否可用。
	HasOrderPage bool
	// Notice 无法渲染时的提示（缺站点工程 id）。空表示正常。
	Notice string
	// LoginHint / LoginText / PagesText 无 JS 时的引导文案（审计 I18N-010）。
	// 容器默认内容就是这三句 —— 空容器在无 JS 下等于这个组件不存在。
	LoginHint string
	LoginText string
	PagesText string
}

// CompileCSS 导出样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成订单列表视图。
//
// projectID 来自构建上下文（片段地址要带它），lang 决定要不要带语言参数，
// loginURL / orderPageURL 来自系统页面槽位（无 JS 与未登录时的兜底落点）。
func BuildView(p *Props, projectID, lang, loginURL, orderPageURL string) View {
	// 文案先落中文兜底：ApplyI18n 在 BuildView 之后按语言覆盖；
	// 未接入 i18n 时它们就是最终值（产物与抽 key 前逐字一致）。
	view := View{
		Title:        effectiveTitle(p),
		ShowTitle:    p.ShowTitle,
		LoginURL:     strings.TrimSpace(loginURL),
		OrderPageURL: strings.TrimSpace(orderPageURL),
		LoginHint:    fallbackLoginHint,
		LoginText:    fallbackLoginText,
		PagesText:    fallbackPagesText,
	}
	view.HasLoginURL = view.LoginURL != ""
	view.HasOrderPage = view.OrderPageURL != ""
	if strings.TrimSpace(projectID) == "" {
		// 片段端点按工程定位，没有工程 id 就等于什么都取不到。
		// 这里给一句可见提示，而不是渲染一个永远空着的容器。
		view.Notice = fallbackNotice
		return view
	}
	q := url.Values{}
	q.Set("projectId", strings.TrimSpace(projectID))
	// 每页条数固定带上去：组件上改了它、片段却按默认值取，
	// 表现出来是「设置不生效」，而这类不一致没有报错可查。
	q.Set("limit", strconv.Itoa(effectivePageSize(p)))
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	view.FragmentURL = ordersListPath + "?" + q.Encode()
	return view
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：与账号表单同构 ——
// 外壳根 div 恒输出，订单列表容器（hx-get）只在取到工程 id 时输出。
func (v View) DeclareFeatures() (attrs, classes []string) {
	attrs = append(attrs, "data-orders-widget")
	if v.Notice != "" {
		return attrs, nil
	}
	return append(attrs, "hx-get", "hx-trigger", "hx-swap"), nil
}
