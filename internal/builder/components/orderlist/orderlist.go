// Package orderlist 提供访客订单与售后完整静态区块。
package orderlist

import (
	_ "embed" // orderlist.css / orders_widget.jet 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.orderList"

// OrdersPath 访客事实入口，由订单模块提供 JSON。
const OrdersPath = "/api/site/orders"

const (
	defaultTitle = "我的订单"
	// defaultPageSize 一屏订单数（服务端把上限压到 50，这里给个保守默认）。
	defaultPageSize = 10
)

// Props 订单列表属性。
type Props struct {
	// Title 区块标题（留空用「我的订单」）。
	Title string `json:"title,omitempty" ct:"string,maxlen=40,sec=content,label=标题"`
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
type Component struct{ core.Atom[Props] }

func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{{Key: "login", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionInstance}, TargetCapability: "user.login"}}
}
func (*Component) RequiredActions() []string { return nil }
func (*Component) Parts() []core.PartSpec {
	return []core.PartSpec{{Key: "list", Capabilities: []string{"commerce.orders"}}, {Key: "detail", Capabilities: []string{"commerce.order.detail"}}, {Key: "return", Capabilities: []string{"commerce.order.return"}}, {Key: "result", Capabilities: []string{"commerce.order.return.result"}}}
}

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "订单与售后",
		Hint:            "订单列表、详情、退货申请与进度，自动衔接完整流程",
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
}}

// effectiveTitle 有效标题。
func effectiveTitle(p *Props) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return defaultTitle
}

// effectivePageSize 有效每页条数（越界回落到默认值，与订单读取接口同一口径）。
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

}

//go:embed orders_widget.jet
var orderListTemplate string

// init 注册订单列表组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("orders_widget", orderListTemplate)
}
