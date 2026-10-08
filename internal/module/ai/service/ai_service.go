package aiservice

// Service 只持本模块 model 与出站 HTTP 客户端：**不持 *gorm.DB**（AGENTS /
// internal/module/CLAUDE.md 的硬约束，由 scripts/check-service-db-boundary.sh 拦截）。
//
// 密钥口径：明文只在两个瞬间存在 —— ①调用方传进来待加密的 SaveProviderReq.APIKey，
// ②拉取可用模型时解密后放进 Authorization 头。其余任何时候对外只有 HasAPIKey 布尔。

// 与 webhook 的 ssrf.go 是同一口径的**两份独立实现**（模块间不跨依赖）：
// 防线是「DNS 解析后的 IP」而不是 hostname 字符串 —— 攻击者可以把内网地址绑到公网
// 域名上（DNS rebinding / 域名指内网），只看字符串拦不住。因此这里强制解析出全部
// A/AAAA 记录，任何一个 IP 落在禁止网段即整体拒绝。
//
// lookupIP 做成包级变量是为了测试可注入（私有 DNS / 环回场景不必真起解析）。
//
// 与 webhook 的差别：这里的 URL 是**管理员填的 API 地址**（不是事件发布方指定），
// 但仍然是用户可输入的自由字符串 —— 后台账号能建供应商，也就能拿它去探内网，
// 所以防护强度不降级。

// 安全护栏固定在客户端里（口径与 webhook 的出站客户端一致）：
//
//	· DialContext 走 SSRF 防护（解析后逐 IP 校验，见 ssrf.go）；
//	· 不跟随重定向 —— 302 指向内网会绕过 DNS 检查，直接拒绝；
//	· 连接 / TLS / 整体三级超时全部有硬上限；
//	· 请求头来自**服务端保存的供应商配置**：默认集合是 Accept + 可选 Authorization，
//	  另可叠加 config_data.headers（管理员写入的自定义头，值里可用 {{uuid}} 占位符）——
//	  但仍然**不接受调用方在请求参数里注入任意头**，系统内部凭据没有透传通道。

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"go_wp/internal/module/ai/contract"
	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

// Service AI 供应商 / 模型配置的服务实现。
type Service struct {
	m            *aimodel.Model
	cipherSecret string
	client       *http.Client
	// calls 调用流水（ai_call_log）的写入端口，装配期注入；未注入时 Chat 照常工作、不记流水。
	// 详见 ai_call_log.go 的 logCallAsync（协程 + 脱离请求 ctx + panic 不外溢）。
	calls CallLogWriter
}

// NewService 构造服务（client 用受限的 ai 出站客户端，见 ai_client.go）。
func NewService(m *aimodel.Model) *Service {
	return &Service{m: m, client: newAIClient()}
}

// SetCipherSecret 注入密钥加密口令（装配期从 app.secret 取，不新造配置项）。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = strings.TrimSpace(secret) }

// SetHTTPClient 注入自定义客户端（测试用；nil 忽略）。
func (s *Service) SetHTTPClient(c *http.Client) {
	if c != nil {
		s.client = c
	}
}

// 编译期断言：Service 实现契约。
var _ aicontract.AIService = (*Service)(nil)

// 语义化哨兵错误。
//
// 值是 enums 的 **i18n key**（不是中文原文）：这些错误要么被页面按白名单翻成中文兜底，
// 要么经 pkg/response.ErrorAuto 直接回给接口调用方 —— 中文原文会被判成内部错误（500）。
var (
	ErrProviderNotFound  = errors.New(aienums.ErrProviderNotFound)
	ErrVersionConflict   = errors.New(aienums.ErrVersionConflict)
	ErrCipherUnavailable = errors.New(aienums.ErrCipherUnavailable)
	ErrNoBuiltinModels   = errors.New(aienums.ErrNoBuiltinModels)
	ErrBaseURLRequired   = errors.New(aienums.ErrBaseURLRequired)
	ErrModelsFetchFailed = errors.New(aienums.ErrModelsFetchFailed)
	// ErrProviderKeyExists 供应商标识重复（先查后插漏网时由唯一约束兜底归口到这里）。
	ErrProviderKeyExists = errors.New(aienums.ErrProviderKeyExists)
	// ErrAPIKeyRequiredOnBaseURLChange 改了 API 地址却没给新密钥：旧凭据不能跟着新地址出站。
	ErrAPIKeyRequiredOnBaseURLChange = errors.New(aienums.ErrAPIKeyRequiredOnBaseURLChange)
)

