package aihttp

// 与会话页的 SessionSend 差在三处，值得写清楚：
//  1. **不选模型**：悬浮球在任意页面上被点开，让用户先选供应商等于把「随手问一句」
//     变成一次配置操作；这里用「第一个可用的供应商 + 它的第一个模型」。
//  2. **返回片段而不是重定向**：回答要出现在球旁边，而不是把用户弹到另一个页面
//     （弹走之后他回来就丢了原来的上下文）。
//  3. **带上页面上下文**（D9）：path + query + 页面标题。**不注入页面语义** ——
//     40+ 页面逐个登记「这个页面在筛什么」就是第二份真源，注定与页面本身分叉；
//     注入位置信息、让模型自己说它的理解，错了也看得见（规则里已要求它明说）。
//
// 会话用固定的会话键（每个后台账号一条），所以悬浮球里的对话是连续的，
// 同时它也会出现在会话页的列表里 —— 不另开一套只在这里可见的历史。

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

// 分档口径（见 ai_page_util.go）：
//
//	· 供应商级写操作（保存 / 删除 / 启停）→ PRG 整页重定向：这些动作会改变卡片集合
//	  与版本号，整页重渲染最不容易出现「片段与页面状态不同步」；
//	· 模型目录级操作（保存 / 恢复默认 / 获取可用 / 增行 / 删行）→ HTMX 片段替换目录区：
//	  目录是页面上最高频的编辑区，每次整页刷新会丢掉用户其他输入。
//
// 失败时片段用**用户提交的行**重渲染（保留输入），成功时用库里的行（展示归一后的结果）。
//
// 提示的一律口径：页面内部只流转 **i18n key**（不是译文）——
//
//	· HTMX 片段：渲染时经 facingTextParams 翻成当前语言（含 {n}/{m} 填充）；
//	· 关掉 JS 的原生 POST：同一条 key 翻成当前语言后，由 shell.RenderJump 渲染成提示页
//	  （文案走响应体，不再写进 ?err= / ?done=）。

// 形状照 internal/module/inventory/inbound/http/inventory_page_util.go（模块各写一份，
// 不跨模块共用）：写操作在同一个 handler 里按 HX-Request 分档 —— HTMX 请求回片段或
// HX-Redirect，原生表单回 303/302 重定向。这样表单**保留原生的 method/action**，
// 关掉 JS 也是完整可用的后台。

// 与配置面共用同包的小工具（aiErrKey / redirectWhere / parseInt64 / userID），
// 不引入配置面的任何状态。
//
// 写操作的结论走提示页（shell.RenderJump）：文案在响应体里，回跳地址只带筛选，
// 不再把 ?done= / ?err= 写进 URL。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
	"go_wp/internal/shell"
	"go_wp/internal/uispec"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// fabSessionKey 悬浮球的会话键（EnsureSession 按 userID 隔离，所以每个人一条）。
const fabSessionKey = "admin-fab"

// fabInputLimit 一次提问的字符上限。
//
// 不给上限时一次粘贴可以把整个请求顶爆，而失败发生在出站层 —— 用户看到的是
// 「模型调用失败」，与「你贴太多了」完全对不上。截断而不是拒绝：悬浮球是随手用的，
// 报错让人重来一遍比截断更烦，而截断是可见的（下面会附一句说明）。
const fabInputLimit = 4000

// fabContextQueryLimit 页面 query 串的截断长度。
//
// 它可能很长（筛选条件多的列表页），而它对回答的用处只在「这个页面正按什么筛」，
// 所以留前若干字符足够；超出部分截掉并在注入文本里说明。
const fabContextQueryLimit = 300

// FabAsk POST /admin/ai/ask：悬浮球发一条消息，回一段可渲染的片段。
func (h *SessionPageHandle) FabAsk(c *gin.Context) {
	input := strings.TrimSpace(c.PostForm("input"))
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {
		// 没有可用模型时**直说**，不要让请求走到出站层再失败：
		// 那里的错误是「模型调用失败」，用户会去查网络，而真正的原因是没配供应商。
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabNoModel),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	if input == "" {
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabEmptyInput),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {
		// 按 rune 截断：按字节截会把一个汉字切成两半，那半个字会一路进数据库与请求体。
		input = string(runes[:fabInputLimit])
		truncated = true
	}

	res, err := h.svc.SendMessage(c.Request.Context(), aidto.SendMessageReq{
		SessionKey:  fabSessionKey,
		ProviderKey: providerKey,
		Model:       model,
		Input:       composeFabInput(c, input),
		UserText:    input,
		UserID:      userID(c),
	})
	if err != nil {
		h.renderFab(c, gin.H{
			"FabError":  fabText(aienums.MsgFabFailed),
			"FabAskKey": c.PostForm("ctxPath"),
		})
		return
	}
	answer := strings.TrimSpace(res.AssistantEvent.Content)
	views := fabViewsOf(res)
	data := gin.H{
		"FabAnswer":    answer,
		"FabViews":     views,
		"FabSessionID": res.Session.ID,
		"FabAskKey":    c.PostForm("ctxPath"),
	}
	if len(views) > 0 {
		// 与 AI 会话页同一形态：ui_blocks.html 自己 `{{range .Views}}`，
		// 所以 include 时**必须**传一个带 Views 键的上下文，不能把切片本身传进去。
		// Jet 的 include 只接受变量表达式，不接受 `gin.H{...}` 字面量（解析期就报
		// `unexpected token '{'`），所以这个包装必须在 Go 侧做好。
		data["FabViewsCtx"] = gin.H{"Views": views}
	}
	// 思考过程：从助手事件的 meta 里取（会话层在 finishReply 写进去的）。
	// 直接取 res.AssistantEvent.Meta —— 那是**本次**这一轮的助手事件，
	// 而不是从事件列表里找最后一条（列表可能因为折叠/重放而不含它）。
	if r := fabReasoningOf(res); r != "" {
		data["FabReasoning"] = r
		data["FabThinkLabel"] = fabText(aienums.MsgFabThinkLabel)
	}
	if truncated {
		data["FabTruncatedText"] = fabText(aienums.MsgFabTruncated)
	}
	if n := len(res.ToolEvents); n > 0 {
		data["FabToolsText"] = fabText(aienums.MsgFabToolsPrefix) + strconv.Itoa(n) + fabText(aienums.MsgFabToolsSuffix)
	}
	// 既没有话、也没有取过数：可能是模型只调了工具但工具全失败。
	// 这句要说清「可以去看详情」—— 详情里有工具名与参数，那才是排查的入口。
	if answer == "" && len(res.ToolEvents) == 0 {
		data["FabEmptyText"] = fabText(aienums.MsgFabEmpty)
	}
	h.renderFab(c, data)
}

// renderFab 渲染悬浮球的结果片段。
//
// **一律 200**：面板里的每一句话（没写问题、没配模型、这轮失败）都是**正常的业务回答**，
// 而 htmx 默认不替换 4xx/5xx 的响应 —— 用状态码表达业务结果会让用户点了按钮
// 看到面板毫无变化。真正的内部错误（模板渲染失败）由框架自己回 500，那才是 5xx 的位置。
func (h *SessionPageHandle) renderFab(c *gin.Context, data gin.H) {
	c.HTML(http.StatusOK, "partials/ai_fab_result", data)
}

