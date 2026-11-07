package aihttp

import (
	"cmp"
	"context"
	"go_wp/internal/middleware/builtin"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pagination"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/adminctx"
	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/mcp"
	"go_wp/pkg/uispec"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const fabSessionKey = "admin-fab"

func fabSessionKeyOf(uid int64) string {
	return fabSessionKey + ":" + strconv.FormatInt(uid, 10)
}

const fabInputLimit = 4000

const fabContextQueryLimit = 300

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

const aiProviderEnabled = 1

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

const (
	mcpPagePath = "/admin/ai/mcp"
)

const (
	sessionPath = "/admin/ai/sessions"

	sessionPageSize = 20

	// sessionEventTop 是「会话详情里先显示最近多少条事件」。
	// 页面只做阅读起点，翻页由 /api/ai/session/events 承担。
	sessionEventTop = 50

	sessionTrendDays = 30

	sessionTabSessions = "sessions"
	sessionTabModels   = "models"

	permProviderList = "ai:provider_list"
	permSessionList  = "ai:session_list"
)

// providerLister 是会话页需要的「供应商清单」端口。
//
// 页面只要一份列表（卡片渲染用），不该依赖整个 AI Service：端口小到只有这一条，
// 测试塞一个两行实现就够了。
type providerLister interface {
	ListProviders(ctx context.Context) ([]aidto.Provider, error)
}

// sessionServicePort 是会话页对会话服务的最小依赖面。
//
// 与 contract.AIService 分开：那是给 /api 用的全集，页面只要读视图。
// 端口在此收窄，页面 handler 就无法顺手调到别的域的能力。
type sessionServicePort interface {
	ListSessionsFiltered(ctx context.Context, f aidto.SessionQuery, page, size int) ([]aidto.Session, int64, error)
	SessionModelUsageOf(ctx context.Context, sessionIDs []int64) (map[int64][]aidto.SessionModelUsage, error)
	SessionCallsOf(ctx context.Context, sessionIDs []int64) (map[int64]aidto.SessionCalls, error)
	SessionUsageOf(ctx context.Context, f aidto.SessionQuery) (aidto.SessionUsage, error)
	SessionTrend(ctx context.Context, f aidto.SessionQuery, trendDays int) (aidto.SessionTrend, error)
	SessionFilterOptions(ctx context.Context) (aidto.SessionFilterOptions, error)
	GetSession(ctx context.Context, id int64) (*aidto.SessionDetail, error)
	ListEvents(ctx context.Context, sessionID int64, page, size int) ([]aidto.SessionEventItem, int64, error)
	AppendEvent(ctx context.Context, req aidto.AppendEventReq) (*aidto.AppendEventResult, error)
	RenameSession(ctx context.Context, req aidto.RenameSessionReq) (*aidto.Session, error)
	Fold(ctx context.Context, req aidto.FoldReq) (*aidto.FoldResult, error)
	FoldPlan(ctx context.Context, req aidto.FoldPlanReq) (*aidto.FoldPlanResult, error)
	SendMessage(ctx context.Context, req aidto.SendMessageReq) (*aidto.SendMessageResult, error)
	SendMessageStream(ctx context.Context, req aidto.SendMessageReq, emit func(aiservice.StreamEvent)) (*aidto.SendMessageResult, error)
	RecentDialogue(ctx context.Context, key string, limit int, userID int64) ([]aidto.DialogueTurn, error)
}

// sessionDetailEventSize 是会话详情先取多少条事件（与列表页的首屏口径一致）。
const sessionDetailEventSize = sessionEventTop

func NewSessionPageHandle(svc sessionServicePort, providers providerLister) *SessionPageHandle {
	return &SessionPageHandle{svc: svc, providers: providers}
}

// SessionPageHandle 是「AI 会话」页的读视图与 SSE 推送。
//
// 会话与供应商的普通写操作都不在这里：它们只有 /api 一条入口（见 ai_router.go），
// 页面侧的 POST 双轨已经清退。留在页面组上的只有整页读取、SSE 流与只读历史。
type SessionPageHandle struct {
	svc       sessionServicePort
	providers providerLister
}

type McpPageHandle struct {
	tokens   *aiservice.AccessTokenService
	registry *mcp.Registry
	config   sysconfigcontract.Service
	dir      aicontract.PermDirectory
}

func NewMcpPageHandle(tokens *aiservice.AccessTokenService, registry *mcp.Registry) *McpPageHandle {
	return &McpPageHandle{tokens: tokens, registry: registry}
}

func (h *McpPageHandle) SetConfigService(s sysconfigcontract.Service) { h.config = s }

// SetPermDirectory 注入权限目录读口：scope 选项的名称只来自数据库，不维护代码侧名称表。
func (h *McpPageHandle) SetPermDirectory(d aicontract.PermDirectory) { h.dir = d }

func (h *McpPageHandle) Page(c *gin.Context) {
	data := h.pageData(c)
	if data == nil {
		return
	}
	pageview.Render(c, data)
}

// pageData 只服务整页渲染。令牌的生成与撤销都回 JSON 事实，不再借这块数据
// 拼「一次性明文 / 回执」这类只有片段刷新才需要的字段。
func (h *McpPageHandle) pageData(c *gin.Context) *aidto.McpPageResp {
	ctx := c.Request.Context()

	tokens, err := h.tokens.List(ctx, &aidto.TokenListReq{All: true, UserID: userID(c)})
	if err != nil {
		// 列表读不出来就让整页失败：假装「还没有令牌」会让页面说假话。
		aiPageFail(c, err)
		return nil
	}

	enabled := h.mcpEnabled(ctx)
	// 标题与导语不在这里：它们是界面文案，由 mcp.html 用全局 t 取词。
	return &aidto.McpPageResp{
		Endpoint:     mcpEndpointURL(c),
		Tokens:       toAnyRows(tokenRows(tokens)),
		Tools:        toAnyRows(h.tools()),
		ScopeOptions: toAnyRows(h.scopeOptions(ctx)),
		McpEnabled:   enabled,
	}
}

func builtinProviderKeys() []string { return aiservice.BuiltinProviderKeys() }

func parseInt64(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

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

func userID(c *gin.Context) int64 {
	return int64(builtin.CurrentUserID(c))
}

// modelRows 只交模型事实：id / 显示名 / 上下文窗口 / 最大输出 / 输入类型。
//
// 行序号与勾选状态是显示派生，归模板/JS：模板 {{range i, row := .Rows}} 拿序号并自查
// InputTypes 里有没有 text / image，JS 重建行时同样自己算。服务端不再把「第几行」
// 「该不该打勾」算好放进来。
func modelRows(models []aidto.ModelEntry) []aidto.ModelRowResp {
	rows := make([]aidto.ModelRowResp, 0, len(models))
	for i := range models {
		types := models[i].InputTypes
		if types == nil {
			types = []string{}
		}
		rows = append(rows, aidto.ModelRowResp{
			ID:              models[i].ID,
			DisplayName:     models[i].DisplayName,
			ContextWindow:   models[i].ContextWindow,
			MaxOutputTokens: models[i].MaxOutputTokens,
			InputTypes:      types,
		})
	}
	return rows
}

// cardData 是一张供应商卡的整页事实。
//
// 卡片里没有「模型区提示条」：模型的写动作一律回 JSON，提示由前端按 payload.message
// 显示（见 static/js/ui/ai-models.js），服务端不预置一条空提示位。
func cardData(base aidto.ProviderCardBase, provider aidto.Provider, defaultBaseURL string) aidto.ProviderCardResp {
	return aidto.ProviderCardResp{
		Provider:        provider,
		Rows:            modelRows(provider.Models),
		ProtocolOptions: aienums.ProtocolOptions,
		DefaultBaseURL:  defaultBaseURL,
		BuiltinKeys:     builtinProviderKeys(),

		CSRFToken: base.CSRFToken,
		T:         base.T,
		PermSet:   base.PermSet,
		// 透传权限按钮集合：漏了它，卡片里所有 isset(.Buttons[...]) 都是假，
		// 保存/启停/删除/模型区整片隐藏 —— 页面上看着像「没有权限」。
		Buttons: base.Buttons,
	}
}

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

// tokenRow 是令牌行的**事实**形状。
//
// 没有 StatusLabel / ScopesText / LastUsedText / ExpiresText：状态名、作用域分隔符、
// 时间格式都是显示派生，模板用 {{if eq .Status 1}} / {{range .Scopes}} / {{dateTime .LastUsed}}
// 自己决定；JS 重建行时按 TokenItem 的真实键（status / scopes / lastUsedTime）同样自己算。
// 服务端算好放进来会让同一件事有两个事实源。
type tokenRow struct {
	ID        int64
	Name      string
	Prefix    string
	Scopes    []string
	Status    int16
	Revoked   bool
	LastUsed  *utils.JSONTime
	ExpiresAt *utils.JSONTime
}

func tokenRows(items []aidto.TokenItem) []tokenRow {
	out := make([]tokenRow, 0, len(items))
	for _, it := range items {
		scopes := it.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, tokenRow{
			ID:        it.ID,
			Name:      it.Name,
			Prefix:    it.TokenPrefix,
			Scopes:    scopes,
			Status:    it.Status,
			Revoked:   it.Status == int16(aienums.TokenStatusRevoked),
			LastUsed:  it.LastUsedTime,
			ExpiresAt: it.ExpiresAt,
		})
	}
	return out
}

type toolRow struct {
	Name        string
	Description string
	Permission  string
}

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

type scopeOption struct {
	Code  string
	Label string
}

func (h *McpPageHandle) scopeOptions(ctx context.Context) []scopeOption {
	if h.registry == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]scopeOption, 0, 8)
	for _, t := range h.registry.List() {
		p := t.Permission()
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, scopeOption{Code: p, Label: p})
	}
	// 名称一次性批量取；读不到就保留原始码，不让整页失败。
	if h.dir != nil && len(out) > 0 {
		codes := make([]string, 0, len(out))
		for _, o := range out {
			codes = append(codes, o.Code)
		}
		if entries, err := h.dir.List(ctx, codes); err == nil {
			names := make(map[string]string, len(entries))
			for _, e := range entries {
				names[e.Code] = e.Name
			}
			for i := range out {
				if name := names[out[i].Code]; name != "" {
					out[i].Label = name + "（" + out[i].Code + "）"
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b scopeOption) int { return cmp.Compare(a.Code, b.Code) })
	return out
}

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

func (h *SessionPageHandle) SessionsPage(c *gin.Context) {
	h.sessionsPage(c, "")
}

// ProvidersPage 是老地址 /admin/ai/providers 的入口。
//
// 菜单在 532 迁移里已经合并：模型与会话现在是同一个页面的两个标签，
// 唯一入口是 /admin/ai/sessions?tab=models。这个地址留着只为旧书签不 404。
//
// 它不发 302 —— 后端不做成功重定向，导航归前端。直接按模型标签渲染同一个页面：
// 用户看到的内容与从菜单进来完全一致，浏览器也停在他点的那条链接上。
func (h *SessionPageHandle) ProvidersPage(c *gin.Context) {
	h.sessionsPage(c, sessionTabModels)
}

// sessionQueryOf 解析会话列表的页码与筛选条件。
func sessionQueryOf(c *gin.Context) (int, aidto.SessionQuery) {
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	if page < 1 {
		page = 1
	}
	return page, aidto.SessionQuery{
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Status:      sessionIntOr(c.DefaultQuery("status", "-1"), -1),
		ProviderKey: strings.TrimSpace(c.Query("provider")),
		ModelID:     strings.TrimSpace(c.Query("model")),
		CreateBy:    parseInt64(c.Query("user")),
		From:        strings.TrimSpace(c.Query("from")),
		To:          strings.TrimSpace(c.Query("to")),
	}
}

// sessionTabFor 决定展示哪个标签：强制标签优先，其次按权限回落到用户能看的那个。
func sessionTabFor(want, forced string, canModels, canSessions bool) string {
	tab := sessionTabOf(want)
	if forced != "" {
		tab = forced
	}
	if tab == sessionTabModels && !canModels && canSessions {
		return sessionTabSessions
	}
	if tab == sessionTabSessions && !canSessions && canModels {
		return sessionTabModels
	}
	return tab
}

func (h *SessionPageHandle) sessionsPage(c *gin.Context, forcedTab string) {
	ctx := c.Request.Context()
	page, filter := sessionQueryOf(c)
	rows, total, err := h.svc.ListSessionsFiltered(ctx, filter, page, sessionPageSize)
	loadFailed := err != nil

	canModels := sessionCan(c, permProviderList)
	canSessions := sessionCan(c, permSessionList)
	tab := sessionTabFor(c.Query("tab"), forcedTab, canModels, canSessions)

	view := &aidto.SessionListPageResp{

		Page:     page,
		PageSize: sessionPageSize,
		Filter:   filter,
		Keyword:  filter.Keyword,
		Status:   filter.Status,
		Tab:      tab,

		IsModels: tab == sessionTabModels,

		CanViewModels:   canModels,
		CanViewSessions: canSessions,

		Presets:         toPresetOptions(aiservice.BuiltinPresets()),
		BuiltinKeys:     aiservice.BuiltinProviderKeys(),
		ProtocolOptions: aienums.ProtocolOptions,
	}
	if loadFailed {
		// 页面 GET 失败不是失败页：交统一 JSON 出口（aiPageFail 会按 StatusOf 给真实状态）。
		// 不用 pageview.Fail 是因为它的场景名取自模板路径，而 ai 的声明状态表在 enums 里，
		// 只有 aiErrFact 认得全；两条出口并存会让同一类错误一会儿 400 一会儿 404。
		aiPageFail(c, err)
		return
	}
	// 逐层交回错误由这里统一出口：子步骤写 JSON 后父流程继续渲染模板，
	// 响应就会变成「JSON 正文 + HTML 尾巴」，前端连 JSON.parse 都过不去。
	if err := h.attachSessionsBlocks(c, ctx, view, rows, total, page, filter); err != nil {
		aiPageFail(c, err)
		return
	}
	pageview.RenderTemplate(c, SessionsPageTemplate, view)
}

func (h *SessionPageHandle) attachSessionsBlocks(c *gin.Context, ctx context.Context,
	v *aidto.SessionListPageResp, rows []aidto.Session, total int64, page int, filter aidto.SessionQuery) error {

	v.Rows = h.sessionRows(ctx, rows)
	v.Total = total

	// 请求事实由 adminctx 提供，这里只取不派生。
	facts := pageview.PageFacts(c)
	v.CSRFToken, v.T, v.PermSet, v.Buttons = facts.CSRFToken, facts.T, facts.PermSet, facts.Buttons

	if err := h.attachSessionDetail(c, ctx, v); err != nil {
		return err
	}
	if err := h.attachSessionUsage(c, ctx, v, filter); err != nil {
		return err
	}
	if err := h.attachProviderCards(c, ctx, v); err != nil {
		return err
	}

	// 只交分页事实：按钮窗口与上一页/下一页链接由模板的共享分页能力算，
	// 后端不再拼 URL（拼法各模块一份必然漂移）。
	v.Pagination = pagination.Page{Total: total, Page: page, Limit: sessionPageSize}
	return nil
}

func toPresetOptions(in []aiservice.BuiltinPresetOption) []aidto.PresetOption {
	out := make([]aidto.PresetOption, 0, len(in))
	for _, p := range in {
		out = append(out, aidto.PresetOption{
			Key: p.Key, DisplayName: p.DisplayName,
			BaseURL: p.BaseURL, Protocol: p.Protocol, ModelCount: p.ModelCount,
		})
	}
	return out
}

func (h *SessionPageHandle) sessionRows(ctx context.Context, rows []aidto.Session) []aidto.SessionRowResp {
	out := make([]aidto.SessionRowResp, 0, len(rows))
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

	calls, cerr := h.svc.SessionCallsOf(ctx, ids)
	if cerr != nil {
		calls = map[int64]aidto.SessionCalls{}
	}
	for i := range rows {
		u := usage[rows[i].ID]
		if u == nil {
			u = []aidto.SessionModelUsage{}
		}
		out = append(out, aidto.SessionRowResp{Session: rows[i], Usage: u, Calls: calls[rows[i].ID]})
	}
	return out
}

func (h *SessionPageHandle) attachSessionUsage(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp, filter aidto.SessionQuery) error {
	// 页面上没有失败提示条：读不出来就整页失败，由调用方交统一 JSON 出口。
	usage, err := h.svc.SessionUsageOf(ctx, filter)
	if err != nil {
		return err
	}
	v.Usage = usage

	trend, terr := h.svc.SessionTrend(ctx, filter, sessionTrendDays)
	if terr != nil {
		return terr
	}
	v.Trend = trend

	opts, oerr := h.svc.SessionFilterOptions(ctx)
	if oerr != nil {
		// 筛选选项读不出来就不该装作「一个可选项都没有」：那会让用户以为真的没数据。
		return oerr
	}
	v.FilterOptions = opts
	return nil
}

func (h *SessionPageHandle) attachProviderCards(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp) error {
	v.Presets = toPresetOptions(aiservice.BuiltinPresets())
	v.BuiltinKeys = aiservice.BuiltinProviderKeys()
	v.ProtocolOptions = aienums.ProtocolOptions
	v.ProviderCards = []any{}
	return h.attachProviders(c, ctx, v, true)
}

func (h *SessionPageHandle) attachProviders(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp, withCards bool) error {
	v.Providers = []aidto.Provider{}
	if h.providers == nil {
		return nil
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		return err
	}
	v.Providers = list
	if !withCards {
		return nil
	}

	cards := make([]any, 0, len(list))
	for i := range list {
		cards = append(cards, pageview.ViewMap(cardData(aidto.ProviderCardBase{
			CSRFToken: v.CSRFToken, T: v.T, PermSet: v.PermSet, Buttons: v.Buttons,
		}, list[i], aiservice.BuiltinBaseURL(list[i].ProviderKey))))
	}
	v.ProviderCards = cards
	return nil
}

func sessionTabOf(raw string) string {

	if strings.EqualFold(strings.TrimSpace(raw), sessionTabSessions) {
		return sessionTabSessions
	}
	return sessionTabModels
}

func sessionCan(c *gin.Context, code string) bool {
	v, ok := c.Get(adminctx.PermSetKey)
	if !ok {
		return false
	}
	set, ok := v.(map[string]bool)
	if !ok {
		return false
	}
	return set[code]
}

func (h *SessionPageHandle) attachSessionDetail(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp) error {
	raw := strings.TrimSpace(c.Query("id"))
	if raw == "" {
		return nil
	}
	id := parseInt64(raw)
	if id <= 0 {
		return aienums.Err(aienums.ErrInvalidParam)
	}

	detail, err := h.svc.GetSession(ctx, id)
	if err != nil {
		return err
	}
	v.Detail = detail
	v.DetailID = id

	events, _, err := h.svc.ListEvents(ctx, id, 1, sessionDetailEventSize)
	if err != nil {
		return err
	}
	v.Events = sessionEventRows(events)

	// 页面曾经在这里顺手算一次折叠建议（FoldPlan），但它没有任何模板消费者，
	// 而 FoldPlan 内部要 ListEventsFrom 把整段事件读一遍 —— 每打开一次详情就白读一次。
	// 折叠建议是独立业务能力（POST /api/ai/session/fold/plan），要展示时由前端单独取。
	return nil
}

func sessionEventRows(events []aidto.SessionEventItem) []aidto.SessionEventRowResp {
	out := make([]aidto.SessionEventRowResp, 0, len(events))
	for i := range events {
		e := events[i]
		views := renderViewsOf(e.Meta)
		row := aidto.SessionEventRowResp{
			Seq:      e.Seq,
			Kind:     e.Kind,
			Content:  e.Content,
			Views:    toAnyRows(views),
			HasViews: len(views) > 0,
			At:       e.CreateTime,

			ViewsCtx: &aidto.FabViewsCtx{Views: toAnyRows(views)},
		}

		if e.Meta != nil {
			if v, ok := e.Meta["tool"].(string); ok {
				row.Tool = v
			}
			if v, ok := e.Meta["phase"].(string); ok {
				row.Phase = v
			}
		}
		out = append(out, row)
	}
	return out
}

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

func toAnyViews(in []uispec.View) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}

func toAnyRows[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}
