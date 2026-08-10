// Package crypto 提供签名与哈希等基础能力。
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	secretMu           sync.RWMutex
	secretKey          = "your-secret-key"
	timestampTolerance = 300 * time.Second
)

// SetSecretKey 设置签名密钥。
func SetSecretKey(key string) {
	secretMu.Lock()
	secretKey = key
	secretMu.Unlock()
}

// GenerateSignature 生成签名。
func GenerateSignature(params map[string]interface{}, timestamp int64) string {
	key := getSecretKey()
	sortedParams := sortParams(params)
	signStr := fmt.Sprintf("%s&timestamp=%d&key=%s", sortedParams, timestamp, key)

	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(signStr))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifySignature 验证签名。
func VerifySignature(params map[string]interface{}, timestamp int64, signature string) error {
	if err := verifyTimestamp(timestamp); err != nil {
		return err
	}

	expectedSign := GenerateSignature(params, timestamp)
	if !hmac.Equal([]byte(expectedSign), []byte(signature)) {
		return fmt.Errorf("签名验证失败")
	}

	return nil
}

func verifyTimestamp(timestamp int64) error {
	now := time.Now().Unix()
	diff := now - timestamp
	tolerance := int64(timestampTolerance.Seconds())
	if diff > tolerance || diff < -tolerance {
		return fmt.Errorf("时间戳已过期")
	}
	return nil
}

func sortParams(params map[string]interface{}) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "signature" {
			continue
		}
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var builder strings.Builder
	for i, k := range keys {
		if i > 0 {
			builder.WriteString("&")
		}
		builder.WriteString(fmt.Sprintf("%s=%v", k, params[k]))
	}

	return builder.String()
}

// GenerateSignatureWithString 字符串签名（简化版）。
func GenerateSignatureWithString(data string) string {
	key := getSecretKey()
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifySignatureWithString 字符串签名验证（简化版）。
func VerifySignatureWithString(data, signature string) bool {
	expected := GenerateSignatureWithString(data)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func getSecretKey() string {
	secretMu.RLock()
	defer secretMu.RUnlock()
	return secretKey
}
