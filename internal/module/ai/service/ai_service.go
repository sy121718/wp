package aiservice

import (
	"bytes"
	"context"
	"crypto/tls"
	json "go_wp/pkg/jsonx"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

type Service struct {
	m            *aimodel.Model
	cipherSecret string
	client       *http.Client

	calls CallLogWriter
}

func NewService(m *aimodel.Model) *Service {
	return &Service{m: m, client: newAIClient()}
}

func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = strings.TrimSpace(secret) }

func (s *Service) SetHTTPClient(c *http.Client) {
	if c != nil {
		s.client = c
	}
}

var _ aicontract.AIService = (*Service)(nil)

var (
	ErrProviderNotFound  = aienums.Err(aienums.ErrProviderNotFound)
	ErrVersionConflict   = aienums.Err(aienums.ErrVersionConflict)
	ErrCipherUnavailable = aienums.Err(aienums.ErrCipherUnavailable)
	ErrNoBuiltinModels   = aienums.Err(aienums.ErrNoBuiltinModels)
	ErrBaseURLRequired   = aienums.Err(aienums.ErrBaseURLRequired)
	ErrModelsFetchFailed = aienums.Err(aienums.ErrModelsFetchFailed)

	ErrProviderKeyExists = aienums.Err(aienums.ErrProviderKeyExists)

	ErrAPIKeyRequiredOnBaseURLChange = aienums.Err(aienums.ErrAPIKeyRequiredOnBaseURLChange)
)

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

func (s *Service) decryptSecret(cipherText string) (string, error) {
	if strings.TrimSpace(cipherText) == "" {
		return "", nil
	}
	if s.cipherSecret == "" {
		return "", ErrCipherUnavailable
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}

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

func (s *Service) updateWithVersion(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) error {
	if version <= 0 {
		return aienums.Err(aienums.ErrVersionRequired)
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

var lookupIP = (*net.Resolver).LookupIPAddr

var validateURL = validateAIURL

var (
	ErrURLMalformed         = aienums.Err(aienums.ErrURLMalformed)
	ErrURLSchemeUnsupported = aienums.Err(aienums.ErrURLSchemeUnsupported)
	ErrURLHostMissing       = aienums.Err(aienums.ErrURLHostMissing)
	ErrURLUnresolvable      = aienums.Err(aienums.ErrURLUnresolvable)
	ErrURLDenied            = aienums.Err(aienums.ErrURLDenied)
)

func isForbiddenIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

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

func validateAIURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {

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

var (
	ErrProtocolUnsupported = aienums.Err(aienums.ErrProtocolUnsupported)

	ErrInvalidParam = aienums.Err(aienums.ErrInvalidParam)

	ErrInternal = aienums.Err(aienums.ErrInternal)
)

const (
	DialTimeout = 5 * time.Second

	TLSHandTimeout = 5 * time.Second

	ClientTimeout = 15 * time.Second

	MaxModelsRead = 1 << 20
)

func newAIClient() *http.Client {
	return &http.Client{
		Timeout: ClientTimeout,

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

type ReplyUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64

	CachedTokens int64

	CachedReported bool
	Reported       bool
}

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

		CachedReported: cachedOK,
		Reported:       true,
	}
}

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

type protocolRequestInput struct {
	Model           string
	Messages        []aidto.ChatMessage
	Tools           []aidto.ToolSpec
	MaxOutputTokens int64

	Stream bool
}

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

		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	applyProviderHeaders(req, providerHeaders(provider.ConfigData))

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

const headerUUIDPlaceholder = "{{uuid}}"

var newHeaderUUID = uuid.NewString

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

func applyProviderHeaders(req *http.Request, headers map[string]string) {
	if len(headers) == 0 {
		return
	}
	id := newHeaderUUID()
	for key, val := range headers {
		req.Header.Set(key, strings.ReplaceAll(val, headerUUIDPlaceholder, id))
	}
}