// fabViewsOf 从本轮的工具事件里收集可渲染视图（P5 的渲染数据通道）。
//
// 取的是**工具事件**而不是助手事件：视图是取数工具挂上去的（ai_session_render.go 的
// 逐块取数），助手事件只有文本。多个工具各出几张视图时按发生顺序拼起来，
// 与它们在会话页时间线里的顺序一致 —— 两处顺序不同会让人怀疑自己看的是不是同一次回答。
// fabReasoningOf 取本次回答的思考过程（没有则空串）。
//
// meta 是 map[string]any，值可能是 string 也可能是别的（将来改结构时）；
// 只认 string，不认就回空 —— 与「上游没返回思考过程」等价，
// 都属于「不显示这一段」，而不是让整个回答失败。
func fabReasoningOf(res *aidto.SendMessageResult) string {
	if res == nil || res.AssistantEvent.Meta == nil {
		return ""
	}
	s, _ := res.AssistantEvent.Meta["reasoning"].(string)
	return strings.TrimSpace(s)
}

func fabViewsOf(res *aidto.SendMessageResult) []uispec.View {
	if res == nil {
		return nil
	}
	out := make([]uispec.View, 0, len(res.ToolEvents))
	for _, ev := range res.ToolEvents {
		out = append(out, renderViewsOf(ev.Meta)...)
	}
	return out
}

// defaultModel 取一个可用于悬浮球的供应商与模型。
//
// 判据三条，缺一不可：**启用**（停用的供应商不该被自动选中）、**有密钥**（没有密钥
// 调不通，而失败信息会指向出站层）、**模型目录非空**（目录空时选不出模型名）。
// 取第一个满足的而不是「最近更新的」：这里要的是**稳定**（每次点开用同一个），
// 而不是「最优」—— 选谁由用户在会话页决定，悬浮球只求能跑。
func (h *SessionPageHandle) defaultModel(ctx context.Context) (providerKey, model string, ok bool) {
	if h.providers == nil {
		return "", "", false
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		return "", "", false
	}
	for _, p := range list {
		if p.Status != aiProviderEnabled || !p.HasAPIKey || len(p.Models) == 0 {
			continue
		}
		key := strings.TrimSpace(p.ProviderKey)
		id := strings.TrimSpace(p.Models[0].ID)
		if key == "" || id == "" {
			continue
		}
		return key, id, true
	}
	return "", "", false
}

// aiProviderEnabled 供应商「启用」的落库值（与配置面同口径）。
const aiProviderEnabled = 1

// composeFabInput 把页面上下文与本轮问题拼成一段输入。
//
// **格式固定、逐行标注**：模型要靠这几行判断「用户是在某个列表页问的」。
// 上下文缺失时整段不写（不写「（无）」占位）—— 占位会让模型以为自己在一个空页面上。
//
// 只用 path + query + 标题，不注入 DOM、不注入页面正文：页面正文里可能有别人的数据，
// 而 path 与 query 是浏览器地址栏里本来就可见的东西，注入它们不扩大任何信息面。
func composeFabInput(c *gin.Context, input string) string {
	var b strings.Builder
	path := strings.TrimSpace(c.PostForm("ctxPath"))
	query := strings.TrimSpace(c.PostForm("ctxQuery"))
	title := strings.TrimSpace(c.PostForm("ctxTitle"))
	if path != "" || query != "" || title != "" {
		b.WriteString("（当前页面：")
		if title != "" {
			b.WriteString(title)
		}
		if path != "" {
			if title != "" {
				b.WriteString(" ")
			}
			b.WriteString(path)
		}
		if query != "" {
			q := query
			if len([]rune(q)) > fabContextQueryLimit {
				q = string([]rune(q)[:fabContextQueryLimit]) + "…（已截断）"
			}
			b.WriteString(q)
		}
		b.WriteString("）\n")
	}
	b.WriteString(input)
	return b.String()
}

// fabText 把 key 换成面向用户的文案。
//
// **必须走这一步**：`aienums.MsgFab*` 是 **key**（`ai.fab.tools` 这种），
// 文案在 `FacingMessages` 里。直接把常量塞进模板的结果是把 key 原样渲染给用户 ——
// 实测真跑一次时页面上出现 `ai.fab.tools2ai.fab.toolsUnit`，而那轮恰好是
// 「模型要求补上下文」的正常回答，看起来像模板坏了（这个缺陷只有真调模型才会暴露，
// 假上游的用例断言的是「渲染出了那句提示」，不会去看那句提示长什么样）。
//
// 取不到时回 key 本身（可见的降级）：这一层是异常路径的最后兜底，
// 宁可显示一个 `ai.fab.xxx` 也不要显示空白 —— 空白会被当成「按钮没生效」。
func fabText(key string) string {
	if v, ok := aienums.FacingText(key); ok {
		return v
	}
	return key
}

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

	// 文案一律由调用方给：整页进入时为空（写动作的结论由 shell.RenderJump 渲染成提示页，
	// 不再经 ?err= / ?done= 回带），片段进入时是刚算出的成品文案。

	title := t("admin.ai.mcp.title", "MCP 与外部访问")
	// 开关只读一次：页面要如实显示它（这里是运维判断「外面能不能进来」的唯一地方），
	// 徽标分档与文案必须同源 —— 读两次若中间被改，会出现「文字说已开启、颜色说已关闭」。
	enabled := h.mcpEnabled(ctx)
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
		"McpEnabled":   enabled,
		"McpTone":      mcpSwitchTone(enabled),
	})
}

// mcpSwitchTone 对外接入开关 → 徽标分档。
//
// 分档而不是让模板写 {{if .McpEnabled}}badge-success{{else}}…：阈值（什么算「开」）
// 是这片代码的语义，颜色是 CSS 的事 —— 模板只把分档写进 class（见 docs/rules/template-boundary.md）。
func mcpSwitchTone(enabled bool) string {
	if enabled {
		return "ok"
	}
	return "mute"
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
		mcpJump(c, false, shell.TranslateFor(c)(aienums.ErrInternal, "暂时无法读取令牌列表"))
		return
	}
	on := c.PostForm("enabled") == "1"
	ctx := c.Request.Context()
	g, err := h.config.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		mcpJump(c, false, shell.TranslateFor(c)(aienums.ErrInternal, "暂时无法读取令牌列表"))
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
		mcpJump(c, false, shell.TranslateFor(c)(aienums.ErrInternal, "保存失败，请稍后重试"))
		return
	}
	if on {
		mcpJump(c, true, shell.TranslateFor(c)("admin.ai.mcp.switch.onDone", "已开启对外接入点（最迟 5 秒后生效）"))
		return
	}
	mcpJump(c, true, shell.TranslateFor(c)("admin.ai.mcp.switch.offDone", "已关闭对外接入点（最晚 5 秒后对任何请求回 404）"))
}

