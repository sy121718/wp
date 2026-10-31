package aihttp

import (
	"cmp"
	"context"
	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
	"go_wp/pkg/reqparams"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const fabSessionKey = "admin-fab"

func fabSendReq(c *gin.Context, providerKey, model, input string) *aidto.SendMessageReq {
	uid := userID(c)
	return &aidto.SendMessageReq{
		SessionKey:  fabSessionKeyOf(uid),
		ProviderKey: providerKey,
		Model:       model,
		Input:       composeFabInput(c, input),
		UserText:    input,
		UserID:      uid,
	}
}

func fabSessionKeyOf(uid int64) string {
	return fabSessionKey + ":" + strconv.FormatInt(uid, 10)
}

const fabInputLimit = 4000

const fabContextQueryLimit = 300

func (h *SessionPageHandle) FabAsk(c *gin.Context) {
	input := strings.TrimSpace(c.PostForm("input"))
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {

		h.renderFabError(c, fabMsg(aienums.MsgFabNoModel), c.PostForm("ctxPath"))
		return
	}
	if input == "" {
		h.renderFabError(c, fabMsg(aienums.MsgFabEmptyInput), c.PostForm("ctxPath"))
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {

		input = string(runes[:fabInputLimit])
		truncated = true
	}

	res, err := h.svc.SendMessage(c.Request.Context(), *fabSendReq(c, providerKey, model, input))
	if err != nil {
		h.renderFabError(c, fabMsg(aienums.MsgFabFailed), c.PostForm("ctxPath"))
		return
	}
	answer := strings.TrimSpace(res.AssistantEvent.Content)
	views := fabViewsOf(res)
	view := &aidto.FabResultView{
		FabAnswer:    answer,
		FabSessionID: res.Session.ID,
		FabAskKey:    c.PostForm("ctxPath"),
	}
	if len(views) > 0 {

		view.FabViewsCtx = &aidto.FabViewsCtx{Views: toAnyViews(views)}
	}

	if r := fabReasoningOf(res); r != "" {
		view.FabReasoning = r
		view.FabThinkLabel = fabTextOrBlank(fabMsg(aienums.MsgFabThinkLabel))
	}
	if truncated {
		view.FabTruncatedText = fabTextOrBlank(fabMsg(aienums.MsgFabTruncated))
	}
	if n := len(res.ToolEvents); n > 0 {

		view.FabToolsPrefix = fabTextOrBlank(fabMsg(aienums.MsgFabToolsPrefix))
		view.FabToolsSuffix = fabTextOrBlank(fabMsg(aienums.MsgFabToolsSuffix))
		view.FabToolsCount = n
	}

	if answer == "" && len(res.ToolEvents) == 0 {
		view.FabEmptyText = fabTextOrBlank(fabMsg(aienums.MsgFabEmpty))
	}
	h.renderFab(c, view)
}

func (h *SessionPageHandle) renderFab(c *gin.Context, view *aidto.FabResultView) {
	pageview.Render(c, view)
}

func (h *SessionPageHandle) renderFabError(c *gin.Context, msg adminui.Msg, askKey string) {
	pageview.Render(c, &aidto.FabErrorView{
		FabError:  aidto.FabMsgFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args},
		FabAskKey: askKey,
	})
}

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

func fabText(key string) string {
	if v, ok := aienums.FacingText(key); ok {
		return v
	}
	return key
}

func fabMsg(key string) adminui.Msg {
	return adminui.Msg{Key: key, Fallback: fabText(key)}
}

const (
	mcpPagePath = "/admin/ai/mcp"
)

type McpPageHandle struct {
	tokens   *aiservice.AccessTokenService
	registry *mcp.Registry
	config   sysconfigcontract.Service
}

func NewMcpPageHandle(tokens *aiservice.AccessTokenService, registry *mcp.Registry) *McpPageHandle {
	return &McpPageHandle{tokens: tokens, registry: registry}
}

func (h *McpPageHandle) SetConfigService(s sysconfigcontract.Service) { h.config = s }

func (h *McpPageHandle) Page(c *gin.Context) {
	data := h.pageData(c, "", "", "")
	pageview.Render(c, data)
}

func (h *McpPageHandle) TokenCreate(c *gin.Context) {
	req, ok := binding.Form[aidto.TokenCreateReq](c)
	if !ok {
		return
	}
	req.UserID = userID(c)
	res, err := h.tokens.Create(c.Request.Context(), req)
	if err != nil {

		h.renderTokens(c, "", aiMsgText(c, err.Error(), err.Error()), "")
		return
	}
	h.renderTokens(c, res.Token, "", "")
}

func (h *McpPageHandle) TokenRevoke(c *gin.Context) {
	form, err := binding.ParseForm[aidto.TokenRevokeReq](c)
	if err != nil || form.ID <= 0 {
		h.renderTokens(c, "", aiMsgText(c, aienums.ErrInvalidParam, "参数不正确"), "")
		return
	}
	if err := h.tokens.Revoke(c.Request.Context(), form.ID); err != nil {
		h.renderTokens(c, "", aiMsgText(c, err.Error(), err.Error()), "")
		return
	}
	h.renderTokens(c, "", "", aiMsgText(c, aienums.McpTokenRevokeDone, "已撤销该令牌（立即失效）"))
}

func (h *McpPageHandle) renderTokens(c *gin.Context, newToken, errText, doneText string) {
	pageview.Render(c, h.pageData(c, newToken, errText, doneText))
}

func (h *McpPageHandle) pageData(c *gin.Context, newToken, errText, doneText string) gin.H {
	ctx := c.Request.Context()

	tokens, err := h.tokens.List(ctx, &aidto.TokenListReq{All: true, UserID: userID(c)})
	if err != nil {

		tokens = nil
		if errText == "" {
			errText = aiMsgText(c, aienums.ErrInternal, "暂时无法读取令牌列表")
		}
	}

	title := aiMsgText(c, aienums.McpTitle, "MCP 与外部访问")

	enabled := h.mcpEnabled(ctx)
	return adminui.PrepareView(c, &aidto.McpPageResp{

		Title:        title,
		Lead:         aiMsgText(c, aienums.McpLead, ""),
		Endpoint:     mcpEndpointURL(c),
		Tokens:       toAnyRows(tokenRows(tokens)),
		Tools:        toAnyRows(h.tools()),
		ScopeOptions: toAnyRows(h.scopeOptions()),
		NewToken:     newToken,
		ErrText:      errText,
		DoneText:     doneText,
		McpEnabled:   enabled,
		McpTone:      mcpSwitchTone(enabled),
	})
}

func mcpSwitchTone(enabled bool) string {
	if enabled {
		return "ok"
	}
	return "mute"
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

func (h *McpPageHandle) McpToggle(c *gin.Context) {
	if h.config == nil {
		mcpJump(c, false, adminui.Msg{Key: aienums.ErrInternal, Fallback: "暂时无法读取令牌列表"})
		return
	}
	on := c.PostForm("enabled") == "1"
	ctx := c.Request.Context()
	g, err := h.config.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		mcpJump(c, false, adminui.Msg{Key: aienums.ErrInternal, Fallback: "暂时无法读取令牌列表"})
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

		logger.Scene("ai").With("group", sysconfigcontract.GroupAI).Error(err, "保存对外接入点开关失败")
		mcpJump(c, false, adminui.Msg{Key: aienums.ErrInternal, Fallback: "保存失败，请稍后重试"})
		return
	}
	if on {
		mcpJump(c, true, adminui.Msg{Key: aienums.McpSwitchOnDone, Fallback: "已开启对外接入点（最迟 5 秒后生效）"})
		return
	}
	mcpJump(c, true, adminui.Msg{Key: aienums.McpSwitchOffDone, Fallback: "已关闭对外接入点（最晚 5 秒后对任何请求回 404）"})
}

func mcpJump(c *gin.Context, ok bool, msg adminui.Msg) {
	back := mcpPagePath
	backText := adminui.Msg{Key: aienums.McpTitle, Fallback: "MCP 与外部访问"}
	jump := shell.Jump{
		OK:       ok,
		MsgKey:   msg.Key,
		Msg:      msg.Fallback,
		MsgArgs:  msg.Args,
		Back:     back,
		BackText: backText.Fallback,
	}
	if backText.Key != "" {
		jump.BackTextKey = backText.Key
	}
	if ok {
		jump.Seconds = 1
	}
	shell.RenderJump(c, jump)
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

func tokenTimeText(t *utils.JSONTime) string {
	if t == nil {
		return ""
	}
	return time.Time(*t).Format("2006-01-02 15:04")
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
	slices.SortFunc(out, func(a, b scopeOption) int { return cmp.Compare(a.Code, b.Code) })
	return out
}

const (
	pagePath = "/admin/ai/sessions"
)

type PageHandle struct {
	svc aicontract.AIService
}

func NewPageHandle(svc aicontract.AIService) *PageHandle { return &PageHandle{svc: svc} }

func (h *PageHandle) ProvidersPage(c *gin.Context) {
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels)
}

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

func (h *PageHandle) ProviderDelete(c *gin.Context) {
	id := parseInt64(c.PostForm("providerId"))
	if err := h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	h.pageNotice(c, "done", aienums.MsgDeleted, nil)
}

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
	picker := &aidto.PickerResp{
		Provider:   result.Provider,
		Candidates: result.Candidates,
		Existing:   idSet(result.Existing),
	}

	page := adminui.PrepareView(c, &aidto.PickerPageResp{
		Title:  aiMsgText(c, aienums.AdminLLMTitle, "大模型管理"),
		Picker: picker,
	})
	if hxFragment(c, page) {
		return
	}
	c.Redirect(http.StatusFound, pagePath+"?tab="+sessionTabModels+"&picker="+strconv.FormatInt(providerID, 10))
}

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

