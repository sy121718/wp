package checkoutform

import (
	"fmt"
	"net/url"
	"strings"

	"go_wp/pkg/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

const ActionPath = "/api/site/cart/checkout"

const DefaultSubmitLabel = "提交订单"

const (
	CouponFieldLabel       = "优惠码"
	CouponFieldPlaceholder = "有优惠码就填在这里"
)

const noticeNoProject = "结算表单暂不可用（未取到站点工程）"

var groupTitles = map[string]string{
	ordercontract.CheckoutGroupContact:  "联系信息",
	ordercontract.CheckoutGroupShipping: "收货地址",
	ordercontract.CheckoutGroupBilling:  "账单地址",
}

func groupTitleOf(group string) string {
	if t, ok := groupTitles[group]; ok {
		return t
	}
	return group
}

type HiddenView struct {
	Name  string
	Value string
}

type OptionView struct {
	Code     string
	Label    string
	Selected bool
}

type FieldView struct {
	Key string

	Label string

	Type string

	Required bool

	Placeholder string

	Wide bool

	Options []OptionView
}

type GroupView struct {
	Key string

	Title string

	Fields []FieldView
}

type CouponView struct {
	Label string

	Placeholder string
}

type ResultLabels struct{ Paid, Pending, PaymentFailed, OrderNo, AccountMailed string }

type View struct {
	ResultLabels     ResultLabels
	BackAction       core.ResolvedAction
	SuccessAction    core.ResolvedAction
	HasBackAction    bool
	HasSuccessAction bool
	BackLabel        string

	CartReadURL string

	Notice string

	Action string

	SubmitLabel string

	Groups []GroupView

	Coupon CouponView

	Hidden []HiddenView
}

func BuildView(p *Props, ctx *core.RenderContext) (View, error) {
	projectID := ""
	lang := ""
	var countries []core.CheckoutCountry
	defaultCountry := ""
	if ctx != nil {
		projectID = strings.TrimSpace(ctx.ProjectID)
		lang = strings.TrimSpace(ctx.Lang)
		countries = ctx.Checkout.Countries
		defaultCountry = strings.TrimSpace(ctx.Checkout.DefaultCountry)
	}
	if projectID == "" {
		return View{Notice: noticeNoProject}, nil
	}

	selected, err := resolveSelection(p)
	if err != nil {
		return View{}, err
	}

	needCountries := false
	for _, f := range selected {
		if f.Type == ordercontract.CheckoutFieldCountry {
			needCountries = true
			break
		}
	}
	if needCountries && len(countries) == 0 {
		return View{}, fmt.Errorf("结算表单缺少国家选项：构建期未注入国家清单（core.checkoutForm 消费 RenderContext.Checkout.Countries）")
	}

	overrides := make(map[string]Field, len(p.Fields))
	for _, f := range p.Fields {
		overrides[strings.TrimSpace(f.Key)] = f
	}

	groups := make([]GroupView, 0, 3)
	index := make(map[string]int, 3)
	for _, f := range selected {
		if f.Group == ordercontract.CheckoutGroupBilling && !p.CollectBilling {
			continue
		}
		gi, ok := index[f.Group]
		if !ok {
			gi = len(groups)
			index[f.Group] = gi
			groups = append(groups, GroupView{Key: f.Group, Title: groupTitleOf(f.Group)})
		}
		groups[gi].Fields = append(groups[gi].Fields, fieldViewOf(f, overrides[f.Key], countries, defaultCountry))
	}

	submit := strings.TrimSpace(p.SubmitLabel)
	if submit == "" {
		submit = DefaultSubmitLabel
	}
	hidden := []HiddenView{{Name: "projectId", Value: projectID}}
	if lang != "" {
		hidden = append(hidden, HiddenView{Name: "lang", Value: lang})
	}
	return View{
		BackLabel:    "返回购物车",
		Action:       ActionPath,
		ResultLabels: ResultLabels{Paid: "订单已付款", Pending: "订单已创建，等待付款", PaymentFailed: "支付暂未成功。订单已保留，请勿重复下单。", OrderNo: "订单号：", AccountMailed: "账号信息已发送至您的邮箱。"},
		CartReadURL:  "/api/site/cart?" + url.Values{"projectId": {projectID}}.Encode(),
		SubmitLabel:  submit,
		Groups:       groups,
		Coupon: CouponView{
			Label:       CouponFieldLabel,
			Placeholder: CouponFieldPlaceholder,
		},
		Hidden: hidden,
	}, nil
}

func (v *View) ResolveActions(node *core.Node, ctx *core.RenderContext) error {
	for _, key := range []string{"back", "success"} {
		if _, ok := node.Actions[key]; !ok {
			continue
		}
		action, err := ctx.ResolveAction(node, key)
		if err != nil {
			return err
		}
		if key == "back" {
			v.BackAction, v.HasBackAction = action, true
		} else {
			v.SuccessAction, v.HasSuccessAction = action, true
		}
	}
	return nil
}

func fieldViewOf(f ordercontract.CheckoutField, override Field, countries []core.CheckoutCountry, defaultCountry string) FieldView {
	label := f.Label
	if override.Label = strings.TrimSpace(override.Label); override.Label != "" {
		label = override.Label
	}

	required := f.Required || override.Required
	view := FieldView{
		Key:         f.Key,
		Label:       label,
		Type:        string(f.Type),
		Required:    required,
		Placeholder: strings.TrimSpace(override.Placeholder),
		Wide:        f.Type == ordercontract.CheckoutFieldTextarea,
	}
	if f.Type == ordercontract.CheckoutFieldCountry {
		view.Options = countryOptionsOf(countries, defaultCountry)
	}
	return view
}

func countryOptionsOf(countries []core.CheckoutCountry, defaultCountry string) []OptionView {
	out := make([]OptionView, 0, len(countries))
	for _, c := range countries {
		code := strings.TrimSpace(c.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(c.Label)
		if label == "" {
			label = code
		}
		out = append(out, OptionView{
			Code:     code,
			Label:    label,
			Selected: defaultCountry != "" && strings.EqualFold(code, defaultCountry),
		})
	}
	return out
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Notice != "" {
		return nil, nil
	}
	attrs = []string{"data-write", "data-site-read", "data-site-cart", "data-requires-js", "data-component-instance", "data-component-part"}
	if v.HasBackAction || v.HasSuccessAction {
		attrs = append(attrs, "data-component-action")
	}
	return attrs, nil
}

func (v *View) ApplyI18n(t func(string, string) string) {
	if v == nil || t == nil {
		return
	}
	v.BackLabel = t("site.component.checkoutForm.back", v.BackLabel)
	v.ResultLabels.Paid = t("site.fragment.checkout.title_paid", v.ResultLabels.Paid)
	v.ResultLabels.Pending = t("site.fragment.checkout.title_pending", v.ResultLabels.Pending)
	v.ResultLabels.PaymentFailed = t("site.component.checkoutForm.paymentFailed", v.ResultLabels.PaymentFailed)
	v.ResultLabels.OrderNo = t("site.fragment.checkout.order_no", v.ResultLabels.OrderNo)
	v.ResultLabels.AccountMailed = t("site.component.checkoutForm.accountMailed", v.ResultLabels.AccountMailed)
	v.Coupon.Label = t("site.component.checkoutForm.coupon", v.Coupon.Label)
	v.Coupon.Placeholder = t("site.component.checkoutForm.couponPlaceholder", v.Coupon.Placeholder)
	if v.SubmitLabel == DefaultSubmitLabel {
		v.SubmitLabel = t("site.component.checkoutForm.submit", v.SubmitLabel)
	}
	if v.Notice != "" {
		v.Notice = t("site.component.checkoutForm.unavailable", v.Notice)
	}
	for i := range v.Groups {
		v.Groups[i].Title = t("site.component.checkoutForm.group."+v.Groups[i].Key, v.Groups[i].Title)
	}
}
