package orderdto

import (
	"strconv"
	"strings"
)

// AdminOrderCreateFormReq 是后台代客建单表单的线上形状：字段名与模板控件逐字一致。
//
// 为什么单独一份而不是直接绑 CreateOrderReq：模板的地址字段是扁平键
// （shipName / billCity …），明细是 variantId / quantity 两个平行数组，而
// CreateOrderReq 的地址是嵌套 OrderAddress、明细是 []OrderItemReq。gin 不折叠
// 扁平前缀，也不把平行数组配对成结构；这些装配放在本类型的 BindForm 里完成。
//
// 业务校验（邮箱格式、至少一行明细）与取词仍在控制器里沿用 orderCreateReqFromForm：
// 字段级错误要按请求语言取词，dto 层不做取词，也不复制第二份校验。
type AdminOrderCreateFormReq struct {
	ProjectID        string `form:"project"`
	CandidateKeyword string `form:"candidateKeyword"`
	RequestID        string `form:"requestId"`

	CustomerEmail string `form:"customerEmail"`
	CustomerName  string `form:"customerName"`
	CustomerPhone string `form:"customerPhone"`

	PaymentMethod      string `form:"paymentMethod"`
	PaymentMethodTitle string `form:"paymentMethodTitle"`

	Currency      string `form:"currency"`
	ShippingTotal string `form:"shippingTotal"`
	CouponCode    string `form:"couponCode"`
	Remark        string `form:"remark"`
	AdminNote     string `form:"adminNote"`

	// 复选框：勾选时浏览器提交 value="1"，不勾选则整个键不出现。
	// 保留字符串而不是 bool，是为了维持「键存在即为真」的原语义（不把 "0" 解释成未勾选）。
	ProvisionGuestAccount string `form:"provisionGuestAccount"`
	SameBilling           string `form:"sameBilling"`

	ShipName     string `form:"shipName"`
	ShipPhone    string `form:"shipPhone"`
	ShipCountry  string `form:"shipCountry"`
	ShipProvince string `form:"shipProvince"`
	ShipCity     string `form:"shipCity"`
	ShipDistrict string `form:"shipDistrict"`
	ShipAddress  string `form:"shipAddress"`
	ShipZip      string `form:"shipZip"`

	BillName     string `form:"billName"`
	BillPhone    string `form:"billPhone"`
	BillCountry  string `form:"billCountry"`
	BillProvince string `form:"billProvince"`
	BillCity     string `form:"billCity"`
	BillDistrict string `form:"billDistrict"`
	BillAddress  string `form:"billAddress"`
	BillZip      string `form:"billZip"`

	// 明细行是两条平行数组，按 DOM 顺序对齐（模板每行都提交这两个控件）。
	VariantID []string `form:"variantId"`
	Quantity  []string `form:"quantity"`

	// 以下由 BindForm 从上面的扁平字段装配，不属于线上字段名。
	Shipping     OrderAddress   `form:"-"`
	Billing      OrderAddress   `form:"-"`
	Items        []OrderItemReq `form:"-"`
	ItemsTooMany bool           `form:"-"`
}

// 复选框语义：勾选时提交 value="1"；键存在即为真（与旧实现完全一致）。
func (r *AdminOrderCreateFormReq) ProvisionAccount() bool {
	return r.ProvisionGuestAccount != ""
}

func (r *AdminOrderCreateFormReq) CopyShippingToBilling() bool {
	return r.SameBilling != ""
}

// adminOrderCreateParseLimit 与原来的逐字段读取保持同一上限：
// 超过即视为「明细过多」，由控制器按 ErrItemLimitExceeded 取词。
const adminOrderCreateParseLimit = 500

// Normalize 在原生 form 绑定之后装配：
//   - 所有文本字段 trim，币种转大写，国家码走 NormalizeCountryCode；
//   - 扁平地址前缀分别合成为 Shipping / Billing；
//   - variantId / quantity 按出现顺序配对成 []OrderItemReq，空变体行跳过（与旧行为一致）。
//
// 字段值来自已经完成的 Gin FormPost 绑定，不在这里重新解键。
func (r *AdminOrderCreateFormReq) Normalize() {
	r.ProjectID = strings.TrimSpace(r.ProjectID)
	r.CandidateKeyword = strings.TrimSpace(r.CandidateKeyword)
	r.RequestID = strings.TrimSpace(r.RequestID)

	r.CustomerEmail = strings.TrimSpace(r.CustomerEmail)
	r.CustomerName = strings.TrimSpace(r.CustomerName)
	r.CustomerPhone = strings.TrimSpace(r.CustomerPhone)

	r.PaymentMethod = strings.TrimSpace(r.PaymentMethod)
	r.PaymentMethodTitle = strings.TrimSpace(r.PaymentMethodTitle)

	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	r.ShippingTotal = strings.TrimSpace(r.ShippingTotal)
	r.CouponCode = strings.TrimSpace(r.CouponCode)
	r.Remark = strings.TrimSpace(r.Remark)
	r.AdminNote = strings.TrimSpace(r.AdminNote)

	r.Shipping = OrderAddress{
		Name:     strings.TrimSpace(r.ShipName),
		Phone:    strings.TrimSpace(r.ShipPhone),
		Country:  NormalizeCountryCode(r.ShipCountry),
		Province: strings.TrimSpace(r.ShipProvince),
		City:     strings.TrimSpace(r.ShipCity),
		District: strings.TrimSpace(r.ShipDistrict),
		Address:  strings.TrimSpace(r.ShipAddress),
		Zip:      strings.TrimSpace(r.ShipZip),
	}
	r.Billing = OrderAddress{
		Name:     strings.TrimSpace(r.BillName),
		Phone:    strings.TrimSpace(r.BillPhone),
		Country:  NormalizeCountryCode(r.BillCountry),
		Province: strings.TrimSpace(r.BillProvince),
		City:     strings.TrimSpace(r.BillCity),
		District: strings.TrimSpace(r.BillDistrict),
		Address:  strings.TrimSpace(r.BillAddress),
		Zip:      strings.TrimSpace(r.BillZip),
	}

	r.Items, r.ItemsTooMany = adminOrderItemsFromRows(r.VariantID, r.Quantity)
}

// adminOrderItemsFromRows 把平行数组配成明细行；顺序即表单行顺序。
func adminOrderItemsFromRows(variantIDs, quantities []string) ([]OrderItemReq, bool) {
	if len(variantIDs) > adminOrderCreateParseLimit {
		return nil, true
	}
	items := make([]OrderItemReq, 0, len(variantIDs))
	for i, raw := range variantIDs {
		variantID := strings.TrimSpace(raw)
		if variantID == "" {
			continue
		}
		quantity := ""
		if i < len(quantities) {
			quantity = strings.TrimSpace(quantities[i])
		}
		// 与旧行为一致：数量非法按 0 处理，由 service 的业务校验兜底。
		qty, _ := strconv.Atoi(quantity)
		items = append(items, OrderItemReq{VariantID: variantID, Quantity: qty})
	}
	return items, false
}
