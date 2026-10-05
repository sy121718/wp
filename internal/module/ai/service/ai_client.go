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
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	aidto "go_wp/internal/module/ai/dto"
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
func buildProtocolBody(protocol, model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64) ([]byte, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return buildChatCompletionsBody(model, msgs, tools, maxOutputTokens)
	case aienums.ProtocolOpenAIResponses:
		return buildResponsesBody(model, msgs, tools, maxOutputTokens)
	default:
		return nil, ErrProtocolUnsupported
	}
}

// parseProtocolReply 按协议解析响应（正文 + 工具调用 + 用量）；未实现的协议返回 ErrProtocolUnsupported。
//
// 三者一起回：它们来自同一个响应体，分几次解析等于把同一份 JSON 解几遍，
// 而且「解析失败」的归口口径会被迫写多处（容易分叉）。
func parseProtocolReply(protocol string, body []byte) (ProtocolReply, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return parseChatCompletionsReply(body)
	case aienums.ProtocolOpenAIResponses:
		return parseResponsesReply(body)
	default:
		return ProtocolReply{}, ErrProtocolUnsupported
	}
}

// ReplyUsage 一次上游响应里上报的用量。
//
// Reported=false 表示这一家**没有上报** usage（不是「用了 0 个 token」）——
// 调用流水必须把「没上报」与「真的是 0」分开，否则未上报的调用在统计里看起来像没消耗。
// 解析失败 / 字段缺失一律 Reported=false，**不猜测、不估算**：流水是事实记录。
type ReplyUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	// CachedTokens 上游报告「这次输入里有多少 token 命中了它侧的前缀缓存」。
	//
	// 它是 docs/16 §3.1 第一个验收数字（命中率 95–97%）的**唯一**数据来源：
	// 本地算不出来这个数（要让本地算出「哪些 token 命中了」等于重新实现一遍上游的分词与
	// 前缀匹配），所以上游不报时只能记 0 —— 而 0 与「这次真的没命中」在观测上无法区分，
	// 因此还需要 Reported 之外的一个独立标记（见 CachedReported）。
	CachedTokens int64
	// CachedReported 上游这次是否报告了缓存命中数。
	//
	// **必须与 CachedTokens=0 分开**：只有真正报过 0，才能说「这次确实没命中」；
	// 没报过时那个 0 什么也不说明，把它算进分母会让命中率看起来比实际低一大截，
	// 而症状只是「命中率数字难看」——没有人会去查是不是上游没报。
	CachedReported bool
	Reported       bool
}

// usageFromJSON 从响应体的 usage 对象里取三个数。
//
// 两种协议的字段名不同（chat/completions 是 prompt_tokens / completion_tokens，
// responses 是 input_tokens / output_tokens），所以候选名一起给、谁在取谁。
// total 缺失时用 input+output 补齐：上游只报前两个是常见形态。
//
// 三个数一个都没有 → Reported=false（「没有 usage 对象」与「usage 全是 0」在观测上等价：
// 都没告诉我们任何消耗信息）。
func usageFromJSON(raw any) ReplyUsage {
	obj, ok := raw.(map[string]any)
	if !ok {
		return ReplyUsage{}
	}
	in := firstPositiveInt(obj, "prompt_tokens", "input_tokens")
	out := firstPositiveInt(obj, "completion_tokens", "output_tokens")
	total := firstPositiveInt(obj, "total_tokens")
	if total <= 0 && (in > 0 || out > 0) {
		total = in + out
	}
	if in == 0 && out == 0 && total == 0 {
		return ReplyUsage{}
	}
	cached, cachedOK := firstPositiveIntOK(obj,
		[]string{"prompt_tokens_details", "input_tokens_details"}, "cached_tokens")
	return ReplyUsage{
		InputTokens:  in,
		OutputTokens: out,
		TotalTokens:  total,
		CachedTokens: cached,
		// 报过（含报 0）才算「有信息」。见 CachedReported 的注释：把「没报」当成 0
		// 会让命中率失真，而失真的方向恰好是「看起来更差」。
		CachedReported: cachedOK,
		Reported:       true,
	}
}

// firstPositiveIntOK 在嵌套对象里按候选键顺序取第一个**存在**的整数值。
//
// 与 firstPositiveInt 的差别在于「存在」与「为正」是两件事：缓存命中数报 0 是一个有效
// 观测（这次确实没命中），而字段缺失不是。返回的 ok 表达的是「字段在不在」。
//
// 嵌套形态两家不同但对称：chat/completions 是 usage.prompt_tokens_details.cached_tokens，
// responses 是 usage.input_tokens_details.cached_tokens。
func firstPositiveIntOK(obj map[string]any, detailsKeys []string, field string) (int64, bool) {
	for _, dk := range detailsKeys {
		details, ok := obj[dk].(map[string]any)
		if !ok {
			continue
		}
		switch v := details[field].(type) {
		case float64:
			return int64(v), true
		case int64:
			return v, true
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// firstPositiveInt 按候选键顺序取第一个正整数值；都没有回 0。
//
// 只认正数：JSON 数字解出来是 float64（也可能是 json.Number），负数与 0 都不是「有消耗」的信号。
func firstPositiveInt(obj map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := obj[key].(type) {
		case float64:
			if v > 0 {
				return int64(v)
			}
		case int64:
			if v > 0 {
				return v
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// protocolRequestInput 一次出站请求的入参。
//
// 收成一个结构体而不是继续加形参：这一串字段（模型 / 消息 / 工具 / 上限）都是
// 「这次要发什么」，将来再加一项（temperature、response_format）不该再动一遍调用点。
type protocolRequestInput struct {
	Model           string
	Messages        []aidto.ChatMessage
	Tools           []aidto.ToolSpec
	MaxOutputTokens int64
}

// buildProtocolRequest 组装一次对话出站请求：地址拼接 → SSRF 校验 → 请求体 → 头。
//
// 协议取 provider.Protocol。空串会落到 protocolPath 的 default 分支报 ErrProtocolUnsupported ——
// 「协议为空按默认协议处理」由 SaveProvider 在写入时保证，出站层不再兜底猜测。
func (s *Service) buildProtocolRequest(ctx context.Context, provider *aimodel.AIProviderEntity, in protocolRequestInput) (*http.Request, error) {
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
	body, berr := buildProtocolBody(provider.Protocol, in.Model, in.Messages, in.Tools, in.MaxOutputTokens)
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
