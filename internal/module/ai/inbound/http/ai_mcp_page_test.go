package aihttp

import (
	"context"
	aicontract "go_wp/internal/module/ai/contract"
	json "go_wp/pkg/jsonx"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	orderenums "go_wp/internal/module/order/enums"
	pageenums "go_wp/internal/module/page/enums"
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

func pageDataFor(tokens []tokenRow, tools []toolRow) gin.H {
	// 页面数据与生产的 McpPageResp 同形：没有 title/Title —— 标题是界面文案，
	// 由 mcp.html 的 tr("admin.ai.mcp.title", …) 取词，不由 controller 塞成品句。
	return gin.H{
		"Endpoint":     "http://example.test/mcp",
		"Tokens":       tokens,
		"Tools":        tools,
		"ScopeOptions": []scopeOption{{Code: "order:list", Label: "订单查看（order:list）"}},

		"McpEnabled": false,
	}
}

func TestMcpPageRendersToolsAndTokens(t *testing.T) {
	body := renderPage(t, "admin/ai/mcp", pageDataFor(
		[]tokenRow{{
			ID: 7, Name: "dsh 本地客户端", Prefix: "wp_pat_ab12", Scopes: []string{"order:list"},
			Status: int16(aienums.TokenStatusActive),
			ExpiresAt: func() *utils.JSONTime {
				t := time.Date(2026, 12, 31, 0, 0, 0, 0, time.Local)
				v := utils.NewJSONTime(t)
				return &v
			}(),
		}},
		[]toolRow{{Name: "orders_summary", Description: "订单汇总", Permission: "order:list"}},
	))

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		"http://example.test/mcp",
		"orders_summary",
		"order:list",
		"dsh 本地客户端",
		"wp_pat_ab12",
		`hx-post="/api/ai/token/revoke"`,
		`id="mcp-token-row-7"`,
		"从未使用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("整页缺少 %q", want)
		}
	}

	// 令牌行由服务端直接渲染：状态词、时刻与权限点都在模板里取词，前端不再按 JSON 键取值。
	for _, key := range []string{
		`hx-target="#mcp-token-row-7"`,
		`name="id" value="7"`,
		`name="csrf_token" value="test-csrf"`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("令牌行缺少 %q", key)
		}
	}

	// 一次性明文只在生成那一次的响应里出现：整页渲染时结果区是空的，也不能出现明文。
	if strings.Contains(body, `id="mcp-token-plain"`) {
		t.Errorf("整页不该渲染一次性明文区（只有生成响应才有）")
	}
}

func TestMcpTokensBlockRendersEmptyStateWithoutPlaintext(t *testing.T) {
	body := renderPage(t, "admin/ai/mcp_tokens", pageDataFor(nil, nil))
	// 令牌区块不再承载一次性明文：那是生成这一次响应的事，列表只管已有令牌。
	if strings.Contains(body, "wp_pat_") {
		t.Fatalf("令牌区块不该出现任何明文")
	}
	if !strings.Contains(body, `id="mcp-token-panel"`) {
		t.Fatalf("区块缺少面板 id")
	}
	if !strings.Contains(body, `id="mcp-token-rows"`) {
		t.Fatalf("区块缺少行容器 id —— 前端按它插行")
	}
	if !strings.Contains(body, "还没有令牌") {
		t.Fatalf("空列表没有提示")
	}
	// 空列表时表格与撤销说明都要隐藏，否则会出现「表头 + 空表」。
	if !strings.Contains(body, `id="mcp-token-table" hidden`) {
		t.Errorf("空列表时表格应隐藏")
	}

	if strings.Contains(body, "</html>") {
		t.Fatalf("片段不该渲染整页壳")
	}
}