// encryptSecret 把明文密钥加密成落库密文；空串表示「不改动已存的密钥」，直接回空。
func (s *Service) encryptSecret(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	if s.cipherSecret == "" {
		return "", ErrCipherUnavailable
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

// decryptSecret 解开落库密文；空密文回空串（不报错：没有密钥是合法状态）。
func (s *Service) decryptSecret(cipherText string) (string, error) {
	if strings.TrimSpace(cipherText) == "" {
		return "", nil
	}
	if s.cipherSecret == "" {
		return "", ErrCipherUnavailable
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}

// providerOf 实体 → 对外形状（模型目录从 config_data 解析；解析失败回退空目录）。
func (s *Service) providerOf(e *aimodel.AIProviderEntity) aidto.Provider {
	return aidto.Provider{
		ID:          e.ID,
		ProviderKey: e.ProviderKey,
		DisplayName: e.DisplayName,
		BaseURL:     e.BaseURL,
		Protocol:    e.Protocol,
		Status:      e.Status,
		Sort:        e.Sort,
		Models:      parseModels(e.ConfigData),
		HasAPIKey:   strings.TrimSpace(e.APIKeyCipher) != "",
		Version:     e.Version,
		UpdateTime:  utils.NewJSONTime(e.UpdateTime),
	}
}

// updateWithVersion 带乐观锁的更新，并把「0 行」归因到具体错误。
//
// model 层刻意不区分「不存在」与「版本冲突」（它只回受影响行数），归因属于业务判断：
// 再读一次版本号，能读到 = 版本被别人推进过，读不到 = 行已经没了。
func (s *Service) updateWithVersion(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) error {
	if version <= 0 {
		return errors.New(aienums.ErrVersionRequired)
	}
	rows, err := s.m.UpdateFields(ctx, id, version, fields, updateBy)
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	_, found, verr := s.m.VersionOf(ctx, id)
	if verr != nil {
		return verr
	}
	if !found {
		return ErrProviderNotFound
	}
	return ErrVersionConflict
}

// lookupIP DNS 解析入口（测试可替换）。
var lookupIP = (*net.Resolver).LookupIPAddr

// validateURL 出站校验入口（测试可替换；生产恒为 validateAIURL）。
var validateURL = validateAIURL

// SSRF 防护错误。
//
// 与 webhook 同口径：错误值取自 enums 的 **i18n key**（不是中文文案）—— 这几个错误会
// 经 service 直接回给接口调用方，而 pkg/response.ErrorAuto 按「值是不是 key 形态」
// 区分业务错误；用中文原文会让「你填了个内网地址」变成「服务器内部错误」（500），
// 用户看不出该改哪里。
var (
	ErrURLMalformed         = errors.New(aienums.ErrURLMalformed)
	ErrURLSchemeUnsupported = errors.New(aienums.ErrURLSchemeUnsupported)
	ErrURLHostMissing       = errors.New(aienums.ErrURLHostMissing)
	ErrURLUnresolvable      = errors.New(aienums.ErrURLUnresolvable)
	ErrURLDenied            = errors.New(aienums.ErrURLDenied)
)

// isForbiddenIP 判断 IP 是否落在禁止网段：
// 私有段（10/8、172.16/12、192.168/16、IPv6 fc00::/7）、环回（127/8、::1）、
// 链路本地（169.254/16、fe80::/10）、组播与未指定地址。
func isForbiddenIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// dialAIContext 把校验绑定到真正建立连接的 IP，避免校验与拨号各解析一次域名。
// URL 中的 hostname 保持原样，HTTP Host 与 TLS 证书校验仍针对原始主机。
func dialAIContext(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := lookupIP(net.DefaultResolver, ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrURLUnresolvable
	}
	// 先检查全部结果，再尝试连接；混合公私地址不得因记录顺序不同而放行。
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) || addr.Zone != "" {
			return nil, ErrURLDenied
		}
	}
	dialer := net.Dialer{Timeout: DialTimeout}
	for _, addr := range ips {
		var conn net.Conn
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, err
}

// validateAIURL 校验出站目标 URL：协议白名单 + DNS 解析后逐 IP 检查。
//
// 调用点在「真要发请求之前」，所以地址是**入库后的值**（不做「只信填写时校验」的错误假设：
// API 地址随时可被改，每次出站都要重新过一遍）。
func validateAIURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		// 丢掉 url.Parse 的技术细节：用户要的是「这个地址填得不对」，
		// 而不是 Go 标准库的错误串（后者会一起被渲染到界面上）。
		return ErrURLMalformed
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrURLSchemeUnsupported
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return ErrURLHostMissing
	}

	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	ips, lerr := lookupIP(net.DefaultResolver, ctx, host)
	if lerr != nil || len(ips) == 0 {
		return ErrURLUnresolvable
	}
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) {
			return ErrURLDenied
		}
	}
	return nil
}

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
func buildProtocolBody(protocol, model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64, stream bool) ([]byte, error) {
	switch protocol {
	case aienums.ProtocolOpenAIChatCompletions:
		return buildChatCompletionsBody(model, msgs, tools, maxOutputTokens, stream)
	case aienums.ProtocolOpenAIResponses:
		return buildResponsesBody(model, msgs, tools, maxOutputTokens, stream)
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
	// Stream 走 SSE 流式（请求体里 stream=true）。
	//
	// 它是**请求层面**的开关，所以放在这个结构体而不是另开一条构造路径：
	// 地址拼接、SSRF 校验、密钥解密、自定义头这四步与非流式完全一样，
	// 复制一份出来必然会在下次改这四步时漏掉流式那条分支。
	Stream bool
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
	body, berr := buildProtocolBody(provider.Protocol, in.Model, in.Messages, in.Tools, in.MaxOutputTokens, in.Stream)
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
	if in.Stream {
		// 流式的响应不是 JSON 而是一串 SSE 事件。请求 accept 上写 application/json
		// 会让部分网关（严格实现 content negotiation 的）按非流式回 —— 而调用方
		// 已经在按流读，症状是「一直没有增量、最后一次性拿到全部」。
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
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