// mcpJump 开关动作的出口：整页提示（对应 ThinkPHP 的 success() / error()）。
//
// 取代了原先的 PRG + ?err= / ?done=：那条通道的文案要在 URL 上走一圈，
// 于是读侧还得证明「这条提示确实是本仓给的」。现在文案走响应体，读侧判定随之消失。
// 代价是刷新会重放这次提交 —— 而开关是幂等的（再关一次还是关），与 order 的写动作同一取舍。
func mcpJump(c *gin.Context, ok bool, msg string) {
	back := mcpPagePath
	backText := shell.TranslateFor(c)("admin.ai.mcp.title", "MCP 与外部访问")
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
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

// 模板名与页面路径。
const (
	// modelsTemplate 模型目录片段（每张供应商卡片里那块）。
	modelsTemplate = "admin/ai/provider_models"
	// pickerTemplate 候选弹窗片段（插进 #ai-picker-host）。
	pickerTemplate = "admin/ai/provider_picker"
	// pagePath 统一入口（532 合并后模型与会话共用一个页面），也是各写操作 PRG 的回跳地址。
	pagePath = "/admin/ai/sessions"
)

// PageHandle 后台页面的处理器。
type PageHandle struct {
	svc aicontract.AIService
}

// NewPageHandle 构造页面处理器。
func NewPageHandle(svc aicontract.AIService) *PageHandle { return &PageHandle{svc: svc} }

// ProvidersPage GET /admin/ai/providers → 302 到统一入口的模型标签。
//
// 两个菜单合成一个（532）之后这一页不再有独立模板：留着 302 是为了旧书签与外部链接，
// 而不是「还有第二条渲染路径」—— 页面本体只有 admin/ai/sessions.html 一份。
func (h *PageHandle) ProvidersPage(c *gin.Context) {
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels)
}

// ProviderSave POST /admin/ai/providers/save → 新建 / 更新供应商（PRG）。
func (h *PageHandle) ProviderSave(c *gin.Context) {
	req := &aidto.SaveProviderReq{
		ID:          parseInt64(c.PostForm("providerId")),
		ProviderKey: c.PostForm("providerKey"),
		DisplayName: c.PostForm("displayName"),
		BaseURL:     c.PostForm("baseUrl"),
		Protocol:    c.PostForm("protocol"),
		APIKey:      c.PostForm("apiKey"),
		Status:      statusPtr(c.PostForm("status")),
		Sort:        int(parseInt64(c.PostForm("sort"))),
		Version:     parseInt64(c.PostForm("version")),
		UpdateBy:    userID(c),
	}
	applyPresetDefaults(req)
	if _, err := h.svc.SaveProvider(c.Request.Context(), req); err != nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	h.pageNotice(c, "done", aienums.MsgSaved, nil)
}

// applyPresetDefaults 用内置预设补齐「第三方模型提供商」tab 留空的字段。
//
// 只补展示名与协议两处：预设 tab 的表单刻意不强制用户填它们（下拉里选一家就够了），
// 未登记协议的家预设协议本身为空 → 兜底后仍为空，仍由 service 的协议校验拒绝，
// 不静默回落成默认协议。
//
// **不补 base_url**：留空是有含义的（「用该提供商的默认地址」，见卡片里的 hint），
// 在这里填死会把「跟随预设」变成「拷了一份快照」，预设更新再也影响不到它。
func applyPresetDefaults(req *aidto.SaveProviderReq) {
	if req == nil {
		return
	}
	preset, ok := aiservice.BuiltinPreset(req.ProviderKey)
	if !ok {
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = preset.DisplayName
	}
	if strings.TrimSpace(req.Protocol) == "" {
		req.Protocol = preset.Protocol
	}
}

// ProviderDelete POST /admin/ai/providers/delete → 删除供应商（PRG）。
func (h *PageHandle) ProviderDelete(c *gin.Context) {
	id := parseInt64(c.PostForm("providerId"))
	if err := h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	h.pageNotice(c, "done", aienums.MsgDeleted, nil)
}

// ProviderStatus POST /admin/ai/providers/status → 启停供应商（PRG）。
func (h *PageHandle) ProviderStatus(c *gin.Context) {
	req := &aidto.SetStatusReq{
		ID:       parseInt64(c.PostForm("providerId")),
		Status:   int(parseInt64(c.PostForm("status"))),
		Version:  parseInt64(c.PostForm("version")),
		UpdateBy: userID(c),
	}
	if _, err := h.svc.SetProviderStatus(c.Request.Context(), req); err != nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	h.pageNotice(c, "done", aienums.MsgStatusChanged, nil)
}

// ModelsSave POST /admin/ai/providers/models/save → 整组保存模型目录（HTMX 片段）。
func (h *PageHandle) ModelsSave(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	version := parseInt64(c.PostForm("version"))
	rows := parseModelRows(c)
	provider, err := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: providerID,
		Version:    version,
		Models:     rows,
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, rows, err)
		return
	}
	h.modelsSuccess(c, *provider, aienums.MsgModelsSaved, nil)
}

// ModelsRestore POST /admin/ai/providers/models/restore → 恢复内置默认模型（HTMX 片段）。
func (h *PageHandle) ModelsRestore(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	provider, err := h.svc.RestoreDefaultModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, parseModelRows(c), err)
		return
	}
	h.modelsSuccess(c, *provider, aienums.MsgModelsRestored, nil)
}

// ModelsFetch POST /admin/ai/providers/models/fetch → 拉取可用模型并合并（HTMX 片段）。
//
// 回执带计数（拉回 {n} 个、新增 {m} 个）：计数走文案参数，不拼进中文串 ——
// 拼出来的串不在白名单里，关掉 JS 的 PRG 路径会把提示整条丢掉。
func (h *PageHandle) ModelsFetch(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	result, err := h.svc.FetchAvailableModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.modelsFailure(c, providerID, parseModelRows(c), err)
		return
	}
	if result == nil {
		h.modelsSuccess(c, aidto.Provider{}, aienums.MsgModelsFetched, nil)
		return
	}
	h.modelsSuccess(c, *result.Provider, aienums.MsgModelsFetchedDetail, map[string]string{
		"n": strconv.Itoa(result.Fetched),
		"m": strconv.Itoa(len(result.Added)),
	})
}

// ModelsCandidates POST /admin/ai/providers/models/candidates → 只拉候选，整页渲染 + 候选弹窗。
//
// 为什么回整页而不是 htmx 片段：候选弹窗挂在页面上，片段换入后还得再触发打开（多一层时序）；
// 整页渲染里弹窗自带 data-modal-auto-open，控件扫描时自动打开。
// 这次请求**不写库**，卡片状态不因它变化，所以整页重渲染没有副作用。
func (h *PageHandle) ModelsCandidates(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	result, err := h.svc.FetchModelCandidates(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: providerID,
		Version:    parseInt64(c.PostForm("version")),
		UpdateBy:   userID(c),
	})
	if err != nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	if result == nil || result.Provider == nil {
		h.pageNotice(c, "err", aienums.ErrProviderNotFound, nil)
		return
	}
	picker := gin.H{
		"Provider":   result.Provider,
		"Candidates": result.Candidates,
		"Existing":   idSet(result.Existing),
	}
	// 两个页面合成一个（532 的菜单合并）之后，弹窗不再靠「重渲染整页 + data-modal-auto-open」，
	// 而是作为片段插进模型标签底部的 #ai-picker-host —— 整页重渲染会把用户在同一页其它卡片里
	// 刚填的密钥冲掉。非 htmx 请求（没有 JS）退回整页入口，至少不丢功能。
	page := shell.Prepare(c, gin.H{
		"title":  shell.TranslateFor(c)(aienums.AdminLLMTitle, "大模型管理"),
		"Picker": picker,
	})
	if hxFragment(c, pickerTemplate, page) {
		return
	}
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels+"&picker="+strconv.FormatInt(providerID, 10))
}

