package aihttp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"
)

func trStub(_, fallback string) string { return fallback }

func renderPage(t *testing.T, instance string, data gin.H) string {
	t.Helper()
	data["t"] = trStub
	data["csrf_token"] = "test-csrf"
	rec := httptest.NewRecorder()
	if err := templates.NewJetHTMLRender("../../../../templates", true).
		Instance(instance, data).Render(rec); err != nil {
		t.Fatalf("渲染 %s 失败: %v", instance, err)
	}
	return rec.Body.String()
}

func pageDataFor(tokens []tokenRow, tools []toolRow, newToken string) gin.H {
	return gin.H{
		"title":        "MCP 与外部访问",
		"Title":        "MCP 与外部访问",
		"Endpoint":     "http://example.test/mcp",
		"Tokens":       tokens,
		"Tools":        tools,
		"ScopeOptions": []scopeOption{{Code: "order:list", Label: "订单查看（order:list）"}},
		"NewToken":     newToken,
		"ErrText":      "",
		"DoneText":     "",

		"McpEnabled": false,
		"McpTone":    mcpSwitchTone(false),
	}
}

func TestMcpPageRendersToolsAndTokens(t *testing.T) {
	body := renderPage(t, "admin/ai/mcp", pageDataFor(
		[]tokenRow{{
			ID: 7, Name: "dsh 本地客户端", Prefix: "wp_pat_ab12", ScopesText: "order:list",
			StatusLabel: "启用中", LastUsedText: "", ExpiresText: "2026-12-31 00:00",
		}},
		[]toolRow{{Name: "orders_summary", Description: "订单汇总", Permission: "order:list"}},
		""))

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		"http://example.test/mcp",
		"orders_summary",
		"order:list",
		"dsh 本地客户端",
		"wp_pat_ab12",
		`hx-post="/admin/ai/mcp/token/revoke"`,
		"从未使用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("整页缺少 %q", want)
		}
	}

	if strings.Contains(body, "令牌只显示这一次") {
		t.Errorf("没有新令牌时不应出现一次性明文提示")
	}
}

func TestMcpTokensPartialShowsPlaintext(t *testing.T) {
	body := renderPage(t, "admin/ai/mcp_tokens", pageDataFor(nil, nil, "wp_pat_PLAINTEXT_ONCE"))
	if !strings.Contains(body, "wp_pat_PLAINTEXT_ONCE") {
		t.Fatalf("片段里没有一次性明文")
	}
	if !strings.Contains(body, `id="mcp-token-panel"`) {
		t.Fatalf("片段缺少替换目标 id —— HTMX hx-target 会指空")
	}
	if !strings.Contains(body, "还没有令牌") {
		t.Fatalf("空列表没有提示")
	}

	if strings.Contains(body, "</html>") {
		t.Fatalf("片段不该渲染整页壳")
	}
}

func TestTokenRowsFormatsEmptyTimes(t *testing.T) {
	now := utils.NewJSONTime(time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC))
	rows := tokenRows([]aidto.TokenItem{
		{ID: 1, Name: "a", TokenPrefix: "wp_pat_x", Scopes: []string{"order:list", "page:list"},
			Status: int16(aienums.TokenStatusActive), StatusLabel: "启用中", LastUsedTime: &now},
		{ID: 2, Name: "b", Status: int16(aienums.TokenStatusRevoked), StatusLabel: "已撤销"},
	})
	if len(rows) != 2 {
		t.Fatalf("行数不对: %d", len(rows))
	}
	if rows[0].LastUsedText != "2026-10-05 09:30" {
		t.Errorf("时间格式化不对: %q", rows[0].LastUsedText)
	}
	if rows[0].ScopesText != "order:list、page:list" {
		t.Errorf("权限点拼接不对: %q", rows[0].ScopesText)
	}
	if rows[0].Revoked || !rows[1].Revoked {
		t.Errorf("撤销状态判定不对: %v / %v", rows[0].Revoked, rows[1].Revoked)
	}
	if rows[1].LastUsedText != "" || rows[1].ExpiresText != "" {
		t.Errorf("nil 时间应渲染成空串（由模板决定显示什么）: %q / %q", rows[1].LastUsedText, rows[1].ExpiresText)
	}
}

func TestMcpEndpointURLUsesForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, proto, host, want string
	}{
		{"直连 http", "", "example.test", "http://example.test/mcp"},
		{"反代 https", "https", "example.test", "https://example.test/mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "http://"+tc.host+"/admin/ai/mcp", nil)
			if tc.proto != "" {
				c.Request.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if got := mcpEndpointURL(c); got != tc.want {
				t.Errorf("地址不对: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestScopeOptionsDedupAndSort(t *testing.T) {
	reg := mcp.NewRegistry()
	mk := func(name string, perm permission.Perm) {
		tool := mcp.New[struct{}](name, name, "desc", perm, mcp.Schema{},
			func(_ context.Context, _ struct{}) (mcp.Result, error) { return mcp.Result{}, nil })
		if err := reg.Register(tool); err != nil {
			t.Fatalf("注册工具失败: %v", err)
		}
	}
	mk("t_order_a", permission.OrderList)
	mk("t_order_b", permission.OrderList)
	mk("t_page", permission.PageList)
	mk("t_free", permission.Exempt)

	h := &McpPageHandle{registry: reg}
	got := h.scopeOptions()
	if len(got) != 2 {
		t.Fatalf("选项数不对（去重或 Exempt 过滤失效）: %d -> %+v", len(got), got)
	}
	if got[0].Code >= got[1].Code {
		t.Errorf("选项没有按权限点值排序: %+v", got)
	}
	if !strings.Contains(got[0].Label, got[0].Code) {
		t.Errorf("标签里应带上权限点原文，便于与日志对照: %+v", got[0])
	}

	empty := &McpPageHandle{}
	if opt := empty.scopeOptions(); opt != nil {
		t.Errorf("registry 为空应返回空列表: %+v", opt)
	}
	var _ = json.RawMessage(nil)
}

func TestMcpPageRendersSwitch(t *testing.T) {
	off := renderPage(t, "admin/ai/mcp", pageDataFor(nil, nil, ""))
	for _, want := range []string{
		`action="/admin/ai/mcp/toggle"`,
		"已关闭",
		"开启接入",
		`name="enabled" value="1"`,
		"data-confirm",
	} {
		if !strings.Contains(off, want) {
			t.Errorf("关闭态缺少 %q", want)
		}
	}

	onData := pageDataFor(nil, nil, "")
	onData["McpEnabled"] = true
	onData["McpTone"] = mcpSwitchTone(true)
	on := renderPage(t, "admin/ai/mcp", onData)
	for _, want := range []string{"已开启", "关闭接入", `name="enabled" value="0"`} {
		if !strings.Contains(on, want) {
			t.Errorf("开启态缺少 %q", want)
		}
	}
	if strings.Contains(on, "开启接入") {
		t.Error("开启态不该渲染「开启接入」按钮（状态传错了）")
	}
	if strings.Contains(on, "data-confirm") {
		t.Error("关闭动作不需要二次确认（它只收紧权限）")
	}
}
