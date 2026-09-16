// Package orderlist 实现 core.orderList 访客订单列表组件。
//
// 它解决的是「访客在哪看自己的订单」：一个容器，进页面就拉自己的订单列表。
// 内容由 /_fragments/ordersList 现取 —— 订单是**已发生的事实**，
// 烘进静态产物等于把某一个人的订单发给所有访客。
//
// 三种状态都要说清楚，不能只有一个空壳：
//
//	· 未登录 —— 片段渲染一句引导 + 登录链接（系统页面槽位 login）；
//	· 无 JS —— 片段拉不进来，容器里保留这段引导，并给一个指向订单页
//	  （槽位 orders）的链接；槽位没配就不输出链接（不猜路径，猜错的链接比没有难查）；
//	· 缺站点工程 id —— 片段按工程定位，没有它什么都取不到，直接给一句可见提示
//	  （「看起来正常但不工作」是最难查的一类缺陷）。
package orderlist

import (
	_ "embed" // orderlist.css / orders_widget.jet 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.orderList"

// ordersListPath 片段路径（与 runtimefragment 的注册一致，改一处必须改两处）。
const ordersListPath = "/_fragments/ordersList"

const (
	defaultTitle = "我的订单"
	// defaultPageSize 一屏订单数（服务端把上限压到 50，这里给个保守默认）。
	defaultPageSize = 10
)

// Props 订单列表属性。
type Props struct {
	// Title 区块标题（留空用「我的订单」）。
	Title string `json:"title,omitempty" ct:"text,maxlen=40,sec=content,label=标题"`
	// ShowTitle 是否显示标题（想用页面自己的小标题时关掉）。
	ShowTitle bool `json:"showTitle,omitempty" ct:"bool,sec=content,label=显示标题"`
	// PageSize 每页订单数。
	PageSize int `json:"pageSize,omitempty" ct:"int,min=1,max=50,default=10,sec=content,label=每页条数"`
	// Color 标题与强调色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=强调色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "我的订单",
		Hint:            "访客订单列表（登录后可见，片段现拉）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"title":     "我的订单",
			"showTitle": true,
			"pageSize":  10,
		},
		TypeName: Type,
		// 标题是作者填的文案，参与内容翻译。
		Translatable: []string{"title"},
	},
}

// effectiveTitle 有效标题。
func effectiveTitle(p *Props) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return defaultTitle
}

// effectivePageSize 有效每页条数（越界回落到默认值，与片段侧同一口径）。
func effectivePageSize(p *Props) int {
	if p.PageSize <= 0 {
		return defaultPageSize
	}
	if p.PageSize > 50 {
		return 50
	}
	return p.PageSize
}

// effectiveColor 有效强调色。
func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return "var(--sky-c-text, #111827)"
}

//go:embed orderlist.css
var orderListCSS string

// compileCSS 订单列表组件样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"color": effectiveColor(p),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, orderListCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷：静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("orderList 组件样式解析失败: %v", err))
	}
	// 订单**片段**的样式不在这里注入（审计 UIK-005）：片段样式已归基座
	// （internal/builder/fragment_base.go），按 hx-* 指向 /_fragments/ 的特征判定。
}

//go:embed orders_widget.jet
var orderListTemplate string

// init 注册订单列表组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("orders_widget", orderListTemplate)
}
