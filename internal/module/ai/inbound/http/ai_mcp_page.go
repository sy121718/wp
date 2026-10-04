// ai_mcp_page.go — 后台「MCP 与外部访问」页（整页 + 令牌区块片段）。
//
// 这一页只做三件事，都围着同一个事实：**令牌明文只在创建那一刻存在**。
//
//	· 告诉人去哪填（接入地址 + 请求头），
//	· 生成（明文只渲染一次，落在片段里而不是重定向后的页面数据里 ——
//	  重定向只能把值塞进 query，而 query 会进浏览器历史、访问日志与 Referer），
//	· 列表与撤销（撤销是 PRG：它会改变整张表的形状）。
//
// 「能授权哪些权限点」由**工具清单**决定而不是列全部权限点：本页的令牌是给外部调工具用的，
// 列出 300 多个与本页无关的权限点只会让人选错。将来有更多类别的外部能力时，
// 再把选项来源扩成「工具权限点 ∪ 明示的额外能力」。
package aihttp

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// mcpPagePath 页面地址，也是写操作失败时的回跳地址。
	mcpPagePath = "/admin/ai/mcp"
	// mcpPageTemplate 整页模板；mcpTokensPartial 是令牌区块（HTMX 片段与整页共用同一份标记）。
	mcpPageTemplate  = "admin/ai/mcp"
	mcpTokensPartial = "admin/ai/mcp_tokens"
)

// McpPageHandle 页面处理器。
type McpPageHandle struct {
	tokens   *aiservice.AccessTokenService
	registry *mcp.Registry
	config   sysconfigcontract.Service
}

// NewMcpPageHandle 构造。
func NewMcpPageHandle(tokens *aiservice.AccessTokenService, registry *mcp.Registry) *McpPageHandle {
	return &McpPageHandle{tokens: tokens, registry: registry}
}

// SetConfigService 注入配置读写口（开关对外接入点用）。
//
// 用完整 Service 而不是只读口：这一页既要**显示**开关状态，也要**改**它。
// 端点那边只拿只读口（它只需要判断可达性）—— 同一个模块的两处边界不必一样宽。
func (h *McpPageHandle) SetConfigService(s sysconfigcontract.Service) { h.config = s }

// Page GET /admin/ai/mcp → 整页。
func (h *McpPageHandle) Page(c *gin.Context) {
	data := h.pageData(c, "", "", "")
	c.HTML(http.StatusOK, mcpPageTemplate, data)
}

// TokenCreate POST /admin/ai/mcp/token/create → 令牌区块片段（含**一次性明文**）。
//
// 回片段而不是 PRG：明文不能进 query（浏览器历史 / 访问日志 / Referer 都会带走它），
// 而片段是唯一能「只在这一屏出现一次」的通道。
func (h *McpPageHandle) TokenCreate(c *gin.Context) {
	var req aidto.TokenCreateReq
	if err := c.ShouldBind(&req); err != nil {
		h.renderTokens(c, "", shell.TranslateFor(c)(aienums.ErrInvalidParam, "参数不正确"), "")
		return
	}
	req.UserID = userID(c)
	res, err := h.tokens.Create(c.Request.Context(), &req)
	if err != nil {
		// 业务错误按 key 转当前语言；未登记的 key 原样显示（缺陷可见，而不是吞掉）。
		h.renderTokens(c, "", shell.TranslateFor(c)(err.Error(), err.Error()), "")
		return
	}
	h.renderTokens(c, res.Token, "", "")
}

// TokenRevoke POST /admin/ai/mcp/token/revoke → 令牌区块片段。
//
// 与创建同一条通道（回片段而不是 PRG）：撤销按钮在**表格行**里，而 `<form>` 不能包住 `<tr>`
// —— 每行一个表单不是合法 HTML。片段替换整块既绕开这个限制，也让「撤销后状态列立刻变」
// 与创建后「列表多一行」共用同一份渲染路径。
func (h *McpPageHandle) TokenRevoke(c *gin.Context) {
	var form struct {
		ID int64 `form:"id"`
	}
	t := shell.TranslateFor(c)
	if err := c.ShouldBind(&form); err != nil || form.ID <= 0 {
		h.renderTokens(c, "", t(aienums.ErrInvalidParam, "参数不正确"), "")
		return
	}
	if err := h.tokens.Revoke(c.Request.Context(), form.ID); err != nil {
		h.renderTokens(c, "", t(err.Error(), err.Error()), "")
		return
	}
	h.renderTokens(c, "", "", t("admin.ai.mcp.token.revokeDone", "已撤销该令牌（立即失效）"))
}

