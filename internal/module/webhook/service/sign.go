package webhookservice

// sign.go — 出站 webhook 的 HMAC-SHA256 签名。
//
// 签名串 = "<timestamp>.<rawBody>"，密钥 = 端点预注册的 secret（管理员配置，
// 加密存库）。接收方按同一算法重算并用常量时间比较验签；
// timestamp 入签可防重放（接收方按自己的时间窗校验时效）。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// SignHeader 签名头 / 时间戳头（与交付客户端共用）。
const (
	HeaderSignature = "X-Webhook-Signature"
	HeaderTimestamp = "X-Webhook-Timestamp"
	HeaderEvent     = "X-Webhook-Event"
	HeaderDelivery  = "X-Webhook-Delivery-ID"
)

// SignaturePrefix 签名值前缀（对齐 GitHub webhook 的 sha256= 约定，便于通用验签器解析）。
const SignaturePrefix = "sha256="

// SignPayload 计算 HMAC-SHA256 签名，返回 "sha256=<hex>" 形式的头值。
func SignPayload(secret string, timestampNano int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestampNano)
	mac.Write(body)
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// VerifyPayload 验签（常量时间比较；导出供接收方实现与测试共用）。
func VerifyPayload(secret string, timestampNano int64, body []byte, signature string) bool {
	expected := SignPayload(secret, timestampNano, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// TimestampHeaderValue 时间戳头的字符串形态。
func TimestampHeaderValue(ts int64) string { return strconv.FormatInt(ts, 10) }
