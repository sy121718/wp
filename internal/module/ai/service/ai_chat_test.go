package aiservice_test

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

	chatTestBaseURL = "http://198.51.100.7"
)

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

type upstreamHit struct {
	method  string
	path    string
	auth    string
	session string
}

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