const maxModelAppend = 2000

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

func modelIDs(models []aidto.ModelEntry) []string {
	out := make([]string, 0, len(models))
	for i := range models {
		out = append(out, models[i].ID)
	}
	return out
}

func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func (h *PageHandle) ModelsRowAdd(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := append(parseModelRows(c), aidto.ModelEntry{InputTypes: []string{aienums.InputTypeText}})
	h.renderRows(c, providerID, rows, "", nil, false)
}

func (h *PageHandle) ModelsRowDelete(c *gin.Context) {
	providerID := parseInt64(c.PostForm("providerId"))
	rows := parseModelRows(c)
	index := int(parseInt64(c.PostForm("rowIndex")))
	if index >= 0 && index < len(rows) {
		rows = append(rows[:index], rows[index+1:]...)
	}
	h.renderRows(c, providerID, rows, "", nil, false)
}

func (h *PageHandle) renderRows(c *gin.Context, providerID int64, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) {
	provider, err := h.svc.GetProvider(c.Request.Context(), providerID)
	if err != nil || provider == nil {
		h.pageNotice(c, "err", aiErrKey(err), nil)
		return
	}
	if hxFragment(c, h.modelsData(c, *provider, rows, noticeKey, params, isErr)) {
		return
	}
	h.pageNotice(c, redirectSlot(isErr), noticeKey, params)
}