// ModelsAppend POST /admin/ai/providers/models/append → 把弹窗勾中的模型追加进目录（PRG）。
//
// 追加语义而不是整组覆盖：弹窗只交回「这次勾中的 id」，用覆盖会把这些之外的行全删掉。
// 已存在的 id 跳过 —— 弹窗里那几行是禁用状态，但请求可以被绕过，服务端仍然要判。
func (h *PageHandle) ModelsAppend(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	selected := normalizeSelectedIDs(c.PostFormArray("modelIds"))
	if len(selected) == 0 {
		h.pageNotice(c, "err", aienums.ErrNoModelSelected, nil)
		return
	}
	if len(selected) > maxModelAppend {
		h.pageNotice(c, "err", aienums.ErrInvalidParam, nil)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), providerID)
	if err != nil || provider == nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	have := idSet(modelIDs(provider.Models))
	rows := append([]aidto.ModelEntry{}, provider.Models...)
	added := 0
	for _, id := range selected {
		if have[id] {
			continue
		}
		have[id] = true
		rows = append(rows, aidto.ModelEntry{ID: id, DisplayName: id, InputTypes: []string{aienums.InputTypeText}})
		added++
	}
	if added == 0 {
		// 勾中的全在目录里（或与库里重复）：没有产生任何新增，回一条能看懂的提示，
		// 而不是「操作成功」—— 那会让用户以为目录多了一批模型。
		h.pageNotice(c, "err", aienums.ErrNoModelSelected, nil)
		return
	}
	if _, serr := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: providerID,
		Version:    provider.Version,
		Models:     rows,
		UpdateBy:   userID(c),
	}); serr != nil {
		h.pageNotice(c, "err", aiErrKey(serr), nil)
		return
	}
	h.pageNotice(c, "done", aienums.MsgModelsAppended, map[string]string{"n": strconv.Itoa(added)})
}

// maxModelAppend 一次可追加的模型上限：请求方不该决定服务端的循环次数
// （候选最多几百条，2000 是「正常用量不可能碰到、畸形请求早失败」的界线）。
const maxModelAppend = 2000

// normalizeSelectedIDs 归一提交的模型 id：去空白、丢空串、按首次出现去重（保持提交顺序）。
func normalizeSelectedIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// modelIDs 取目录里各行的 id。
func modelIDs(models []aidto.ModelEntry) []string {
	out := make([]string, 0, len(models))
	for i := range models {
		out = append(out, models[i].ID)
	}
	return out
}

// idSet 把 id 列表转成集合（候选弹窗标「已在目录」、追加时判重都用它）。
func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ModelsRowAdd POST /admin/ai/providers/models/row/add → 追加一个空行（HTMX 片段，不落库）。
func (h *PageHandle) ModelsRowAdd(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := append(parseModelRows(c), aidto.ModelEntry{InputTypes: []string{aienums.InputTypeText}})
	h.renderRows(c, providerID, rows, "", nil, false)
}

// ModelsRowDelete POST /admin/ai/providers/models/row/delete → 删除第 rowIndex 行
// （HTMX 片段，不落库；行序号是渲染时编的，服务端按提交内容整体重渲染）。
func (h *PageHandle) ModelsRowDelete(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := parseModelRows(c)
	index := int(parseInt64(c.PostForm("rowIndex")))
	if index >= 0 && index < len(rows) {
		rows = append(rows[:index], rows[index+1:]...)
	}
	h.renderRows(c, providerID, rows, "", nil, false)
}

// renderRows 用给定行重渲染目录区（增删行路径；失败则整页重定向）。
func (h *PageHandle) renderRows(c *gin.Context, providerID int64, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) {
	provider, err := h.svc.GetProvider(c.Request.Context(), providerID)
	if err != nil || provider == nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	if hxFragment(c, modelsTemplate, h.modelsData(c, *provider, rows, noticeKey, params, isErr)) {
		return
	}
	h.pageNotice(c, redirectSlot(isErr), noticeKey, params)
}

// modelsSuccess 成功路径：用库里的行（归一后的结果）+ 提示渲染目录区。
func (h *PageHandle) modelsSuccess(c *gin.Context, provider aidto.Provider, noticeKey string, params map[string]string) {
	if hxFragment(c, modelsTemplate, h.modelsData(c, provider, provider.Models, noticeKey, params, false)) {
		return
	}
	h.pageNotice(c, "done", noticeKey, params)
}

// modelsFailure 失败路径：用**用户提交的行**渲染（保留输入）+ 错误提示。
//
// 版本冲突时也要走到这里：把用户刚填的内容丢掉换成库里的旧值，是最让人恼火的失败方式。
func (h *PageHandle) modelsFailure(c *gin.Context, providerID int64, rows []aidto.ModelEntry, err error) {
	key := aiErrKey(err)
	provider, gerr := h.svc.GetProvider(c.Request.Context(), providerID)
	if gerr != nil || provider == nil {
		h.pageNotice(c, "err", key, nil)
		return
	}
	if hxFragment(c, modelsTemplate, h.modelsData(c, *provider, rows, key, nil, true)) {
		return
	}
	h.pageNotice(c, "err", key, nil)
}

// modelsData 目录区的模板数据：Provider 提供版本号等库内事实，Rows 提供要渲染的行。
//
// noticeKey 是 i18n key（不是译文）；这里翻成当前语言并填充 {n}/{m} 后才交给模板。
func (h *PageHandle) modelsData(c *gin.Context, provider aidto.Provider, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) gin.H {
	provider.Models = rows
	return shell.Prepare(c, gin.H{
		"Provider":        provider,
		"Rows":            modelRows(rows),
		"ModelsNotice":    facingTextParams(c, noticeKey, params),
		"ModelsNoticeErr": isErr,
		// 分档与无障碍角色在**这一份**数据里也要给全：provider_models.html 有两条渲染路径
		//（整页 include 走 cardData、htmx 片段单独渲染走这里），只补一处会渲染出 `badge badge-`。
		"ModelsNoticeTone": noticeTone(isErr),
		"ModelsNoticeRole": noticeRole(isErr),
	})
}

// pageNotice 供应商 / 模型写动作的结论出口。
//
// key 非空：翻成当前语言后渲染提示页，回大模型管理页。文案走响应体，
// 不再写进 ?err= / ?done=。
// key 为空（增删行、不落库、也没有一句要说的话）：只回到页面，不插提示页。
func (h *PageHandle) pageNotice(c *gin.Context, slot, key string, params map[string]string) {
	if strings.TrimSpace(key) == "" {
		redirectWhere(c, pagePath)
		return
	}
	aiSessionsJump(c, slot != "err", facingTextParams(c, key, params), pagePath)
}

