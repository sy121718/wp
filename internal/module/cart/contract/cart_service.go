package cartcontract

import (
	"context"

	cartdto "go_wp/internal/module/cart/dto"
)

const (
	CartCookieName = "gw_cart"

	CartCookieMaxAgeSeconds = 30 * 24 * 3600
)

type CartService interface {
	Add(ctx context.Context, req *cartdto.CartAddReq) (res *cartdto.CartSnapshot, err error)

	SetQuantity(ctx context.Context, req *cartdto.CartSetQuantityReq) (res *cartdto.CartSnapshot, err error)

	Clear(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error)

	View(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error)

	Checkout(ctx context.Context, req *cartdto.CartCheckoutReq) (res *cartdto.CheckoutResp, err error)

	HandlePaymentCallback(ctx context.Context, req *cartdto.PaymentCallbackReq) (res *cartdto.PaymentCallbackResp, err error)
}

type PaymentGateway interface {
	Method() string

	Title() string

	Charge(ctx context.Context, req *PaymentChargeReq) (res *PaymentChargeResult, err error)

	VerifyCallback(headers map[string]string, rawBody []byte) (res *PaymentCallback, err error)
}

type PaymentCallback struct {
	OrderNo string

	TransactionID string

	Paid bool

	Amount int64

	Method      string
	MethodTitle string

	Sandbox bool
}

type PaymentChargeReq struct {
	OrderNo  string
	Amount   int64
	Currency string
	Email    string

	ReturnURL string
}

type PaymentChargeResult struct {
	TransactionID string

	Sandbox bool
}
