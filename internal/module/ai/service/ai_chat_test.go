package aiservice_test

// ai_chat_test.go — 对话入口的 service 链路（真上游换 httptest 假上游）。
//
// 外部测试包（aiservice_test）而不是 package aiservice：service 被 inbound/http 依赖，
// 而 inbound/http 被 internal/routers 依赖，public/test/support 又依赖 routers ——
// 内部测试包 import support 会成环（vet: import cycle not allowed in test）。
//
// 表结构来自生产迁移 + 种子（support.NewMigratedPGTestDB + migrations.RunSeeds），不手抄
// CREATE TABLE。出站目标这件事用两层配合绕开 SSRF 门禁（service 的出站校验拒绝环回地址）：
//   - BaseURL 用 TEST-NET-2 的全局单播字面量，只为通过 validateAIURL 的 IP 检查；
//   - 出站客户端换成把拨号改写到假上游的自定义 Transport —— 不依赖任何 DNS/网络解析。
// SSRF 防护本身由 service 内的 ssrf 相关用例钉住，这里不复测。
//
// 这一层钉住「Chat 把协议层正确串起来」：
//   - 两种已实现协议各自把上游响应解析成 Output；
//   - 上游非 2xx 归口业务错误，且上游报文原文（可能回显密钥）不进错误文本；
//   - 未实现协议回 ErrProtocolUnsupported（不静默回落）；
//   - 出站请求头来自服务端保存的供应商配置（Authorization 明文密钥 + config_data.headers）。
//
// PG 不可用时用例整体 t.Skip（support 的一致口径）。

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	chatTestCipherSecret = "test-ai-chat-cipher-secret"
	chatTestPlainKey     = "sk-ai-chat-plain-0123456789abcdef"
	// 全局单播的 documentation 网段字面量：只为通过 SSRF 门禁，真实连接被下面的 Transport 改写。
	chatTestBaseURL = "http://198.51.100.7"
)

// newChatTestService 建一个跑过生产迁移与种子的库，并接好密钥口令。
func newChatTestService(t *testing.T) (*aiservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产种子失败：%v", err)
	}
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	svc.SetCipherSecret(chatTestCipherSecret)
	return svc, db
}

// newChatProvider 建一个已启用的供应商（密钥走加密入库）。
func newChatProvider(t *testing.T, svc *aiservice.Service, key, protocol string) *aidto.Provider {
	t.Helper()
	p, err := svc.SaveProvider(context.Background(), &aidto.SaveProviderReq{
		ProviderKey: key,
		DisplayName: key,
		BaseURL:     chatTestBaseURL,
		Protocol:    protocol,
		APIKey:      chatTestPlainKey,
	})
	if err != nil {
		t.Fatalf("建供应商失败：%v", err)
	}
	return p
}

// upstreamHit 假上游收到的一次请求。
type upstreamHit struct {
	method  string
	path    string
	auth    string
	session string
}

// fakeUpstream 起一个假上游：记录请求头，按给定状态码与正文应答，并把 service 的出站客户端
// 接到它上面（忽略 URL host）。
func fakeUpstream(t *testing.T, svc *aiservice.Service, status int, body string) <-chan upstreamHit {
	t.Helper()
	hits := make(chan upstreamHit, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- upstreamHit{
			method:  r.Method,
			path:    r.URL.Path,
			auth:    r.Header.Get("Authorization"),
			session: r.Header.Get("x-opencode-session"),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	addr := srv.Listener.Addr().String()
	svc.SetHTTPClient(&http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}})
	return hits
}

func awaitHit(t *testing.T, hits <-chan upstreamHit) upstreamHit {
	t.Helper()
	select {
	case h := <-hits:
		return h
	case <-time.After(3 * time.Second):
		t.Fatal("假上游没有收到请求")
		return upstreamHit{}
	}
}

// TestChatResponsesProtocolReturnsOutput openai_responses 协议 + 200 → Output 正确。
func TestChatResponsesProtocolReturnsOutput(t *testing.T) {
	svc, _ := newChatTestService(t)
	hits := fakeUpstream(t, svc, http.StatusOK, `{"object":"response","output_text":"你好，世界"}`)
	newChatProvider(t, svc, "opencode-go", aienums.ProtocolOpenAIResponses)

	res, err := svc.Chat(context.Background(), &aidto.ChatReq{
		ProviderKey: "opencode-go", Model: "muse", Input: "ping",
	})
	if err != nil {
		t.Fatalf("对话失败：%v", err)
	}
	if res.Output != "你好，世界" {
		t.Errorf("Output 期望「你好，世界」，实际 %q", res.Output)
	}
	if res.Protocol != aienums.ProtocolOpenAIResponses || res.Model != "muse" || res.ProviderKey != "opencode-go" {
		t.Errorf("结果标识不对：%+v", res)
	}
	h := awaitHit(t, hits)
	if h.method != http.MethodPost || h.path != "/responses" {
		t.Errorf("出站请求期望 POST /responses，实际 %s %s", h.method, h.path)
	}
}