// aiSessionsJump 渲染整页提示，回跳地址只带筛选、不带结论文案。
func aiSessionsJump(c *gin.Context, ok bool, msg, back string) {
	if strings.TrimSpace(back) == "" {
		back = pagePath
	}
	backText := shell.TranslateFor(c)(aienums.AdminLLMTitle, "大模型管理")
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// redirectSlot PRG 回执落在哪个槽位（错误 → err，其余 → done）。
func redirectSlot(isErr bool) string {
	if isErr {
		return "err"
	}
	return "done"
}

// facingTextParams 按 key 取当前语言的文案并填充占位符（未登记的 key 原样返回）。
func facingTextParams(c *gin.Context, key string, params map[string]string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	text, ok := aienums.FacingText(key)
	if !ok {
		return key
	}
	return aienums.FormatFacing(shell.TranslateFor(c)(key, text), params)
}

// facingText 按 key 取当前语言的文案（未登记的 key 原样返回）。
func facingText(c *gin.Context, key string) string { return facingTextParams(c, key, nil) }

// builtinProviderKeys 内置供应商键（模板 datalist 用）。
func builtinProviderKeys() []string { return aiservice.BuiltinProviderKeys() }

// isHXRequest 判断是不是 htmx 发出的请求。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// redirectWhere 按请求类型选择跳转方式：HTMX 用 HX-Redirect（浏览器整页跳转），
// 原生表单用 302（PRG 的 P 部分）。
func redirectWhere(c *gin.Context, target string) {
	if isHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	// 303：POST 之后换成 GET 去看结果（PRG）。302 在部分客户端会被当成「重放原方法」，
	// 于是「提交一次」变成「提交两次」——表单页尤其明显。
	c.Redirect(http.StatusSeeOther, target)
}

// hxFragment 渲染一个可独立渲染的片段；非 HTMX 请求返回 false（调用方走整页重定向）。
func hxFragment(c *gin.Context, name string, data gin.H) bool {
	if !isHXRequest(c) {
		return false
	}
	c.HTML(http.StatusOK, name, data)
	return true
}

// parseInt64 宽容解析整数（空串 / 非法值回 0）。
func parseInt64(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// statusPtr 可选的「启用 / 停用」表单值：空串回 nil（= 让 service 用默认的启用），
// 有值才表态。非空但解析不出来按停用处理（fail-closed），不静默变启用。
func statusPtr(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v := aienums.StatusDisabled
	if parseInt64(raw) == int64(aienums.StatusEnabled) {
		v = aienums.StatusEnabled
	}
	return &v
}

// userID 当前登录用户 ID（写操作的 update_by / create_by）。
func userID(c *gin.Context) int64 {
	return int64(shell.CurrentUserID(c))
}

// modelRows 把模型目录转成模板用的行视图。
//
// 转换放在 Go 侧而不是模板里：模板只做「取字段 + isset 判断」，
// 「哪些输入类型勾上了」这类判断不该由模板表达式承担（错了是整页 500）。
func modelRows(models []aidto.ModelEntry) []gin.H {
	rows := make([]gin.H, 0, len(models))
	for i := range models {
		hasText, hasImage := false, false
		for _, t := range models[i].InputTypes {
			switch t {
			case aienums.InputTypeText:
				hasText = true
			case aienums.InputTypeImage:
				hasImage = true
			}
		}
		rows = append(rows, gin.H{
			"Index":           i,
			"ID":              models[i].ID,
			"DisplayName":     models[i].DisplayName,
			"ContextWindow":   models[i].ContextWindow,
			"MaxOutputTokens": models[i].MaxOutputTokens,
			"TextChecked":     hasText,
			"ImageChecked":    hasImage,
		})
	}
	return rows
}

// cardData 一个供应商卡片（含模型目录区）的模板数据。
//
// csrf_token / t / PermSet 从页面基座 map 里借（卡片是 include 进整页的，
// 作用域是这张卡片自己的 map，拿不到整页的键）。
func cardData(base gin.H, provider aidto.Provider, notice string, isErr bool, defaultBaseURL string) gin.H {
	return gin.H{
		"Provider":        provider,
		"Rows":            modelRows(provider.Models),
		"ProtocolOptions": aienums.ProtocolOptions,
		"DefaultBaseURL":  defaultBaseURL,
		"BuiltinKeys":     builtinProviderKeys(),
		"ModelsNotice":    notice,
		"ModelsNoticeErr": isErr,
		// 卡片上两处徽标的分档：启停与「模型目录读取结果」。阈值留在这里，
		// 模板只把分档写进 class（原先两处都是 {{if}} 在模板里选 badge-*）。
		"StatusTone":       providerStatusTone(provider.Status),
		"ModelsNoticeTone": noticeTone(isErr),
		"ModelsNoticeRole": noticeRole(isErr),
		"csrf_token":       base["csrf_token"],
		"t":                base["t"],
		"PermSet":          base["PermSet"],
	}
}

// providerStatusTone 供应商启停状态 → 徽标分档。
func providerStatusTone(status int) string {
	if status == 1 {
		return "ok"
	}
	return "warn"
}

// noticeTone 一句「拉取结果」提示 → 徽标分档（失败 warn、正常 ok）。
func noticeTone(isErr bool) string {
	if isErr {
		return "warn"
	}
	return "ok"
}

// noticeRole 同一句提示的无障碍角色（失败是 alert、正常是 status）。
//
// 与 tone 分开两个键而不是让模板判 isErr：role 是语义（要不要打断读屏），
// tone 是外观 —— 两者都从同一个布尔来，模板不该再学一遍这个布尔。
func noticeRole(isErr bool) string {
	if isErr {
		return "alert"
	}
	return "status"
}

// parseModelRows 从表单解析模型目录（表单字段同名多值，按出现顺序与行对齐）。
//
// 输入类型用**行序号后缀**（input_text_0 / input_image_1）：checkbox 只在勾选时提交，
// 靠「同名多值」的数组下标对齐会在「中间某行没勾」时整体错位。行序号由服务端渲染时
// 重新编号（每次增删行都整体重渲染），所以序号始终连续。
func parseModelRows(c *gin.Context) []aidto.ModelEntry {
	ids := c.PostFormArray("modelId")
	names := c.PostFormArray("modelDisplayName")
	windows := c.PostFormArray("modelContextWindow")
	maxOutputs := c.PostFormArray("modelMaxOutputTokens")
	rows := make([]aidto.ModelEntry, 0, len(ids))
	for i := range ids {
		types := make([]string, 0, 2)
		if c.PostForm("input_text_"+strconv.Itoa(i)) != "" {
			types = append(types, aienums.InputTypeText)
		}
		if c.PostForm("input_image_"+strconv.Itoa(i)) != "" {
			types = append(types, aienums.InputTypeImage)
		}
		rows = append(rows, aidto.ModelEntry{
			ID:              ids[i],
			DisplayName:     pick(names, i),
			ContextWindow:   parseInt64(pick(windows, i)),
			MaxOutputTokens: parseInt64(pick(maxOutputs, i)),
			InputTypes:      types,
		})
	}
	return rows
}

// pick 取数组第 i 项（越界回空串）。
func pick(items []string, i int) string {
	if i < 0 || i >= len(items) {
		return ""
	}
	return items[i]
}

const (
	sessionTemplate = "admin/ai/sessions"
	sessionPath     = "/admin/ai/sessions"
	sessionPageSize = 20
	sessionEventTop = 50
	// sessionTrendDays 趋势图窗口（天）。写死而不是做成控件：时间维度已经有 from/to 两个
	// 日期在筛，再加一个「粒度」下拉等于让人自己算区间；窗口跟随筛选区间即可。
	sessionTrendDays = 30
	// 页内两个标签：会话看板与模型配置。
	sessionTabSessions = "sessions"
	sessionTabModels   = "models"
	// 两个标签各自的「进入」权限点（菜单可见性也挂这两个，任一命中即显示菜单）。
	permProviderList = "ai:provider_list"
	permSessionList  = "ai:session_list"
)

// providerLister 会话页需要的最小供应商读取能力（发消息区的 provider 下拉 / model 候选）。
//
// 用窄接口而不是 aiservice.Service：会话页对配置面保持「只读、单向、可替换」，
// 用例测试塞一个假列表即可，不必建出整条配置面。
type providerLister interface {
	ListProviders(ctx context.Context) ([]aidto.Provider, error)
}

// SessionPageHandle 会话管理页处理器。
type SessionPageHandle struct {
	svc       *aiservice.SessionService
	providers providerLister
}

// NewSessionPageHandle 构造；providers 为 nil 时不渲染发消息区的供应商候选。
func NewSessionPageHandle(svc *aiservice.SessionService, providers providerLister) *SessionPageHandle {
	return &SessionPageHandle{svc: svc, providers: providers}
}

// SessionsPage GET /admin/ai/sessions：AI 会话页 —— 用量看板（指标卡 + 趋势图）
// + 多维筛选 + 会话明细表，带 ?id= 时同时渲染详情；页内第二个标签是模型配置。
//
// 两个标签页在同一个页面里由服务端一次渲染完（不跳转、不 htmx 二次请求）：
// 「模型」那一块就是配置页同一套供应商卡片，切标签不该让整页重新走一遍请求。
func (h *SessionPageHandle) SessionsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	if page < 1 {
		page = 1
	}
	filter := aidto.SessionQuery{
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Status:      sessionIntOr(c.DefaultQuery("status", "-1"), -1),
		ProviderKey: strings.TrimSpace(c.Query("provider")),
		ModelID:     strings.TrimSpace(c.Query("model")),
		CreateBy:    parseInt64(c.Query("user")),
		From:        strings.TrimSpace(c.Query("from")),
		To:          strings.TrimSpace(c.Query("to")),
	}

	rows, total, err := h.svc.ListSessionsFiltered(ctx, filter, page, sessionPageSize)
	if err != nil {
		shell.PageError(c, "ai", err)
		return
	}
	tab := sessionTabOf(c.Query("tab"))
	// 停在没权限的标签上等于给用户一个空面板：切到另一个。两个都没有时保持原样 ——
	// 页面渲染成「没有可用的 AI 功能」空态，比白屏或 403 都清楚。
	canModels := sessionCan(c, permProviderList)
	canSessions := sessionCan(c, permSessionList)
	if tab == sessionTabModels && !canModels && canSessions {
		tab = sessionTabSessions
	} else if tab == sessionTabSessions && !canSessions && canModels {
		tab = sessionTabModels
	}
	data := shell.Prepare(c, gin.H{
		"title": shell.TranslateFor(c)(aienums.AdminLLMTitle, "大模型管理"),
		// Rows 是 []gin.H（每项 {Session, Usage}），不是 []aidto.Session：
		// 每行要带上它自己的按 (供应商, 模型) 拆分，而模板对 map 做动态索引
		// （.ModelUsage[r.ID]）依赖 Jet 的索引表达式；拼成同序的嵌套结构没这层不确定性。
		"Rows":     h.sessionRows(ctx, rows),
		"Total":    total,
		"Page":     page,
		"PageSize": sessionPageSize,
		"Filter":   filter,
		"Keyword":  filter.Keyword,
		"Status":   filter.Status,
		"Tab":      tab,
		// IsModels 是给模板用的布尔：在模板里比较字符串（.Tab == "models"）
		// 要依赖 Jet 的比较表达式求值，多一处能出错的地方。
		"IsModels": tab == sessionTabModels,
		// 标签按权限裁剪：没权限的标签连按钮都不渲染（模板据此包 {{if}}）。
		"CanViewModels":   canModels,
		"CanViewSessions": canSessions,
	})

	// 详情（?id=）先装配：它决定详情区显示什么，而看板与卡片与它无关，各自独立降级。
	h.attachSessionDetail(c, ctx, data)
	h.attachSessionUsage(c, ctx, data, filter)
	h.attachProviderCards(c, ctx, data)

	// 分页链接在服务端拼好带着全部筛选条件：让模板自己拼 query 就得在每个翻页链接里
	// 把七八个参数各写一遍，漏一个就变成「翻页把筛选清空」。
	totalPages := int((total + int64(sessionPageSize) - 1) / int64(sessionPageSize))
	data["TotalPages"] = totalPages
	data["HasPrev"] = page > 1
	data["HasNext"] = page < totalPages
	if page > 1 {
		data["PrevURL"] = sessionPageURL(c, page-1)
	}
	if page < totalPages {
		data["NextURL"] = sessionPageURL(c, page+1)
	}

	c.HTML(http.StatusOK, sessionTemplate, data)
}

// sessionRows 把会话列表拼成模板行数据：每行带上它自己的按 (供应商, 模型) 拆分。
//
// 拆分一次查完整页（见 SessionService.SessionModelUsageOf），拉取失败不阻断整页 ——
// 行照常渲染，只是悬浮卡里没有明细（给空切片，模板不必再写一层 isset）。
func (h *SessionPageHandle) sessionRows(ctx context.Context, rows []aidto.Session) []gin.H {
	out := make([]gin.H, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	usage, err := h.svc.SessionModelUsageOf(ctx, ids)
	if err != nil {
		usage = map[int64][]aidto.SessionModelUsage{}
	}
	// 调用流水预览（最近几次调用各自多久、多少 token、成没成）：同样一次查完整页。
	// 失败也不阻断整页 —— 悬浮卡少一段「最近调用」，聚合那段照常显示。
	calls, cerr := h.svc.SessionCallsOf(ctx, ids)
	if cerr != nil {
		calls = map[int64]aidto.SessionCalls{}
	}
	for i := range rows {
		u := usage[rows[i].ID]
		if u == nil {
			u = []aidto.SessionModelUsage{}
		}
		out = append(out, gin.H{"Session": rows[i], "Usage": u, "Calls": calls[rows[i].ID]})
	}
	return out
}

// attachSessionUsage 装配用量看板：指标卡 + 趋势图 + 三个筛选下拉的候选值。
//
// 统计失败不阻断整页：列表与详情照常可用，只是看板那几张卡显示不出来 ——
// 一个聚合查询超时把整页打成 500 不成比例。失败信息交给模板在看板区提示。
func (h *SessionPageHandle) attachSessionUsage(c *gin.Context, ctx context.Context, data gin.H, filter aidto.SessionQuery) {
	usage, err := h.svc.SessionUsageOf(ctx, filter)
	if err != nil {
		data["UsageErr"] = aiErrText(c, err)
		// 仍然给零值：模板不必为「这个键可能不存在」再写一遍 isset 分支。
		usage = aidto.SessionUsage{}
	}
	data["Usage"] = usage

	// 趋势单独取：它按 (供应商, 模型) 分组，比指标卡多一次聚合；两者任一个失败都只
	// 影响自己那一块，不把整页打成 500。窗口两端与峰值由 service 一并给出（见 SessionTrend），
	// 模板不再自己遍历取首尾 —— Jet 里取切片首尾要判空索引。
	trend, terr := h.svc.SessionTrend(ctx, filter, sessionTrendDays)
	if terr != nil {
		if _, has := data["UsageErr"]; !has {
			data["UsageErr"] = aiErrText(c, terr)
		}
		trend = aidto.SessionTrend{}
	}
	data["Trend"] = trend

	opts, oerr := h.svc.SessionFilterOptions(ctx)
	if oerr != nil {
		data["OptionsErr"] = aiErrText(c, oerr)
		// 候选拉不到时给空集合而不是 nil：下拉里只剩「全部」，筛选仍可用 URL 手填。
		opts = aidto.SessionFilterOptions{Providers: []string{}, Models: []string{}, Creators: []int64{}}
	}
	data["FilterOptions"] = opts
}

// attachProviderCards 装配第二个标签（模型配置）要用的供应商卡片。
//
// 复用配置面的 cardData：两块页面说的是同一批供应商，各拼一份数据必然漂移
// （加了字段只改一处）。数据仍从 providerLister 这个只读窄接口来。
func (h *SessionPageHandle) attachProviderCards(c *gin.Context, ctx context.Context, data gin.H) {
	data["Presets"] = aiservice.BuiltinPresets()
	data["BuiltinKeys"] = aiservice.BuiltinProviderKeys()
	data["ProtocolOptions"] = aienums.ProtocolOptions
	data["ProviderCards"] = []gin.H{}
	h.attachProviders(c, ctx, data, true)
}

// attachProviders 拉一次供应商列表，写进 data 的 Providers；withCards 时顺带拼好卡片数据。
//
// 会话列表页、模型标签页、详情抽屉都读这一份 —— 发消息区的候选与模型标签页要的是同一个列表，
// 各拉一次就是三次查询加三处漂移点。
// 拉取失败不阻断整页（或整个抽屉）：只置 ProviderErr，其余部分照常渲染。
func (h *SessionPageHandle) attachProviders(c *gin.Context, ctx context.Context, data gin.H, withCards bool) {
	data["Providers"] = []aidto.Provider{}
	if h.providers == nil {
		return
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		data["ProviderErr"] = aiErrText(c, err)
		return
	}
	data["Providers"] = list
	if !withCards {
		return
	}
	cards := make([]gin.H, 0, len(list))
	for i := range list {
		cards = append(cards, cardData(data, list[i], "", false, aiservice.BuiltinBaseURL(list[i].ProviderKey)))
	}
	data["ProviderCards"] = cards
}

// sessionTabOf 归一标签参数：只认 "models"，其余一律回会话标签。
//
// 不做严格校验：手改错一个字母时落回默认标签，比回 400 有用。
func sessionTabOf(raw string) string {
	// 默认停在模型标签：合并成一个「大模型管理」入口之后，模型是第一个标签（m00210），
	// 默认标签必须与它一致，否则点菜单进来会落在第二个标签上。
	if strings.EqualFold(strings.TrimSpace(raw), sessionTabSessions) {
		return sessionTabSessions
	}
	return sessionTabModels
}

// sessionCan 读本请求的权限码集合（shell.PermSetKey 由 Casbin 中间件注入）。
//
// 页面路由只挂了「进入页面」的那一个 obj，标签内部各自的动作权限点要在这里判 ——
// 没权限的标签不渲染，用户不会看到一个点进去全是 403 的入口。
func sessionCan(c *gin.Context, code string) bool {
	v, ok := c.Get(shell.PermSetKey)
	if !ok {
		return false
	}
	set, ok := v.(map[string]bool)
	if !ok {
		return false
	}
	return set[code]
}

// sessionPageURL 拼一个「只改页码、其余筛选照旧」的列表页 URL。
//
// 刻意不带 ?id=：翻页时那条会话的详情已经不在当前页上，把它留在 URL 里会让
// 每一次翻页都多一次详情查询，而页面根本不显示它。
func sessionPageURL(c *gin.Context, page int) string {
	q := url.Values{}
	for _, name := range []string{"keyword", "status", "provider", "model", "user", "from", "to", "tab"} {
		if v := strings.TrimSpace(c.Query(name)); v != "" {
			q.Set(name, v)
		}
	}
	q.Set("page", strconv.Itoa(page))
	return sessionPath + "?" + q.Encode()
}

// SessionAppend POST /admin/ai/sessions/append：给指定会话追加一条事件。
func (h *SessionPageHandle) SessionAppend(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.AppendEvent(c.Request.Context(), aidto.AppendEventReq{
		SessionID: id,
		Kind:      strings.TrimSpace(c.PostForm("kind")),
		Content:   strings.TrimSpace(c.PostForm("content")),
		UserID:    userID(c),
	})
	if err != nil {
		h.sessionNotice(c, false, aiErrKey(err))
		return
	}
	h.sessionNotice(c, true, aienums.MsgSessionAppended)
}

// SessionRename POST /admin/ai/sessions/rename：只改标题（乐观锁版本随表单带回）。
func (h *SessionPageHandle) SessionRename(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Title:   strings.TrimSpace(c.PostForm("title")),
		Status:  -1,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.sessionNotice(c, false, aiErrKey(err))
		return
	}
	h.sessionNotice(c, true, aienums.MsgSessionRenamed)
}

// SessionArchive POST /admin/ai/sessions/archive：归档或恢复（表单 status=1 表示恢复）。
func (h *SessionPageHandle) SessionArchive(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	status := int16(aienums.SessionArchived)
	if c.PostForm("status") == "1" {
		status = int16(aienums.SessionActive)
	}
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Status:  status,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.sessionNotice(c, false, aiErrKey(err))
		return
	}
	h.sessionNotice(c, true, aienums.MsgSessionRenamed)
}