func (h *PageHandle) modelsSuccess(c *gin.Context, provider aidto.Provider, noticeKey string, params map[string]string) {
	if hxFragment(c, h.modelsData(c, provider, provider.Models, noticeKey, params, false)) {
		return
	}
	h.pageNotice(c, "done", noticeKey, params)
}

func (h *PageHandle) modelsFailure(c *gin.Context, providerID int64, rows []aidto.ModelEntry, err error) {
	key := aiErrKey(err)
	provider, gerr := h.svc.GetProvider(c.Request.Context(), providerID)
	if gerr != nil || provider == nil {
		h.pageNotice(c, "err", key, nil)
		return
	}
	if hxFragment(c, h.modelsData(c, *provider, rows, key, nil, true)) {
		return
	}
	h.pageNotice(c, "err", key, nil)
}

func (h *PageHandle) modelsData(c *gin.Context, provider aidto.Provider, rows []aidto.ModelEntry, noticeKey string, params map[string]string, isErr bool) gin.H {
	provider.Models = rows
	return adminui.PrepareView(c, &aidto.ModelsDataResp{
		Provider: provider,
		Rows:     modelRows(rows),

		ModelsNotice:    facingTextParams(c, noticeKey, params),
		ModelsNoticeErr: isErr,

		ModelsNoticeTone: noticeTone(isErr),
		ModelsNoticeRole: noticeRole(isErr),
	})
}

func (h *PageHandle) pageNotice(c *gin.Context, slot, key string, params map[string]string) {
	if strings.TrimSpace(key) == "" {
		redirectWhere(c, pagePath)
		return
	}
	aiSessionsJump(c, slot != "err", aiConclusionFact(c, key, params), pagePath)
}

func aiConclusionFact(c *gin.Context, key string, params map[string]string) adminui.Msg {
	if len(params) == 0 {
		return adminui.Msg{Key: key, Fallback: aiErrFallback(key)}
	}
	return adminui.Msg{Fallback: facingTextParams(c, key, params)}
}

func aiSessionsJump(c *gin.Context, ok bool, msg adminui.Msg, back string) {
	if strings.TrimSpace(back) == "" {
		back = pagePath
	}
	backText := adminui.Msg{Key: aienums.AdminLLMTitle, Fallback: "大模型管理"}
	jump := shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
	}
	if ok {
		jump.Seconds = 1
	}
	shell.RenderJump(c, jump)
}

func redirectSlot(isErr bool) string {
	if isErr {
		return "err"
	}
	return "done"
}

