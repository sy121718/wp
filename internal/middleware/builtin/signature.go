package builtin

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"go_wp/pkg/crypto"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// signatureReplayWindow nonce 有效时间窗口，超过此时间的 nonce 可被回收。
const signatureReplayWindow = 5 * time.Minute

var (
	nonceMu    sync.Mutex
	nonceStore = map[string]time.Time{}
)

// SignatureMiddleware 请求签名验证 + 防重放中间件。
//
// 验证流程：
//  1. 检查 X-Timestamp（或 query timestamp）— 必须在 ±5 分钟内
//  2. 检查 X-Signature（或 query signature）— 通过 crypto.VerifySignature 验证
//  3. 检查 X-Nonce（或 query nonce）— 同一 nonce 不能重复使用
//
// 签名覆盖的参数：
//   - GET 请求：所有 query 参数（排除 signature、timestamp）
//   - POST 请求：query 参数 + JSON 请求体参数（排除 signature、timestamp）
//
// 安全要点：
//   - 时间戳校验防止重放攻击窗口（±5 分钟）
//   - nonce 唯一性确保同一请求不能被二次提交
//   - 签名防篡改：参数被修改后客户端无法重新计算合法签名
//
// 适用位置：需要防篡改和防重放的敏感写操作路由。
func SignatureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		timestampStr := c.GetHeader("X-Timestamp")
		if timestampStr == "" {
			timestampStr = c.Query("timestamp")
		}
		if timestampStr == "" {
			response.ParamError(c, "缺少时间戳参数")
			c.Abort()
			return
		}

		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			response.ParamError(c, "时间戳格式错误")
			c.Abort()
			return
		}

		now := time.Now().Unix()
		if now-timestamp > 300 || timestamp-now > 300 {
			response.ErrorWithMessage(c, 400, "请求已过期")
			c.Abort()
			return
		}

		signature := c.GetHeader("X-Signature")
		if signature == "" {
			signature = c.Query("signature")
		}
		if signature == "" {
			response.ParamError(c, "缺少签名参数")
			c.Abort()
			return
		}

		nonce := c.GetHeader("X-Nonce")
		if nonce == "" {
			nonce = c.Query("nonce")
		}
		nonce = strings.TrimSpace(nonce)
		if nonce == "" {
			response.ParamError(c, "缺少 nonce 参数")
			c.Abort()
			return
		}

		// 收集签名参数：GET 用 query，POST 额外解析 JSON body
		params := make(map[string]interface{})
		query := c.Request.URL.Query()
		for k, v := range query {
			if k == "signature" || k == "timestamp" || len(v) == 0 {
				continue
			}
			params[k] = v[0]
		}

		if c.Request.Method == "POST" {
			bodyParams, readErr := readBodyParams(c)
			if readErr != nil {
				response.ParamError(c, "请求体格式错误")
				c.Abort()
				return
			}
			for k, v := range bodyParams {
				if k == "signature" || k == "timestamp" {
					continue
				}
				params[k] = v
			}
		}

		if err := crypto.VerifySignature(params, timestamp, signature); err != nil {
			response.ErrorWithMessage(c, 400, "签名验证失败")
			c.Abort()
			return
		}

		if !consumeNonce(nonce, time.Unix(timestamp, 0).Add(signatureReplayWindow)) {
			response.ErrorWithMessage(c, 400, "请求重复提交")
			c.Abort()
			return
		}

		c.Next()
	}
}

// readBodyParams 从请求体中读取 JSON 参数。
// 读取完后恢复 Body，便于后续绑定器再次读取。
func readBodyParams(c *gin.Context) (map[string]interface{}, error) {
	if c.Request == nil || c.Request.Body == nil {
		return map[string]interface{}{}, nil
	}

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))

	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}

	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

// consumeNonce 消费一个 nonce：如果 nonce 存在且未过期则返回 false（重复请求），
// 否则记录 nonce 并返回 true。
// 每次调用时顺便清理已过期的 nonce 记录，防止内存泄漏。
func consumeNonce(nonce string, expiresAt time.Time) bool {
	now := time.Now()

	nonceMu.Lock()
	defer nonceMu.Unlock()

	// 清理过期 nonce
	for key, expiry := range nonceStore {
		if !expiry.After(now) {
			delete(nonceStore, key)
		}
	}

	// 检查重复
	if expiry, exists := nonceStore[nonce]; exists && expiry.After(now) {
		return false
	}

	nonceStore[nonce] = expiresAt
	return true
}

// ResetNonceStore 清空 nonce 存储。
// 主要用于测试场景，确保每个测试用例从干净的 nonce 池开始。
func ResetNonceStore() {
	nonceMu.Lock()
	defer nonceMu.Unlock()
	nonceStore = map[string]time.Time{}
}
