package feature

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	orderenums "go_wp/internal/module/order/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/public/test/support"
)

func echoTool() mcp.Tool {
	type args struct {
		ProjectID string `json:"projectId"`
	}
	return mcp.New("orders_summary", "订单区间摘要", "查询某个工程在时间窗内的订单数",
		orderenums.OrderList,
		mcp.Object("查询参数",
			map[string]mcp.Schema{"projectId": mcp.String("工程 id")},
			"projectId"),
		func(_ context.Context, req args) (mcp.Result, error) {
			return mcp.Result{Text: "工程 " + req.ProjectID + " 有 12 单"}, nil
		})
}

func newMCPEngine(t *testing.T, scopes []string, accountAllowed func(string) bool) (*gin.Engine, string) {
	t.Helper()
	return newMCPEngineCfg(t, scopes, accountAllowed, stubMCPConfig{enabled: true})
}

func newMCPEngineCfg(t *testing.T, scopes []string, accountAllowed func(string) bool, cfg sysconfigcontract.ConfigReader) (*gin.Engine, string) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)

	applyProductionSchema(t, db)

	tokens := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokens.SetScopeValidator(func(_ context.Context, p string) bool { return p != "" })
	created, err := tokens.Create(context.Background(), &aidto.TokenCreateReq{
		Name: "外部集成", Scopes: scopes, UserID: 7,
	})
	if err != nil {
		t.Fatalf("签发测试令牌失败：%v", err)
	}

	registry := mcp.NewRegistry()
	if err := registry.Register(echoTool()); err != nil {
		t.Fatalf("注册测试工具失败：%v", err)
	}

	endpoint := aihttp.NewMcpEndpoint(tokens, registry, aiservice.NewToolCallRecorder(aimodel.NewToolCallLogModel(db)))
	endpoint.SetAccountAuthorizer(func(_ context.Context, _ int64, perm string) (bool, error) {
		return accountAllowed(perm), nil
	})

	if cfg != nil {
		endpoint.SetConfigReader(cfg)
	}

	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.POST("/mcp", endpoint.Handle)
	return e, created.Token
}

func rpcCall(t *testing.T, e *gin.Engine, token, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	out := map[string]any{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w.Code, out
}

func TestMcpRejectsAnonymousAndBadToken(t *testing.T) {
	e, _ := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	code, out := rpcCall(t, e, "", body)
	if code != http.StatusUnauthorized {
		t.Fatalf("匿名应 401，实际 %d", code)
	}
	if _, ok := out["result"]; ok {
		t.Error("未认证的响应不该带 result")
	}

	code, _ = rpcCall(t, e, "wp_0000000000000000000000000000000000000000000", body)
	if code != http.StatusUnauthorized {
		t.Fatalf("无效令牌应 401，实际 %d", code)
	}
}

func TestMcpInitialize(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	code, out := rpcCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	result, _ := out["result"].(map[string]any)
	if result == nil || result["protocolVersion"] == "" {
		t.Fatalf("应回 protocolVersion，实际 %v", out)
	}
	if result["serverInfo"] == nil {
		t.Error("应回 serverInfo")
	}
}

func TestMcpToolsListIsScopedByToken(t *testing.T) {

	e, token := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	_, out := rpcCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out); len(got) != 1 || got[0] != "orders_summary" {
		t.Fatalf("应看到 1 个工具，实际 %v", got)
	}

	e2, token2 := newMCPEngine(t, []string{"ai:chat"}, func(string) bool { return true })
	_, out2 := rpcCall(t, e2, token2, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out2); len(got) != 0 {
		t.Fatalf("令牌未声明时不该看到工具，实际 %v", got)
	}

	e3, token3 := newMCPEngine(t, []string{"order:list"}, func(string) bool { return false })
	_, out3 := rpcCall(t, e3, token3, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out3); len(got) != 0 {
		t.Fatalf("账号无权限时不该看到工具，实际 %v", got)
	}
}

func TestMcpToolsCallSucceeds(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	code, out := rpcCall(t, e, token,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"orders_summary","arguments":{"projectId":"p-1"}}}`)
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	result, _ := out["result"].(map[string]any)
	if result == nil {
		t.Fatalf("应回 result，实际 %v", out)
	}
	if result["isError"] != false {
		t.Errorf("成功时 isError 应为 false，实际 %v", result["isError"])
	}
	if text := firstText(result); !strings.Contains(text, "p-1") {
		t.Errorf("文本应含入参透传的工程 id，实际 %q", text)
	}
}

func TestMcpToolsCallDeniedLooksLikeMissing(t *testing.T) {
	e, token := newMCPEngine(t, []string{"ai:chat"}, func(string) bool { return true })
	code, out := rpcCall(t, e, token,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"orders_summary","arguments":{"projectId":"p-1"}}}`)
	if code != http.StatusOK {
		t.Fatalf("协议层错误也应 200（JSON-RPC 的约定），实际 %d", code)
	}
	denied := errorMessage(out)

	e2, token2 := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	_, out2 := rpcCall(t, e2, token2,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"not_a_tool","arguments":{}}}`)
	missing := errorMessage(out2)

	if denied == "" || missing == "" {
		t.Fatalf("两种情况都应有 error，实际 denied=%q missing=%q", denied, missing)
	}
	if denied != missing {
		t.Errorf("无权限与不存在应回同一句话，实际 %q vs %q", denied, missing)
	}
}

func TestMcpToolsCallArgsErrorIsNormalReply(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })
	code, out := rpcCall(t, e, token,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"orders_summary","arguments":{}}}`)
	if code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", code)
	}
	result, _ := out["result"].(map[string]any)
	if result == nil || result["isError"] != true {
		t.Fatalf("参数错应是 isError=true 的正常应答，实际 %v", out)
	}
	if text := firstText(result); !strings.Contains(text, "projectId") {
		t.Errorf("参数错的说明应指出缺了哪个字段，实际 %q", text)
	}
}