func facingTextParams(c *gin.Context, key string, params map[string]string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	text, ok := aienums.FacingText(key)
	if !ok {
		return key
	}
	return aienums.FormatFacing(aiMsgText(c, key, text), params)
}

func facingText(c *gin.Context, key string) string { return facingTextParams(c, key, nil) }

func builtinProviderKeys() []string { return aiservice.BuiltinProviderKeys() }

func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}

func redirectWhere(c *gin.Context, target string) {
	if isHXRequest(c) {
		pageview.HXRedirect(c, target)
		return
	}
	pageview.AfterWrite(c, target)
}

func hxFragment(c *gin.Context, view any) bool {
	if !isHXRequest(c) {
		return false
	}
	pageview.Render(c, view)
	return true
}

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

func modelRows(models []aidto.ModelEntry) []aidto.ModelRowResp {
	rows := make([]aidto.ModelRowResp, 0, len(models))
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
		rows = append(rows, aidto.ModelRowResp{
			Index:           i,
			ID:              models[i].ID,
			DisplayName:     models[i].DisplayName,
			ContextWindow:   models[i].ContextWindow,
			MaxOutputTokens: models[i].MaxOutputTokens,
			TextChecked:     hasText,
			ImageChecked:    hasImage,
		})
	}
	return rows
}

func cardData(base aidto.ProviderCardBase, provider aidto.Provider, notice string, isErr bool, defaultBaseURL string) aidto.ProviderCardResp {
	return aidto.ProviderCardResp{
		Provider:        provider,
		Rows:            modelRows(provider.Models),
		ProtocolOptions: aienums.ProtocolOptions,
		DefaultBaseURL:  defaultBaseURL,
		BuiltinKeys:     builtinProviderKeys(),
		ModelsNotice:    notice,
		ModelsNoticeErr: isErr,

		StatusTone:       providerStatusTone(provider.Status),
		ModelsNoticeTone: noticeTone(isErr),
		ModelsNoticeRole: noticeRole(isErr),
		CSRFToken:        base.CSRFToken,
		T:                base.T,
		PermSet:          base.PermSet,
	}
}

func providerStatusTone(status int) string {
	if status == 1 {
		return "ok"
	}
	return "warn"
}

func noticeTone(isErr bool) string {
	if isErr {
		return "warn"
	}
	return "ok"
}

func noticeRole(isErr bool) string {
	if isErr {
		return "alert"
	}
	return "status"
}

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

func pick(items []string, i int) string {
	if i < 0 || i >= len(items) {
		return ""
	}
	return items[i]
}

const (
	sessionPath     = "/admin/ai/sessions"
	sessionPageSize = 20
	sessionEventTop = 50

	sessionTrendDays = 30

	sessionTabSessions = "sessions"
	sessionTabModels   = "models"

	permProviderList = "ai:provider_list"
	permSessionList  = "ai:session_list"
)

type providerLister interface {
	ListProviders(ctx context.Context) ([]aidto.Provider, error)
}

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

type SessionPageHandle struct {
	svc       sessionServicePort
	providers providerLister
}

func NewSessionPageHandle(svc sessionServicePort, providers providerLister) *SessionPageHandle {
	return &SessionPageHandle{svc: svc, providers: providers}
}

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
	loadFailed := err != nil

	tab := sessionTabOf(c.Query("tab"))

	canModels := sessionCan(c, permProviderList)
	canSessions := sessionCan(c, permSessionList)
	if tab == sessionTabModels && !canModels && canSessions {
		tab = sessionTabSessions
	} else if tab == sessionTabSessions && !canSessions && canModels {
		tab = sessionTabModels
	}

	view := &aidto.SessionListPageResp{

		Title:    aiMsgText(c, aienums.AdminLLMTitle, "大模型管理"),
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

		pageview.Fail(c, err)
		return
	}
	h.attachSessionsBlocks(c, ctx, view, rows, total, page, filter)
	pageview.Render(c, view)
}

