package cartdto

import (
	"net/url"

	ordercontract "go_wp/internal/module/order/contract"
)

type TrackCookies struct {
	Current string
	First   string
	Session string
	Trail   string

	Visitor string
}

type CartAddReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	VariantID string `json:"variantId" form:"variantId" binding:"required,uuid"`
	Quantity  int    `json:"quantity" form:"quantity"`
	Cookie    string `json:"-" form:"-"`
}

type CartSetQuantityReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	VariantID string `json:"variantId" form:"variantId" binding:"required,uuid"`
	Quantity  *int   `json:"quantity" form:"quantity" binding:"required"`
	Cookie    string `json:"-" form:"-"`
}

type CartViewReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	Cookie    string `json:"-" form:"-"`
}

type PaymentCallbackReq struct {
	ProjectID string
	Headers   map[string]string
	RawBody   []byte
}

type CartCheckoutReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	Cookie    string `json:"-" form:"-"`

	Email string `json:"email" form:"email"`
	Name  string `json:"name" form:"name"`
	Phone string `json:"phone" form:"phone"`

	Shipping ordercontract.OrderAddress `json:"shipping" form:"-"`
	Billing  ordercontract.OrderAddress `json:"billing" form:"-"`

	Remark string `json:"remark" form:"remark"`

	RequestID string `json:"requestId" form:"requestId" binding:"required,max=100"`

	Locale string `json:"locale" form:"locale"`

	CouponCode string `json:"couponCode" form:"couponCode"`

	Tracking TrackCookies `json:"-" form:"-"`

	UserID    *uint64 `json:"-" form:"-"`
	IPAddress string  `json:"-" form:"-"`
	UserAgent string  `json:"-" form:"-"`
}

func (r *CartCheckoutReq) BindForm(v url.Values) error {
	r.Shipping = ordercontract.OrderAddress{Name: v.Get("shipName"), Phone: v.Get("shipPhone"), Country: v.Get("country"), Province: v.Get("province"), City: v.Get("city"), District: v.Get("district"), Address: v.Get("address"), Zip: v.Get("zip")}
	r.Billing = ordercontract.OrderAddress{Name: v.Get("billName"), Phone: v.Get("billPhone"), Country: v.Get("billCountry"), Province: v.Get("billProvince"), City: v.Get("billCity"), District: v.Get("billDistrict"), Address: v.Get("billAddress"), Zip: v.Get("billZip")}
	return nil
}
