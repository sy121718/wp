package orderlist

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.orderList"

const OrdersPath = "/api/site/orders"

const (
	defaultTitle = "我的订单"

	defaultPageSize = 10
)

type Props struct {
	Title string `json:"title,omitempty" ct:"string,maxlen=40,sec=content,label=标题"`

	ShowTitle bool `json:"showTitle,omitempty" ct:"bool,sec=content,label=显示标题"`

	PageSize int `json:"pageSize,omitempty" ct:"int,min=1,max=50,default=10,sec=content,label=每页条数"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=强调色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

		Translatable: []string{"title"},
	},
}}

func effectiveTitle(p *Props) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return defaultTitle
}

func effectivePageSize(p *Props) int {
	if p.PageSize <= 0 {
		return defaultPageSize
	}
	if p.PageSize > 50 {
		return 50
	}
	return p.PageSize
}

func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return "var(--sky-c-text, #111827)"
}

//go:embed orderlist.css
var orderListCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"color": effectiveColor(p),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, orderListCSS, vars); err != nil {

		panic(fmt.Sprintf("orderList 组件样式解析失败: %v", err))
	}

}

//go:embed orders_widget.jet
var orderListTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("orders_widget", orderListTemplate)
}
