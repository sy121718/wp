package aihttp

import (
	"context"
	"encoding/json"
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

// trStub 让模板里的 t 调用直接回兜底文案（缺词条时页面仍可读 —— 与运行时同语义）。
func trStub(_, fallback string) string { return fallback }

// renderPage 用真实模板渲染一页，返回 HTML。
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

// pageDataFor 组装渲染所需的最小数据（字段名与 handler 的 pageData 一致）。
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
		// 开关默认关闭：这是 P8 的口径（「`/mcp` 默认关闭，显式开启才生效」），
		// 用例要测开启态就自己把它改成 true。
		//
		// 分档跟着开关一起给：模板读 `badge-{{.McpTone}}`，map 缺键会渲染失败（Jet 严格）；
		// 这里调**真 helper** 而不是写死 "mute" —— 分档规则改了，这份测试数据跟着走。
		"McpEnabled": false,
		"McpTone":    mcpSwitchTone(false),
	}
}

// TestMcpPageRendersToolsAndTokens 整页：工具清单、令牌行、接入地址都在。
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
		"http://example.test/mcp",              // 接入地址来自请求
		"orders_summary",                       // 工具名
		"order:list",                           // 工具所需权限
		"dsh 本地客户端",                            // 令牌用途
		"wp_pat_ab12",                          // 令牌前缀（明文不可见）
		`hx-post="/admin/ai/mcp/token/revoke"`, // 撤销走片段
		"从未使用",                                 // 空值的显示口径
	} {
		if !strings.Contains(body, want) {
			t.Errorf("整页缺少 %q", want)
		}
	}
	// 没有新令牌时**不能**出现一次性提示与明文块。
	if strings.Contains(body, "令牌只显示这一次") {
		t.Errorf("没有新令牌时不应出现一次性明文提示")
	}
}

// TestMcpTokensPartialShowsPlaintext 片段：只有它承载一次性明文。
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
	// 片段是**区块**而不是整页：不该出现 layout 的东西。
	if strings.Contains(body, "</html>") {
		t.Fatalf("片段不该渲染整页壳")
	}
}

// TestTokenRowsFormatsEmptyTimes 视图行：nil 时间交给模板显示成「从未使用 / 不过期」。
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

// TestMcpEndpointURLUsesForwardedProto 反代下地址必须跟当前请求，而不是写死的配置。
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

// TestScopeOptionsDedupAndSort 可授权权限点 = 工具所需权限点集合（去重、按值排序）。
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
	mk("t_order_b", permission.OrderList) // 同一个权限点：只应出现一次
	mk("t_page", permission.PageList)
	mk("t_free", permission.Exempt) // 不需要权限的工具不进选项

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

	// 没有注册表时安静返回空（装配降级不该让页面崩）。
	empty := &McpPageHandle{}
	if opt := empty.scopeOptions(); opt != nil {
		t.Errorf("registry 为空应返回空列表: %+v", opt)
	}
	var _ = json.RawMessage(nil)
}

// TestMcpPageRendersSwitch 开关状态如实渲染。
//
// 这一页是运维判断「外面能不能进来」的唯一地方：显示成「已开启」而实际关闭
// （或反过来）比不显示更糟 —— 所以两个态各断言一遍，并且断言状态确实传到了表单里
// （按钮提交的 enabled 值必须与当前状态相反，否则点一下等于什么都不做）。
func TestMcpPageRendersSwitch(t *testing.T) {
	off := renderPage(t, "admin/ai/mcp", pageDataFor(nil, nil, ""))
	for _, want := range []string{
		`action="/admin/ai/mcp/toggle"`,
		"已关闭",
		"开启接入",
		`name="enabled" value="1"`, // 关闭态 → 点下去是开启
		"data-confirm",             // 开启会带来对外暴露，必须先确认
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
