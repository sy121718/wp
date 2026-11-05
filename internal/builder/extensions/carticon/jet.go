package carticon

import (
	"net/url"
	"strings"

	"go_wp/pkg/builder/core"
)

type CartLabels struct{ Empty, Qty, Update, Total, Checkout, Clear, Remove, OffShelf, Out, Low, In, Unknown string }

type View struct {
	ProjectID         string
	CheckoutAction    core.ResolvedAction
	HasCheckoutAction bool
	CartLabels        CartLabels
	Inline            bool

	Mode string

	IconSVG string

	Label string

	LoadingText string

	ShowLabel bool

	ShowCount bool

	UseDetails bool

	CartViewURL string

	SummaryURL string

	Notice string
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

func BuildView(p *Props, projectID, lang string) View {
	mode := effectiveMode(p)
	view := View{
		Mode:        mode,
		ProjectID:   strings.TrimSpace(projectID),
		CartLabels:  CartLabels{Empty: "购物车是空的", Qty: "数量", Update: "更新", Total: "合计", Checkout: "去结算", Clear: "清空购物车", Remove: "移除", OffShelf: "商品已下架，请移除", Out: "暂时缺货", Low: "仅剩 %d 件", In: "库存充足", Unknown: "以结算时库存为准"},
		IconSVG:     iconSVG(p),
		Label:       effectiveLabel(p),
		LoadingText: textFallbackLoading,
		ShowLabel:   p.ShowLabel,
		ShowCount:   p.ShowCount,
		UseDetails:  mode != ModeHover && mode != ModeInline,
		Inline:      mode == ModeInline,
	}
	if strings.TrimSpace(projectID) == "" {

		view.Notice = noticeNoProject
		return view
	}
	q := url.Values{}
	q.Set("projectId", strings.TrimSpace(projectID))

	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	encoded := q.Encode()
	view.CartViewURL = cartViewPath + "?" + encoded
	view.SummaryURL = cartSummaryPath + "?" + encoded
	return view
}

func (v *View) ResolveActions(node *core.Node, ctx *core.RenderContext) error {
	if _, ok := node.Actions["checkout"]; !ok {
		return nil
	}
	action, err := ctx.ResolveAction(node, "checkout")
	if err != nil {
		return err
	}
	v.CheckoutAction, v.HasCheckoutAction = action, true
	return nil
}

const (
	TextKeyLabel = "site.component.cartIcon.label"

	TextKeyLoading = "site.component.cartIcon.loading"

	TextKeyNotice = "site.component.cartIcon.notice"
)

const (
	textFallbackLabel = defaultLabel

	textFallbackLoading = "正在加载购物车…"

	noticeNoProject = "购物车暂不可用（未取到站点工程）"
)

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {

		v.Label = textFallbackLabel
		v.LoadingText = textFallbackLoading
		return
	}

	if v.Label == textFallbackLabel {
		v.Label = text(TextKeyLabel, textFallbackLabel)
	}
	v.CartLabels.Empty = text("site.fragment.cart.empty", v.CartLabels.Empty)
	v.CartLabels.Qty = text("site.fragment.cart.qty_aria", v.CartLabels.Qty)
	v.CartLabels.Update = text("site.fragment.cart.update", v.CartLabels.Update)
	v.CartLabels.Total = text("site.fragment.cart.total_prefix", v.CartLabels.Total)
	v.CartLabels.Checkout = text("site.fragment.cart.checkout", v.CartLabels.Checkout)
	v.CartLabels.Clear = text("site.fragment.cart.clear", v.CartLabels.Clear)
	v.CartLabels.Remove = text("site.fragment.cart.remove", v.CartLabels.Remove)
	v.CartLabels.OffShelf = text("site.fragment.stock.off_shelf", v.CartLabels.OffShelf)
	v.CartLabels.Out = text("site.fragment.stock.out", v.CartLabels.Out)
	v.CartLabels.Low = text("site.fragment.stock.low", v.CartLabels.Low)
	v.CartLabels.In = text("site.fragment.stock.in", v.CartLabels.In)
	v.CartLabels.Unknown = text("site.fragment.stock.checkout", v.CartLabels.Unknown)
	v.LoadingText = text(TextKeyLoading, textFallbackLoading)

	if v.Notice != "" {
		v.Notice = text(TextKeyNotice, noticeNoProject)
	}
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	attrs = append(attrs, "data-cart-icon", "data-component-instance")
	if v.Notice != "" {

		return attrs, nil
	}
	attrs = append(attrs, "data-site-read", "data-site-cart", "data-write", "data-requires-js", "data-component-part")
	if v.HasCheckoutAction {
		attrs = append(attrs, "data-component-action")
	}
	if v.UseDetails {
		attrs = append(attrs, "data-cart-icon-panel")
		if v.ShowCount {
			attrs = append(attrs, "data-cart-icon-count")
		}
	}
	return attrs, nil
}