// renderTokens 渲染令牌区块片段。
func (h *McpPageHandle) renderTokens(c *gin.Context, newToken, errText, doneText string) {
	c.HTML(http.StatusOK, mcpTokensPartial, h.pageData(c, newToken, errText, doneText))
}

// pageData 组装整页与片段共用的数据。
//
// 片段要**同样的数据**：它是令牌区块的完整重渲染（含刚生成的那一行），
// 少给一个字段就会出现「创建之后列表少了一行」这类不一致。
func (h *McpPageHandle) pageData(c *gin.Context, newToken, errText, doneText string) gin.H {
	t := shell.TranslateFor(c)
	ctx := c.Request.Context()

	tokens, err := h.tokens.List(ctx, &aidto.TokenListReq{All: true, UserID: userID(c)})
	if err != nil {
		// 列表拉不到不阻塞页面：给空列表 + 一句提示，比让整页 500 好（页面还有工具清单与地址可看）。
		tokens = nil
		if errText == "" {
			errText = t(aienums.ErrInternal, "暂时无法读取令牌列表")
		}
	}

	// 整页进入时用 query 回执（页面 URL 可分享、刷新不丢）；片段进入时由调用方直接给。
	if doneText == "" {
		switch c.Query("done") {
		case "1":
			doneText = t("admin.ai.mcp.token.revokeDone", "已撤销该令牌（立即失效）")
		case "on":
			doneText = t("admin.ai.mcp.switch.onDone", "已开启对外接入点（最迟 5 秒后生效）")
		case "off":
			doneText = t("admin.ai.mcp.switch.offDone", "已关闭对外接入点（最晚 5 秒后对任何请求回 404）")
		}
	}
	if q := strings.TrimSpace(c.Query("err")); q != "" && errText == "" {
		errText = t(q, q)
	}

	title := t("admin.ai.mcp.title", "MCP 与外部访问")
	return shell.Prepare(c, gin.H{
		// title（小写）是 layout 壳读的键（与同后台其它页面一致）；页面正文自己再取一次做 h1。
		"title":        title,
		"Title":        title,
		"Lead":         t("admin.ai.mcp.lead", ""),
		"Endpoint":     mcpEndpointURL(c),
		"Tokens":       tokenRows(tokens),
		"Tools":        h.tools(),
		"ScopeOptions": h.scopeOptions(),
		"NewToken":     newToken,
		"ErrText":      errText,
		"DoneText":     doneText,
		"McpEnabled":   h.mcpEnabled(ctx),
	})
}

// mcpEnabled 读对外接入点的当前开关（读不到按关闭 —— 与端点那边的判定同一口径）。
//
// 页面**必须**如实显示：这里是运维判断「外面能不能进来」的唯一地方，
// 显示成「已开启」而实际关闭（或反过来）比不显示更糟。
func (h *McpPageHandle) mcpEnabled(ctx context.Context) bool {
	if h.config == nil {
		return false
	}
	g, err := h.config.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil || g == nil {
		return false
	}
	on, _ := g.Data[sysconfigcontract.KeyMCPEnabled].(bool)
	return on
}

// McpToggle POST /admin/ai/mcp/toggle：开关对外接入点。
//
// 走 PRG：这个动作改变的是**全站**的可达性，重放一次语义完全不同
// （「再关一次」和「关掉」在用户眼里是两件事），所以不留在 POST 的响应里。
//
// 读-改-写整组：只改 mcp_enabled 这一个键，其余键原样带回（这一组将来还会有别的开关），
// 并带上读到的 Version 走乐观锁 —— 两个人同时点，后一个会被拒绝而不是覆盖对方。
func (h *McpPageHandle) McpToggle(c *gin.Context) {
	if h.config == nil {
		c.Redirect(http.StatusFound, mcpPagePath+"?err="+aienums.ErrInternal)
		return
	}
	on := c.PostForm("enabled") == "1"
	ctx := c.Request.Context()
	g, err := h.config.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		c.Redirect(http.StatusFound, mcpPagePath+"?err="+aienums.ErrInternal)
		return
	}
	data := make(map[string]any, len(g.Data)+1)
	for k, v := range g.Data {
		data[k] = v
	}
	data[sysconfigcontract.KeyMCPEnabled] = on
	if _, err := h.config.SetGroup(ctx, &sysconfigcontract.SetGroupReq{
		GroupKey: sysconfigcontract.GroupAI,
		Data:     data,
		Version:  g.Version,
		UpdateBy: userID(c),
	}); err != nil {
		// 内部错误只进日志：它可能带表名、SQL 与约束名，而它会出现在 URL 上
		// （浏览器历史、访问日志、Referer 都留一份）。对外给归口文案 ——
		// 用户此刻要做的是刷新重试，不是读一段数据库报错。
		logger.Scene("ai").With("group", sysconfigcontract.GroupAI).Error(err, "保存对外接入点开关失败")
		c.Redirect(http.StatusFound, mcpPagePath+"?err="+aienums.ErrInternal)
		return
	}
	if on {
		c.Redirect(http.StatusFound, mcpPagePath+"?done=on")
		return
	}
	c.Redirect(http.StatusFound, mcpPagePath+"?done=off")
}

