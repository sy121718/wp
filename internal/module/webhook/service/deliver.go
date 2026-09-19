package webhookservice

// deliver.go — webhook 的出站 HTTP 投递客户端。
//
// 安全护栏全部固定在客户端里：
//   · 每次投递前重新过 validateWebhookURL（端点 URL 可能事后被改，不能只信入库时）；
//   · 不跟随重定向 —— 302 指向内网会绕过 DNS 检查，直接拒绝；
//   · 连接 / TLS / 整体三级超时全部有硬上限；
//   · 请求头是固定集合（签名 + 事件元数据），不接收、不合并任何调用方传入的头，
//     系统内部凭据（会话密钥、API key）因此没有透传通道。

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 投递硬上限（OSS-006 安全模型第 3/4 条）。
const (
	DialTimeout     = 5 * time.Second
	TLSHandTimeout  = 5 * time.Second
	ClientTimeout   = 15 * time.Second
	MaxPayloadBytes = 64 << 10 // 请求体（事件 JSON）上限
	MaxResponseRead = 4 << 10  // 响应体只读 4KB（仅用于错误信息，不信任内容）
)

// newWebhookClient 构造受限 HTTP 客户端（包内共享；测试可注入 Transport）。
func newWebhookClient() *http.Client {
	return &http.Client{
		Timeout: ClientTimeout,
		// 不跟随重定向：重定向目标未过 SSRF 校验，一律视为失败。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:         dialWebhookContext,
			TLSHandshakeTimeout: TLSHandTimeout,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// postWebhook 发出一次签名投递，返回远端 HTTP 状态码。
//
// body 必须是已序列化的事件 JSON（调用方负责 ≤ MaxPayloadBytes）；
// secret 是端点解密后的明文密钥，只在本次调用内存中存在。
func postWebhook(ctx context.Context, client *http.Client, targetURL, secret, eventType string, deliveryID uint64, body []byte) (status int, err error) {
	if len(body) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook 请求体 %d 字节超出上限 %d", len(body), MaxPayloadBytes)
	}
	if verr := validateURL(targetURL); verr != nil {
		return 0, verr
	}

	now := time.Now().UnixNano()
	req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if rerr != nil {
		return 0, fmt.Errorf("构造 webhook 请求失败: %w", rerr)
	}
	// 固定头集：不透传任何内部凭据（安全模型第 6 条）。
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderEvent, eventType)
	req.Header.Set(HeaderDelivery, fmt.Sprintf("%d", deliveryID))
	req.Header.Set(HeaderTimestamp, TimestampHeaderValue(now))
	req.Header.Set(HeaderSignature, SignPayload(secret, now, body))

	resp, derr := client.Do(req)
	if derr != nil {
		return 0, fmt.Errorf("webhook 投递失败: %w", derr)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseRead))
	return resp.StatusCode, nil
}