// SessionFold POST /admin/ai/sessions/fold：提交一次折叠。
func (h *SessionPageHandle) SessionFold(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.Fold(c.Request.Context(), aidto.FoldReq{
		SessionID: id,
		FromSeq:   parseInt64(c.PostForm("fromSeq")),
		ToSeq:     parseInt64(c.PostForm("toSeq")),
		Summary:   strings.TrimSpace(c.PostForm("summary")),
		UserID:    userID(c),
	})
	if err != nil {
		h.sessionNotice(c, false, aiErrKey(err))
		return
	}
	h.sessionNotice(c, true, aienums.MsgSessionFolded)
}

// SessionSend POST /admin/ai/sessions/send：发一条消息 —— 写 user 事件 → 把当前投影拼成
// 一段文本打一次模型 → 把回复写成 assistant 事件 → 回会话页。
//
// 权限借 /api/ai/chat 的 casbin obj：发消息本质就是一次对话，不新开权限点。
func (h *SessionPageHandle) SessionSend(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.SendMessage(c.Request.Context(), aidto.SendMessageReq{
		SessionID:       id,
		ProviderKey:     strings.TrimSpace(c.PostForm("providerKey")),
		Model:           strings.TrimSpace(c.PostForm("model")),
		Input:           strings.TrimSpace(c.PostForm("input")),
		MaxOutputTokens: parseInt64(c.PostForm("maxOutputTokens")),
		UserID:          userID(c),
	})
	if err != nil {
		h.sessionNotice(c, false, aiErrKey(err))
		return
	}
	h.sessionNotice(c, true, aienums.MsgSessionSent)
}