// mcpEndpointURL 拼出接入地址（用当前请求的 scheme + host）。
//
// 从请求派生而不是写配置项：反代 / 多域名部署下，写死的值一定是错的，
// 而用户看到的地址必须是他此刻能用的那一个。
func mcpEndpointURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + c.Request.Host + "/mcp"
}

// tokenRow 令牌表的一行。
//
// 为什么在 handler 里转一道而不是把 TokenItem 直接交给模板：TokenItem 的时间字段是
// `*utils.JSONTime`（`type JSONTime time.Time`），它**不继承** time.Time 的方法 ——
// 模板里既没法格式化也没法判空显示成「从未使用 / 不过期」。把「空值怎么显示」这件事
// 收在 handler 里，模板只剩取值。
type tokenRow struct {
	ID           int64
	Name         string
	Prefix       string
	ScopesText   string
	StatusLabel  string
	Revoked      bool
	LastUsedText string
	ExpiresText  string
}

// tokenRows 把 DTO 转成视图行。
func tokenRows(items []aidto.TokenItem) []tokenRow {
	out := make([]tokenRow, 0, len(items))
	for _, it := range items {
		row := tokenRow{
			ID:           it.ID,
			Name:         it.Name,
			Prefix:       it.TokenPrefix,
			ScopesText:   strings.Join(it.Scopes, "、"),
			StatusLabel:  it.StatusLabel,
			Revoked:      it.Status == int16(aienums.TokenStatusRevoked),
			LastUsedText: tokenTimeText(it.LastUsedTime),
			ExpiresText:  tokenTimeText(it.ExpiresAt),
		}
		out = append(out, row)
	}
	return out
}

// tokenTimeText 把可空时间渲染成文本；nil 用空串交给模板决定显示什么。
func tokenTimeText(t *utils.JSONTime) string {
	if t == nil {
		return ""
	}
	return time.Time(*t).Format("2006-01-02 15:04")
}

// toolRow 工具清单的一行（模板只需要这三个字段）。
type toolRow struct {
	Name        string
	Description string
	Permission  string
}

// tools 列出当前注册的工具（只读展示：注册是**装配期**的事，运行期没有增删入口）。
//
// 这一点要在页面上说清楚，否则运维会找「新增工具」的按钮 —— 工具的落地路径是
// 模块里写一个 inbound/mcp 并在装配期注册，而不是在后台点一下。
func (h *McpPageHandle) tools() []toolRow {
	if h.registry == nil {
		return nil
	}
	list := h.registry.List()
	out := make([]toolRow, 0, len(list))
	for _, t := range list {
		out = append(out, toolRow{
			Name:        t.Name(),
			Description: t.Description(),
			Permission:  string(t.Permission()),
		})
	}
	return out
}

// scopeOption 一个可授权的权限点选项。
type scopeOption struct {
	Code  string
	Label string
}

// scopeOptions 可授权的权限点 = **工具所需的权限点集合**（去重后按值排序）。
//
// 按值排序而不是按中文名：顺序稳定可复现（刷新页面不该让选项跳来跳去），且与权限点常量表同序。
func (h *McpPageHandle) scopeOptions() []scopeOption {
	if h.registry == nil {
		return nil
	}
	seen := map[permission.Perm]struct{}{}
	out := make([]scopeOption, 0, 8)
	for _, t := range h.registry.List() {
		p := t.Permission()
		if p == permission.Exempt {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		label := string(p)
		if _, name, ok := permission.Label(p); ok && name != "" {
			label = name + "（" + string(p) + "）"
		}
		out = append(out, scopeOption{Code: string(p), Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