func (h *SessionPageHandle) attachSessionsBlocks(c *gin.Context, ctx context.Context,
	v *aidto.SessionListPageResp, rows []aidto.Session, total int64, page int, filter aidto.SessionQuery) {

	v.Rows = h.sessionRows(ctx, rows)
	v.Total = total

	base := adminui.PrepareView(c, &aidto.SessionListPageResp{})
	v.CSRFToken, _ = base["csrf_token"].(string)
	v.T = base["t"]
	v.PermSet = base["PermSet"]
	v.Buttons = base["Buttons"]

	h.attachSessionDetail(c, ctx, v)
	h.attachSessionUsage(c, ctx, v, filter)
	h.attachProviderCards(c, ctx, v)

	totalPages := int((total + int64(sessionPageSize) - 1) / int64(sessionPageSize))
	v.TotalPages = totalPages
	v.HasPrev = page > 1
	v.HasNext = page < totalPages
	if page > 1 {
		v.PrevURL = sessionPageURL(c, page-1)
	}
	if page < totalPages {
		v.NextURL = sessionPageURL(c, page+1)
	}
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

func (h *SessionPageHandle) attachSessionUsage(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp, filter aidto.SessionQuery) {
	usage, err := h.svc.SessionUsageOf(ctx, filter)
	if err != nil {
		v.UsageErr = aiErrText(c, err)

		usage = aidto.SessionUsage{}
	}
	v.Usage = usage

	trend, terr := h.svc.SessionTrend(ctx, filter, sessionTrendDays)
	if terr != nil {
		if v.UsageErr == "" {
			v.UsageErr = aiErrText(c, terr)
		}
		trend = aidto.SessionTrend{}
	}
	v.Trend = trend

	opts, oerr := h.svc.SessionFilterOptions(ctx)
	if oerr != nil {
		v.OptionsErr = aiErrText(c, oerr)

		opts = aidto.SessionFilterOptions{Providers: []string{}, Models: []string{}, Creators: []int64{}}
	}
	v.FilterOptions = opts
}

func (h *SessionPageHandle) attachProviderCards(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp) {
	v.Presets = toPresetOptions(aiservice.BuiltinPresets())
	v.BuiltinKeys = aiservice.BuiltinProviderKeys()
	v.ProtocolOptions = aienums.ProtocolOptions
	v.ProviderCards = []any{}
	h.attachProviders(c, ctx, v, true)
}

func (h *SessionPageHandle) attachProviders(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp, withCards bool) {
	v.Providers = []aidto.Provider{}
	if h.providers == nil {
		return
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		v.ProviderErr = aiErrText(c, err)
		return
	}
	v.Providers = list
	if !withCards {
		return
	}

	cards := make([]any, 0, len(list))
	for i := range list {
		cards = append(cards, adminui.ViewMap(cardData(aidto.ProviderCardBase{
			CSRFToken: v.CSRFToken, T: v.T, PermSet: v.PermSet, Buttons: v.Buttons,
		}, list[i], "", false, aiservice.BuiltinBaseURL(list[i].ProviderKey))))
	}
	v.ProviderCards = cards
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

func (h *SessionPageHandle) sessionNotice(c *gin.Context, ok bool, key string) {

	aiSessionsJump(c, ok, adminui.Msg{Key: key, Fallback: aiErrFallback(key)}, sessionJumpBack(c.PostForm("back")))
}

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

func sessionBackURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == sessionPath || strings.HasPrefix(raw, sessionPath+"?") {
		return raw
	}
	return sessionPath
}

const sessionDetailEventSize = sessionEventTop

func (h *SessionPageHandle) attachSessionDetail(c *gin.Context, ctx context.Context, v *aidto.SessionListPageResp) {
	raw := strings.TrimSpace(c.Query("id"))
	if raw == "" {
		return
	}
	id := parseInt64(raw)
	if id <= 0 {
		v.DetailErr = internalFallback(c)
		return
	}

	detail, err := h.svc.GetSession(ctx, id)
	if err != nil {

		v.DetailErr = aiErrText(c, err)
		return
	}
	v.Detail = detail
	v.DetailID = id

	events, _, err := h.svc.ListEvents(ctx, id, 1, sessionDetailEventSize)
	if err != nil {
		v.EventsErr = aiErrText(c, err)
	} else {
		v.Events = sessionEventRows(events)
	}

	plan, err := h.svc.FoldPlan(ctx, aidto.FoldPlanReq{SessionID: id})
	if err != nil {
		v.FoldPlanErr = aiErrText(c, err)
	} else {
		v.FoldPlan = plan
	}
}

func sessionEventRows(events []aidto.SessionEventItem) []aidto.SessionEventRowResp {
	out := make([]aidto.SessionEventRowResp, 0, len(events))
	for i := range events {
		e := events[i]
		views := renderViewsOf(e.Meta)
		row := aidto.SessionEventRowResp{
			Seq:       e.Seq,
			Kind:      e.Kind,
			Tone:      sessionEventTone(e.Kind),
			Content:   e.Content,
			Views:     toAnyRows(views),
			HasViews:  len(views) > 0,
			TimeLabel: sessionTimeLabel(e.CreateTime),

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

func sessionTimeLabel(t utils.JSONTime) string {
	if t.IsZero() {
		return ""
	}
	return t.Time().Format("01-02 15:04")
}

func fabTextOrBlank(msg adminui.Msg) string {
	return msg.Fallback
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