// sessionNotice 会话写动作的结论出口：提示页，回跳只保留表单带回的筛选。
//
// 不再带 ?id=：详情已经是抽屉，回跳时那句 id 只会让页面白查一次详情而页面并不渲染它。
// 结论文案走响应体，不写进 URL。
func (h *SessionPageHandle) sessionNotice(c *gin.Context, ok bool, key string) {
	aiSessionsJump(c, ok, facingText(c, key), sessionJumpBack(c.PostForm("back")))
}

// sessionJumpBack 校验并清洗抽屉表单带回的返回地址：只留筛选，丢掉旧的 err/done 槽。
func sessionJumpBack(raw string) string {
	target := sessionBackURL(raw)
	u, err := url.Parse(target)
	if err != nil || u.Path != sessionPath {
		return sessionPath
	}
	q := u.Query()
	q.Del("err")
	q.Del("done")
	q.Del("id")
	if len(q) == 0 {
		return sessionPath
	}
	return sessionPath + "?" + q.Encode()
}

// sessionBackURL 校验抽屉表单带回的返回地址。
//
// 只接受本页路径（前缀白名单），其余一律回列表根 —— 这个值会原样变成 302 的 Location，
// 透传就是一个开放重定向。前缀校验足以挡住 `//evil.com`、`https://evil.com` 与
// `/admin/ai/sessions/rename` 这类动作端点，而它要表达的意图本来也只有
// 「回到这个列表的某个筛选组合」。
func sessionBackURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == sessionPath || strings.HasPrefix(raw, sessionPath+"?") {
		return raw
	}
	return sessionPath
}

