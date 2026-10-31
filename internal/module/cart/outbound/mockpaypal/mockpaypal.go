package mockpaypal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"

	cartcontract "go_wp/internal/module/cart/contract"
)

const (
	methodCode = "paypal"

	methodName    = "PayPal（模拟）"
	methodNameKey = "cart.payment.paypalMock"

	idPrefix = "MOCKPAYPAL-"
)

type Gateway struct{ secret []byte }

func New(secret string) *Gateway { return &Gateway{secret: []byte(secret)} }

func (g *Gateway) Method() string { return methodCode }

func (g *Gateway) Title() string { return methodName }

func (g *Gateway) TitleKey() string { return methodNameKey }

func (g *Gateway) Charge(_ context.Context, req *cartcontract.PaymentChargeReq) (res *cartcontract.PaymentChargeResult, err error) {
	if req == nil {
		return nil, errors.New("支付请求为空")
	}
	orderNo := strings.TrimSpace(req.OrderNo)
	if orderNo == "" {

		return nil, errors.New("缺少订单号")
	}
	if req.Amount <= 0 {
		return nil, errors.New("支付金额必须为正")
	}
	sum := sha256.Sum256([]byte(methodCode + ":" + orderNo))
	return &cartcontract.PaymentChargeResult{
		TransactionID: idPrefix + strings.ToUpper(hex.EncodeToString(sum[:8])),
		Sandbox:       true,
	}, nil
}

const callbackSignatureHeader = "x-mock-signature"

func Sign(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

func (g *Gateway) VerifyCallback(headers map[string]string, rawBody []byte) (res *cartcontract.PaymentCallback, err error) {
	if len(g.secret) == 0 {
		return nil, errors.New("支付通道未配置回调密钥")
	}
	got := strings.TrimSpace(headers[callbackSignatureHeader])
	if got == "" {
		return nil, errors.New("回调缺少签名")
	}
	want := Sign(string(g.secret), rawBody)

	if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) != 1 {
		return nil, errors.New("回调签名不匹配")
	}
	var payload struct {
		OrderNo       string `json:"orderNo"`
		TransactionID string `json:"transactionId"`
		Status        string `json:"status"`
		Amount        int64  `json:"amount"`
		Currency      string `json:"currency"`
	}
	if uerr := json.Unmarshal(rawBody, &payload); uerr != nil {
		return nil, errors.New("回调报文解析失败")
	}
	orderNo := strings.TrimSpace(payload.OrderNo)
	if orderNo == "" {
		return nil, errors.New("回调缺少订单号")
	}
	return &cartcontract.PaymentCallback{
		OrderNo:       orderNo,
		TransactionID: strings.TrimSpace(payload.TransactionID),

		Paid:        strings.EqualFold(strings.TrimSpace(payload.Status), "success") || strings.EqualFold(strings.TrimSpace(payload.Status), "completed"),
		Amount:      payload.Amount,
		Method:      methodCode,
		MethodTitle: methodName,
		Sandbox:     true,
	}, nil
}

var _ cartcontract.PaymentGateway = (*Gateway)(nil)
