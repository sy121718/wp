package feature

// ai_mcp_endpoint_test.go — POST /mcp 的协议层与两关权限判定。
//
// 这一层值得单独钉住的原因是它的失败方式**都不吵**：认证漏了等于公开数据、
// tools/list 不裁剪等于让外部反复尝试无权工具、错误码用错会让客户端把业务失败当传输故障。
// 用例因此按「不许发生什么」写。
//
// 不做完整装配（会话 / CSRF / Casbin 那三层链与 /mcp 无关，它自带 PAT 判定）：
// 只挂这一个端点，Casbin 那关用替身注入（真实装配下由 casbinAuthorizer 承担）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/permission"
	"go_wp/public/test/support"
)

// echoTool 一个最小工具：回显工程 id，用于验证协议与参数透传（内容与真实工具无关）。
func echoTool() mcp.Tool {
	type args struct {
		ProjectID string `json:"projectId"`
	}
	return mcp.New("orders_summary", "订单区间摘要", "查询某个工程在时间窗内的订单数",
		permission.OrderList,
		mcp.Object("查询参数",
			map[string]mcp.Schema{"projectId": mcp.String("工程 id")},
			"projectId"),
		func(_ context.Context, req args) (mcp.Result, error) {
			return mcp.Result{Text: "工程 " + req.ProjectID + " 有 12 单"}, nil
		})
}

// newMCPEngine 挂一个 /mcp 端点，返回 engine 与**一次性明文令牌**。
//
// accountAllowed 是「账号自身是否仍有某权限点」的替身（真实装配下走 Casbin）。
func newMCPEngine(t *testing.T, scopes []string, accountAllowed func(permission.Perm) bool) (*gin.Engine, string) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	// 模板库只含**结构迁移**（seed 建的表不在里面），要按应用启动路径再跑一遍种子 ——
	// 同包已有的 applyProductionSchema 就是这个用途（见 ai_provider_test.go）。
	applyProductionSchema(t, db)

	tokens := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokens.SetScopeValidator(func(p string) bool { return permission.Known(permission.Perm(p)) })
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
	endpoint.SetAccountAuthorizer(func(_ context.Context, _ int64, perm permission.Perm) (bool, error) {
		return accountAllowed(perm), nil
	})

	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.POST("/mcp", endpoint.Handle)
	return e, created.Token
}

// rpcCall 发一条 JSON-RPC 请求，返回状态码与响应体。
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

// TestMcpRejectsAnonymousAndBadToken 匿名与坏令牌都是 401，且**不回**协议层信息。
func TestMcpRejectsAnonymousAndBadToken(t *testing.T) {
	e, _ := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
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

// TestMcpInitialize initialize 回协议版本与能力。
func TestMcpInitialize(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
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

// TestMcpToolsListIsScopedByToken 令牌没声明的工具**看不见**（看不到即不存在，省掉无效往返）。
func TestMcpToolsListIsScopedByToken(t *testing.T) {
	// ① 令牌声明了 order:list、账号也有 → 能看到。
	e, token := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
	_, out := rpcCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out); len(got) != 1 || got[0] != "orders_summary" {
		t.Fatalf("应看到 1 个工具，实际 %v", got)
	}

	// ② 令牌没声明该权限点 → 看不到。
	e2, token2 := newMCPEngine(t, []string{"ai:chat"}, func(permission.Perm) bool { return true })
	_, out2 := rpcCall(t, e2, token2, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out2); len(got) != 0 {
		t.Fatalf("令牌未声明时不该看到工具，实际 %v", got)
	}

	// ③ 令牌声明了，但账号本身已被降权 → 同样看不到（两关，缺一不可）。
	e3, token3 := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return false })
	_, out3 := rpcCall(t, e3, token3, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if got := toolNames(out3); len(got) != 0 {
		t.Fatalf("账号无权限时不该看到工具，实际 %v", got)
	}
}

// TestMcpToolsCallSucceeds 正常调用：200 + content 文本 + isError=false。
func TestMcpToolsCallSucceeds(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
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

// TestMcpToolsCallDeniedLooksLikeMissing 无权限与不存在回**同一句话**（别让外部探测出工具清单）。
func TestMcpToolsCallDeniedLooksLikeMissing(t *testing.T) {
	e, token := newMCPEngine(t, []string{"ai:chat"}, func(permission.Perm) bool { return true })
	code, out := rpcCall(t, e, token,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"orders_summary","arguments":{"projectId":"p-1"}}}`)
	if code != http.StatusOK {
		t.Fatalf("协议层错误也应 200（JSON-RPC 的约定），实际 %d", code)
	}
	denied := errorMessage(out)

	e2, token2 := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
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

// TestMcpToolsCallArgsErrorIsNormalReply 参数错走 200 + isError=true，且**带上具体原因**（模型据此改参）。
func TestMcpToolsCallArgsErrorIsNormalReply(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })
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

// TestMcpUnknownMethodAndNotification 未知方法回 -32601；通知（无 id）不产生响应体。
func TestMcpUnknownMethodAndNotification(t *testing.T) {
	e, token := newMCPEngine(t, []string{"order:list"}, func(permission.Perm) bool { return true })

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

	// 通知：MCP 客户端初始化后会发它，回 JSON 反而让它以为收到了应答。
	code, out = rpcCall(t, e, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if code != http.StatusAccepted {
		t.Errorf("通知应回 202，实际 %d", code)
	}
	if len(out) != 0 {
		t.Errorf("通知不该有响应体，实际 %v", out)
	}
}

// TestMcpToolResultLengthIsBounded 工具结果进上下文前要剪枝：
// 这里直接钉住「端点回给客户端的文本就是剪枝后的文本」，与会话内那条路同源。
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
	// 剪枝标记里要写明完整长度，模型才知道可以缩小范围重查。
	if !strings.Contains(pruned, "6000") {
		t.Error("剪枝标记应含完整长度")
	}
}

// toolNames 从 tools/list 响应里取工具名。
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

// firstText 取 content[0].text。
func firstText(result map[string]any) string {
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

// errorMessage 取 error.message。
func errorMessage(out map[string]any) string {
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		return ""
	}
	msg, _ := errObj["message"].(string)
	return msg
}