// sessionDetailEventSize 详情时间线渲染的事件条数上限。
//
// 与 sessionEventTop 同值：详情回答的是「最近发生了什么」，不是全量导出
// （要看完整日志走 /api/ai/session/events 分页）。
const sessionDetailEventSize = sessionEventTop

// attachSessionDetail 装配 ?id= 的会话详情：会话头 + 事件时间线（含图表）+ 折叠预览。
//
// 只在带 ?id= 时装配：列表与详情是同一个 URL，详情是叠加在列表上的一块；
// 不带 id 的请求不该为它多查三次库。
//
// 三块各自独立降级（各自一个 Err 键）：一个会话的事件查询失败不该把整页打成 500，
// 也不该让另外两块跟着消失。
func (h *SessionPageHandle) attachSessionDetail(c *gin.Context, ctx context.Context, data gin.H) {
	raw := strings.TrimSpace(c.Query("id"))
	if raw == "" {
		return
	}
	id := parseInt64(raw)
	if id <= 0 {
		data["DetailErr"] = internalFallback(c)
		return
	}

	detail, err := h.svc.GetSession(ctx, id)
	if err != nil {
		// 会话头取不到就没必要再查事件：详情区只显示这一条提示。
		data["DetailErr"] = aiErrText(c, err)
		return
	}
	data["Detail"] = detail
	data["DetailID"] = id

	events, _, err := h.svc.ListEvents(ctx, id, 1, sessionDetailEventSize)
	if err != nil {
		data["EventsErr"] = aiErrText(c, err)
	} else {
		data["Events"] = sessionEventRows(events)
	}

	// 折叠预览只算不写（FoldPlan 不落库），失败只影响这一块。
	plan, err := h.svc.FoldPlan(ctx, aidto.FoldPlanReq{SessionID: id})
	if err != nil {
		data["FoldPlanErr"] = aiErrText(c, err)
	} else {
		data["FoldPlan"] = plan
	}
}

// sessionEventRows 把事件翻成模板行：类型分档 + 正文 + 可渲染的图表视图。
func sessionEventRows(events []aidto.SessionEventItem) []gin.H {
	out := make([]gin.H, 0, len(events))
	for i := range events {
		e := events[i]
		views := renderViewsOf(e.Meta)
		row := gin.H{
			"Seq":       e.Seq,
			"Kind":      e.Kind,
			"Tone":      sessionEventTone(e.Kind),
			"Content":   e.Content,
			"Views":     views,
			"HasViews":  len(views) > 0,
			"TimeLabel": sessionTimeLabel(e.CreateTime),
			"Tool":      "",
			"Phase":     "",
			// ui_blocks.html 读的是上下文键 **Views**（它自己 range .Views），
			// 所以 include 时得给它一个带这个键的上下文，而不是直接把切片丢过去。
			"ViewsCtx": gin.H{"Views": views},
		}
		// 工具事件把工具名与阶段提出来单独显示：正文里可能只有「工具执行失败」几个字，
		// 是哪个工具、是调用还是结果，只有结构化字段说得清。
		if e.Meta != nil {
			if v, ok := e.Meta["tool"].(string); ok {
				row["Tool"] = v
			}
			if v, ok := e.Meta["phase"].(string); ok {
				row["Phase"] = v
			}
		}
		out = append(out, row)
	}
	return out
}

// renderViewsOf 把事件 meta 里的 render 结构解成视图。
//
// 解不出来就返回 nil（那一块不渲染），**不报错**：库里可能躺着旧版本写下的结构、
// 或者被人手工改过；一个坏结构不该让整页 500，也不该让同一轮别的块跟着消失。
func renderViewsOf(meta map[string]any) []uispec.View {
	if len(meta) == 0 {
		return nil
	}
	raw, ok := meta["render"].(string)
	if !ok || raw == "" {
		return nil
	}
	var views []uispec.View
	if err := json.Unmarshal([]byte(raw), &views); err != nil {
		logger.Scene("ai").Warn("事件里的渲染结构解不出来，已跳过：" + err.Error())
		return nil
	}
	return views
}

// sessionEventTone 事件类型 → 徽标分档（模板据此选 class）。
//
// 分档而不是直接把 kind 当类名：kind 是数据、类名是样式契约，
// 让数据直接进 class 会让「新增一种 kind」变成「页面上冒出一个没有样式的徽标」。
func sessionEventTone(kind string) string {
	switch kind {
	case string(aienums.EventKindUser):
		return "info"
	case string(aienums.EventKindAssistant):
		return "ok"
	case string(aienums.EventKindTool):
		return "warn"
	default:
		return ""
	}
}

// sessionTimeLabel 事件时间的展示串（Jet 侧没有日期函数，格式化只能在 Go 里做）。
func sessionTimeLabel(t utils.JSONTime) string {
	if t.IsZero() {
		return ""
	}
	return t.Time().Format("01-02 15:04")
}
