package webhookservice

// sign_test.go — HMAC-SHA256 签名正确性测试。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// TestSignPayload_MatchesIndependentComputation 独立重算 HMAC 验证签名值正确。
func TestSignPayload_MatchesIndependentComputation(t *testing.T) {
	secret := "test-secret"
	ts := int64(1700000000123456789)
	body := []byte(`{"event":"order.paid"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got := SignPayload(secret, ts, body); got != want {
		t.Fatalf("签名不匹配\nwant: %s\ngot:  %s", want, got)
	}
}

// TestVerifyPayload 正例 / 反例 / 篡改敏感度。
func TestVerifyPayload(t *testing.T) {
	secret := "k"
	ts := int64(42)
	body := []byte("payload")
	sig := SignPayload(secret, ts, body)

	if !VerifyPayload(secret, ts, body, sig) {
		t.Fatal("正确签名期望验签通过")
	}
	if VerifyPayload("wrong", ts, body, sig) {
		t.Fatal("错误密钥期望验签失败")
	}
	if VerifyPayload(secret, ts+1, body, sig) {
		t.Fatal("时间戳不同期望验签失败")
	}
	if VerifyPayload(secret, ts, []byte("payload2"), sig) {
		t.Fatal("正文被篡改期望验签失败")
	}
}