// TestChatCompletionsProtocolReturnsOutput openai_chat_completions 协议 + 200 → Output 正确。
func TestChatCompletionsProtocolReturnsOutput(t *testing.T) {
	svc, _ := newChatTestService(t)
	hits := fakeUpstream(t, svc, http.StatusOK, `{"choices":[{"message":{"content":"hi there"}}]}`)
	newChatProvider(t, svc, "muse-chat", aienums.ProtocolOpenAIChatCompletions)

	res, err := svc.Chat(context.Background(), &aidto.ChatReq{
		ProviderKey: "muse-chat", Model: "muse", Input: "ping", MaxOutputTokens: 128,
	})
	if err != nil {
		t.Fatalf("对话失败：%v", err)
	}
	if res.Output != "hi there" {
		t.Errorf("Output 期望「hi there」，实际 %q", res.Output)
	}
	h := awaitHit(t, hits)
	if h.method != http.MethodPost || h.path != "/chat/completions" {
		t.Errorf("出站请求期望 POST /chat/completions，实际 %s %s", h.method, h.path)
	}
}

// TestChatUpstream401ReturnsBusinessErrorWithoutSecret 上游 401 → 业务错误，且错误文本不含密钥。
//
// 假上游故意在正文里回显密钥：若实现把上游报文原文塞进用户可见错误，断言会红。
func TestChatUpstream401ReturnsBusinessErrorWithoutSecret(t *testing.T) {
	svc, _ := newChatTestService(t)
	fakeUpstream(t, svc, http.StatusUnauthorized, `{"error":"invalid api key: `+chatTestPlainKey+`"}`)
	newChatProvider(t, svc, "opencode-go", aienums.ProtocolOpenAIResponses)

	res, err := svc.Chat(context.Background(), &aidto.ChatReq{
		ProviderKey: "opencode-go", Model: "muse", Input: "ping",
	})
	if err == nil {
		t.Fatalf("上游 401 期望报错，实际成功：%+v", res)
	}
	if err.Error() != aienums.ErrInternal {
		t.Errorf("期望归口业务错误 key %q，实际 %q", aienums.ErrInternal, err.Error())
	}
	if strings.Contains(err.Error(), chatTestPlainKey) {
		t.Errorf("错误文本泄漏了密钥：%q", err.Error())
	}
}

// TestChatUnsupportedProtocolRejected anthropic_messages 未实现 → ErrProtocolUnsupported。
func TestChatUnsupportedProtocolRejected(t *testing.T) {
	svc, _ := newChatTestService(t)
	newChatProvider(t, svc, "claude", aienums.ProtocolAnthropicMessages)

	_, err := svc.Chat(context.Background(), &aidto.ChatReq{
		ProviderKey: "claude", Model: "claude-3", Input: "ping",
	})
	if !errors.Is(err, aiservice.ErrProtocolUnsupported) {
		t.Fatalf("期望 ErrProtocolUnsupported，实际 %v", err)
	}
}

// TestChatProviderNotFoundAndDisabled 不存在与停用同口径归口 ErrProviderNotFound。
func TestChatProviderNotFoundAndDisabled(t *testing.T) {
	svc, _ := newChatTestService(t)
	ctx := context.Background()

	if _, err := svc.Chat(ctx, &aidto.ChatReq{ProviderKey: "nope", Model: "m", Input: "x"}); !errors.Is(err, aiservice.ErrProviderNotFound) {
		t.Errorf("不存在的供应商期望 ErrProviderNotFound，实际 %v", err)
	}

	fakeUpstream(t, svc, http.StatusOK, `{"object":"response","output_text":"x"}`)
	p := newChatProvider(t, svc, "opencode-go", aienums.ProtocolOpenAIResponses)
	disabled := aienums.StatusDisabled
	if _, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ID: p.ID, DisplayName: "opencode-go", BaseURL: chatTestBaseURL,
		Protocol: aienums.ProtocolOpenAIResponses, Status: &disabled, Version: p.Version,
	}); err != nil {
		t.Fatalf("停用供应商失败：%v", err)
	}
	if _, err := svc.Chat(ctx, &aidto.ChatReq{ProviderKey: "opencode-go", Model: "muse", Input: "x"}); !errors.Is(err, aiservice.ErrProviderNotFound) {
		t.Errorf("停用的供应商期望 ErrProviderNotFound，实际 %v", err)
	}
}

// TestChatSendsServerConfiguredHeaders 出站头来自服务端保存的供应商配置：
// Authorization 是服务端解密的明文密钥，x-opencode-session 来自 config_data.headers。
func TestChatSendsServerConfiguredHeaders(t *testing.T) {
	svc, db := newChatTestService(t)
	ctx := context.Background()
	hits := fakeUpstream(t, svc, http.StatusOK, `{"choices":[{"message":{"content":"ok"}}]}`)
	p := newChatProvider(t, svc, "opencode-go", aienums.ProtocolOpenAIChatCompletions)

	if err := db.Model(&aimodel.AIProviderEntity{}).Where("id = ?", p.ID).
		Update("config_data", aimodel.JSONMap{"headers": map[string]any{
			"x-opencode-session": "sess-abc-123",
		}}).Error; err != nil {
		t.Fatalf("写配置头失败：%v", err)
	}

	if _, err := svc.Chat(ctx, &aidto.ChatReq{ProviderKey: "opencode-go", Model: "muse", Input: "ping"}); err != nil {
		t.Fatalf("对话失败：%v", err)
	}
	h := awaitHit(t, hits)
	if h.auth != "Bearer "+chatTestPlainKey {
		t.Errorf("Authorization 期望服务端明文的 Bearer，实际 %q", h.auth)
	}
	if h.session != "sess-abc-123" {
		t.Errorf("x-opencode-session 期望来自 config_data.headers，实际 %q", h.session)
	}
}