func TestMcpUnknownMethodAndNotification(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(string) bool { return true })

	code, out := rpcCall(t, e, token, `{"jsonrpc":"2.0","id":6,"method":"resources/list","params":{}}`)
	if code != http.StatusOK {
		t.Fatalf("协议层错误应 200，实际 %d", code)
	}
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("未知方法应回 error，实际 %v", out)
	}
	if code32, _ := errObj["code"].(float64); int(code32) != -32601 {
		t.Errorf("错误码应为 -32601，实际 %v", errObj["code"])
	}

	code, out = rpcCall(t, e, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if code != http.StatusAccepted {
		t.Errorf("通知应回 202，实际 %d", code)
	}
	if len(out) != 0 {
		t.Errorf("通知不该有响应体，实际 %v", out)
	}
}

func TestMcpToolResultLengthIsBounded(t *testing.T) {
	const long = 6000
	pruned, truncated := aiservice.PruneToolResult(strings.Repeat("单", long))
	if !truncated {
		t.Fatal("超长结果应被标记为已剪枝")
	}
	if n := len([]rune(pruned)); n >= long {
		t.Errorf("剪枝后应短于原文 %d，实际 %d", long, n)
	}
	if !strings.Contains(pruned, "已截断") {
		t.Errorf("剪枝标记应给调用方看到，实际结尾 %q", pruned[len(pruned)-30:])
	}

	if !strings.Contains(pruned, "6000") {
		t.Error("剪枝标记应含完整长度")
	}
}

func toolNames(out map[string]any) []string {
	result, _ := out["result"].(map[string]any)
	if result == nil {
		return nil
	}
	list, _ := result["tools"].([]any)
	names := make([]string, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

func firstText(result map[string]any) string {
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func errorMessage(out map[string]any) string {
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		return ""
	}
	msg, _ := errObj["message"].(string)
	return msg
}

type stubMCPConfig struct {
	enabled bool

	failing bool
}

func (s stubMCPConfig) GetGroup(_ context.Context, key string) (*sysconfigcontract.Group, error) {
	if key != sysconfigcontract.GroupAI {
		return nil, sysconfigcontract.ErrGroupNotFound
	}
	if s.failing {
		return nil, errors.New("配置读取故障")
	}
	return &sysconfigcontract.Group{
		Key:  key,
		Data: map[string]any{sysconfigcontract.KeyMCPEnabled: s.enabled},
	}, nil
}

func newMCPEngineNoConfig(t *testing.T, scopes []string, accountAllowed func(string) bool) (*gin.Engine, string) {
	t.Helper()
	return newMCPEngineCfg(t, scopes, accountAllowed, nil)
}

func TestMCPEndpointClosedByDefault(t *testing.T) {
	e, token := newMCPEngineCfg(t, []string{string(orderenums.OrderList)}, func(string) bool { return true }, stubMCPConfig{})
	if !closedByDefault(t, e, token) {
		t.Fatal("未注入配置读取口时端点必须不可达")
	}

	e2, token2 := newMCPEngineNoConfig(t, []string{string(orderenums.OrderList)}, func(string) bool { return true })
	if !closedByDefault(t, e2, token2) {
		t.Fatal("不注入配置读取口时端点必须不可达")
	}
}

func TestMCPEndpointClosedWhenConfigFails(t *testing.T) {
	e, token := newMCPEngineCfg(t, []string{string(orderenums.OrderList)}, func(string) bool { return true }, stubMCPConfig{failing: true})
	if !closedByDefault(t, e, token) {
		t.Fatal("配置读取失败时必须按关闭处理")
	}
}

func closedByDefault(t *testing.T, e *gin.Engine, token string) bool {
	t.Helper()
	code, _ := rpcCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if code != http.StatusNotFound {
		t.Errorf("关闭时应回 404，实得 %d", code)
		return false
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if strings.TrimSpace(w.Body.String()) != "" {
		t.Errorf("关闭时不该回任何内容：%s", w.Body.String())
		return false
	}
	return true
}
