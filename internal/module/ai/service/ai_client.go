// ai_client.go — ai 模块的出站 HTTP 客户端与硬上限。
//
// 安全护栏固定在客户端里（口径与 webhook 的出站客户端一致）：
//
//	· DialContext 走 SSRF 防护（解析后逐 IP 校验，见 ssrf.go）；
//	· 不跟随重定向 —— 302 指向内网会绕过 DNS 检查，直接拒绝；
//	· 连接 / TLS / 整体三级超时全部有硬上限；
//	· 请求头来自**服务端保存的供应商配置**：默认集合是 Accept + 可选 Authorization，
//	  另可叠加 config_data.headers（管理员写入的自定义头，值里可用 {{uuid}} 占位符）——
//	  但仍然**不接受调用方在请求参数里注入任意头**，系统内部凭据没有透传通道。
package aiservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// 协议层错误哨兵：取值是 i18n key（enums），与 ai_service.go 的哨兵同一口径 ——
// pkg/response.ErrorAuto 按「值是不是 key 形态」区分业务错误，所以不能回中文原文。
var (
	// ErrProtocolUnsupported 未实现的协议（分派遇到即报错，禁止静默回落）。
	ErrProtocolUnsupported = errors.New(aienums.ErrProtocolUnsupported)
	// ErrInvalidParam 请求参数不合法（如缺失 model）。
	ErrInvalidParam = errors.New(aienums.ErrInvalidParam)
	// ErrInternal 响应无法解析等内部归口（底层原文只进日志，不上页面）。
	ErrInternal = errors.New(aienums.ErrInternal)
)

// 出站硬上限。
const (
	// DialTimeout 单次拨号超时。
	DialTimeout = 5 * time.Second
	// TLSHandTimeout TLS 握手超时。
	TLSHandTimeout = 5 * time.Second
	// ClientTimeout 整个请求超时（拉模型列表是个小请求，不需要长超时）。
	ClientTimeout = 15 * time.Second
	// MaxModelsRead 模型列表响应体读取上限（只用于解析 id，不落盘、不回显原文）。
	MaxModelsRead = 1 << 20
)

// newAIClient 构造受限 HTTP 客户端（包内共享；测试可替换 svc.SetHTTPClient）。
func newAIClient() *http.Client {
	return &http.Client{
		Timeout: ClientTimeout,
		// 不跟随重定向：重定向目标未过 SSRF 校验，一律视为失败。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:         dialAIContext,
			TLSHandshakeTimeout: TLSHandTimeout,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// === 协议分派 ===

// protocolPath 返回协议在 base_url 下的相对路径；未实现的协议返回 ErrProtocolUnsupported。
//
// **禁止静默回落**：任何不在下面的协议都必须显式报错，绝不默认走 chat/completions ——
// 静默回落会把「协议配错」伪装成「服务端返回看不懂的响应」，故障点被挪到几百行外。
func protocolPath(protocol string) (string, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return "/chat/completions", nil
	case aienums.ProtocolOpenAIResponses:
		return "/responses", nil
	default:
		return "", ErrProtocolUnsupported
	}
}

// buildProtocolBody 按协议构造请求体（responses / chat_completions 各一份实现）。
func buildProtocolBody(protocol, model, input string, maxOutputTokens int64) ([]byte, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return buildChatCompletionsBody(model, input, maxOutputTokens)
	case aienums.ProtocolOpenAIResponses:
		return buildResponsesBody(model, input, maxOutputTokens)
	default:
		return nil, ErrProtocolUnsupported
	}
}

// parseProtocolReply 按协议解析响应正文；未实现的协议返回 ErrProtocolUnsupported。
func parseProtocolReply(protocol string, body []byte) (string, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return parseChatCompletionsReply(body)
	case aienums.ProtocolOpenAIResponses:
		return parseResponsesReply(body)
	default:
		return "", ErrProtocolUnsupported
	}
}

// buildProtocolRequest 组装一次对话出站请求：地址拼接 → SSRF 校验 → 请求体 → 头。
//
// 协议取 provider.Protocol。空串会落到 protocolPath 的 default 分支报 ErrProtocolUnsupported ——
// 「协议为空按默认协议处理」由 SaveProvider 在写入时保证，出站层不再兜底猜测。
func (s *Service) buildProtocolRequest(ctx context.Context, provider *aimodel.AIProviderEntity, model, input string, maxOutputTokens int64) (*http.Request, error) {
	if provider == nil {
		return nil, ErrProviderNotFound
	}
	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		baseURL = BuiltinBaseURL(provider.ProviderKey)
	}
	if baseURL == "" {
		return nil, ErrBaseURLRequired
	}
	rel, perr := protocolPath(provider.Protocol)
	if perr != nil {
		return nil, perr
	}
	endpoint := strings.TrimRight(baseURL, "/") + rel
	if verr := validateURL(endpoint); verr != nil {
		return nil, verr
	}
	body, berr := buildProtocolBody(provider.Protocol, model, input, maxOutputTokens)
	if berr != nil {
		return nil, berr
	}
	apiKey, kerr := s.decryptSecret(provider.APIKeyCipher)
	if kerr != nil {
		return nil, kerr
	}
	req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if rerr != nil {
		return nil, ErrURLMalformed
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// 自定义头来自**服务端保存的供应商配置**（config_data.headers，管理员写入），
	// 不是调用方参数 —— 调用方没有注入任意头的通道。
	applyProviderHeaders(req, providerHeaders(provider.ConfigData))
	// Authorization 在自定义头之后写：即便配置里混进了同名头也覆盖不回来，
	// 明文密钥只走这一条通道。
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

// === 供应商自定义请求头 ===

// headerUUIDPlaceholder 自定义头值里的 UUID 占位符（每次请求替换成一个新 UUID）。
const headerUUIDPlaceholder = "{{uuid}}"

// newHeaderUUID 生成占位符替换用的 UUID；包内变量，便于单测注入固定值。
var newHeaderUUID = uuid.NewString

// providerHeaders 从 config_data.headers 读出管理员配置的自定义头。
//
// 形状：{"x-opencode-session": "{{uuid}}"} —— 只认字符串值，非字符串值 / 空键一律跳过
// （坏行静默忽略，与 parseModels 的容错口径一致）。
func providerHeaders(cfg aimodel.JSONMap) map[string]string {
	if len(cfg) == 0 {
		return nil
	}
	raw, ok := cfg["headers"]
	if !ok || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(obj))
	for key, val := range obj {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		text, ok := val.(string)
		if !ok {
			continue
		}
		out[key] = text
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyProviderHeaders 把自定义头写进请求。
//
// **同一次请求内共用一个 UUID**：{{uuid}} 的用途是「一次调用一个会话标识」
// （x-opencode-session），多个头共用同一个值才能在服务端拼成一次调用；跨请求则各自新生成。
func applyProviderHeaders(req *http.Request, headers map[string]string) {
	if len(headers) == 0 {
		return
	}
	id := newHeaderUUID()
	for key, val := range headers {
		req.Header.Set(key, strings.ReplaceAll(val, headerUUIDPlaceholder, id))
	}
}