func TestTokenRowsFormatsEmptyTimes(t *testing.T) {
	now := utils.NewJSONTime(time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC))
	rows := tokenRows([]aidto.TokenItem{
		{ID: 1, Name: "a", TokenPrefix: "wp_pat_x", Scopes: []string{"order:list", "page:list"},
			Status: int16(aienums.TokenStatusActive), LastUsedTime: &now},
		{ID: 2, Name: "b", Status: int16(aienums.TokenStatusRevoked)},
	})
	if len(rows) != 2 {
		t.Fatalf("行数不对: %d", len(rows))
	}
	// 时间与分隔符都不在这里格式化：tokenRow 只交代时刻事实，显示格式由模板决定。
	if rows[0].LastUsed == nil || !time.Time(*rows[0].LastUsed).Equal(time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("最近使用时刻事实不对: %v", rows[0].LastUsed)
	}
	if len(rows[0].Scopes) != 2 || rows[0].Scopes[0] != "order:list" || rows[0].Scopes[1] != "page:list" {
		t.Errorf("权限点事实不对: %v", rows[0].Scopes)
	}
	if rows[0].Revoked || !rows[1].Revoked {
		t.Errorf("撤销状态判定不对: %v / %v", rows[0].Revoked, rows[1].Revoked)
	}
	if rows[1].LastUsed != nil || rows[1].ExpiresAt != nil {
		t.Errorf("nil 时刻应保持 nil（由模板决定显示什么）: %v / %v", rows[1].LastUsed, rows[1].ExpiresAt)
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
	mk := func(name string, perm string) {
		tool := mcp.New[struct{}](name, name, "desc", perm, mcp.Schema{},
			func(_ context.Context, _ struct{}) (mcp.Result, error) { return mcp.Result{}, nil })
		if err := reg.Register(tool); err != nil {
			t.Fatalf("注册工具失败: %v", err)
		}
	}
	mk("t_order_a", orderenums.OrderList)
	mk("t_order_b", orderenums.OrderList)
	mk("t_page", pageenums.PageList)
	mk("t_free", "")

	h := &McpPageHandle{registry: reg, dir: stubPermDirectory{names: map[string]string{
		orderenums.OrderList: "订单列表",
		pageenums.PageList:   "页面列表",
	}}}
	got := h.scopeOptions(context.Background())
	if len(got) != 2 {
		t.Fatalf("选项数不对（去重或 Exempt 过滤失效）: %d -> %+v", len(got), got)
	}
	if got[0].Code >= got[1].Code {
		t.Errorf("选项没有按权限点值排序: %+v", got)
	}
	if !strings.Contains(got[0].Label, got[0].Code) {
		t.Errorf("标签里应带上权限点原文，便于与日志对照: %+v", got[0])
	}
	if !strings.Contains(got[0].Label, "订单列表") {
		t.Errorf("标签应取数据库里的权限名称: %+v", got[0])
	}

	empty := &McpPageHandle{}
	if opt := empty.scopeOptions(context.Background()); opt != nil {
		t.Errorf("registry 为空应返回空列表: %+v", opt)
	}
	var _ = json.RawMessage(nil)
}

func TestMcpPageRendersSwitch(t *testing.T) {
	off := renderPage(t, "admin/ai/mcp", pageDataFor(nil, nil))
	// 开关是写动作：标准 HTMX 表单，落点是 /api，页面组的 POST 双轨已清退。
	for _, want := range []string{
		`hx-post="/api/ai/token/toggle"`,
		`hx-target="#mcp-switch"`,
		"已关闭",
		"开启接入",
		`name="enabled" value="1"`,
		"hx-confirm",
	} {
		if !strings.Contains(off, want) {
			t.Errorf("关闭态缺少 %q", want)
		}
	}

	onData := pageDataFor(nil, nil)
	onData["McpEnabled"] = true
	on := renderPage(t, "admin/ai/mcp", onData)
	for _, want := range []string{"已开启", "关闭接入", `name="enabled" value="0"`} {
		if !strings.Contains(on, want) {
			t.Errorf("开启态缺少 %q", want)
		}
	}
	// 开启态里「开启接入」一个都不该出现：那说明状态传反了。
	if strings.Contains(on, "开启接入") {
		t.Error("开启态不该把「开启接入」渲染成任何文字（状态传错了）")
	}
	if strings.Contains(on, "hx-confirm") {
		t.Error("关闭动作不需要二次确认（它只收紧权限）")
	}
}

// stubPermDirectory 是权限目录只读口的测试桩：名称来自显式 map，未命中回退空。
type stubPermDirectory struct {
	names map[string]string
}

func (s stubPermDirectory) Exists(_ context.Context, code string) (bool, error) {
	_, ok := s.names[code]
	return ok, nil
}

func (s stubPermDirectory) List(_ context.Context, codes []string) ([]aicontract.PermEntry, error) {
	out := make([]aicontract.PermEntry, 0, len(codes))
	for _, c := range codes {
		if n, ok := s.names[c]; ok {
			out = append(out, aicontract.PermEntry{Code: c, Name: n, Status: 1})
		}
	}
	return out, nil
}
