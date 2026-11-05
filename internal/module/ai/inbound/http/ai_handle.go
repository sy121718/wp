package aihttp

import (
	"context"
	"errors"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

func (h *Handle) Chat(c *gin.Context) {
	req, ok := binding.Form[aidto.ChatReq](c)
	if !ok {
		return
	}

	req.UserID = userID(c)
	req.SessionID = 0
	result, err := h.svc.Chat(c.Request.Context(), req)
	if err != nil {
		// 状态码由错误声明决定：这里原本手工映射 providerNotFound→404，其余一律 400，
		// 等于把 409 版本冲突、401 未登录也压成 400。
		aiErrExit(c, err)
		return
	}
	response.Success(c, result)
}

type fabStreamChunk struct {
	Kind string `json:"kind"`

	Text string `json:"text,omitempty"`

	Answer string `json:"answer,omitempty"`

	Note string `json:"note,omitempty"`
}

func (h *SessionPageHandle) FabAskStream(c *gin.Context) {
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {
		writeFabStreamError(c, response.MsgText(c, facing.Msg{Key: aienums.MsgFabNoModel}))
		return
	}

	form, ferr := binding.ParseForm[aidto.FabAskFormReq](c)
	if ferr != nil {
		writeFabStreamError(c, response.MsgText(c, facing.Msg{Key: aienums.MsgFabEmptyInput}))
		return
	}
	input := strings.TrimSpace(form.Input)
	if input == "" {
		writeFabStreamError(c, response.MsgText(c, facing.Msg{Key: aienums.MsgFabEmptyInput}))
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {
		input = string(runes[:fabInputLimit])
		truncated = true
	}

	images, imageLabels, imgErr := parseFabImages(form.Images, form.ImageLabels)
	if imgErr != nil {
		writeFabStreamError(c, response.MsgText(c, facing.Msg{Key: imgErr.Key}))
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	flush := func(chunk fabStreamChunk) {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		if _, werr := c.Writer.Write([]byte("data: " + string(payload) + "\n\n")); werr != nil {

			return
		}
		c.Writer.Flush()
	}

	res, err := h.svc.SendMessageStream(c.Request.Context(), aidto.SendMessageReq{
		SessionKey:  fabSessionKeyOf(int64(builtin.CurrentUserID(c))),
		ProviderKey: providerKey,
		Model:       model,
		Input:       composeFabInput(c, input),
		UserText:    input,
		Images:      images,
		ImageLabels: imageLabels,
		UserID:      userID(c),
	}, func(ev aiservice.StreamEvent) {
		flush(fabStreamChunk{Kind: ev.Kind, Text: ev.Text})
	})
	if err != nil {
		flush(fabStreamChunk{Kind: "error", Text: response.MsgText(c, facing.Msg{Key: aienums.MsgFabFailed})})
		return
	}

	answer := strings.TrimSpace(res.AssistantEvent.Content)
	note := fabDoneNote(c, res, truncated)
	flush(fabStreamChunk{Kind: "done", Answer: answer, Note: note})
}

func fabDoneNote(c *gin.Context, res *aidto.SendMessageResult, truncated bool) string {
	parts := make([]string, 0, 2)
	if truncated {
		parts = append(parts, response.MsgText(c, facing.Msg{Key: aienums.MsgFabTruncated}))
	}
	if n := len(res.ToolEvents); n > 0 {
		parts = append(parts, response.MsgText(c, facing.Msg{Key: aienums.MsgFabToolsPrefix})+strconv.Itoa(n)+response.MsgText(c, facing.Msg{Key: aienums.MsgFabToolsSuffix}))
	}
	return strings.Join(parts, " ")
}

func (h *SessionPageHandle) FabHistory(c *gin.Context) {
	uid := int64(builtin.CurrentUserID(c))
	turns, err := h.svc.RecentDialogue(c.Request.Context(), fabSessionKeyOf(uid), 0, uid)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	if turns == nil {
		turns = []aidto.DialogueTurn{}
	}
	response.Success(c, gin.H{"items": turns})
}

func writeFabStreamError(c *gin.Context, text string) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(fabStreamChunk{Kind: "error", Text: text})
	_, _ = c.Writer.Write([]byte("data: " + string(payload) + "\n\n"))
	c.Writer.Flush()
}

type Handle struct {
	svc aicontract.AIService
}

func NewHandle(svc aicontract.AIService) *Handle { return &Handle{svc: svc} }

func (h *Handle) ListProviders(c *gin.Context) {
	rows, err := h.svc.ListProviders(c.Request.Context())
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

func (h *Handle) GetProvider(c *gin.Context) {
	id := parseInt64(c.Query("id"))
	if id <= 0 {
		aiErrExit(c, aienums.Err(aienums.ErrInvalidParam))
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, provider)
}

func (h *Handle) SaveProvider(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.SaveProviderReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	req.UpdateBy = userID(c)
	provider, err := h.svc.SaveProvider(c.Request.Context(), &req)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	// 新建时卡片还不存在，页面刷新后由服务端渲染；保存现有卡片时只回这张卡。
	if req.ID == 0 {
		aiWriteResult(c, aienums.MsgSaved)
		return
	}
	c.HTML(http.StatusOK, aiProviderCardName, aiProviderCardData(c, *provider))
}

func (h *Handle) DeleteProvider(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.ProviderDeleteReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil || req.ID <= 0 {
		aiWriteBadInput(c)
		return
	}
	if err := h.svc.DeleteProvider(c.Request.Context(), req.ID); err != nil {
		aiWriteFail(c, err)
		return
	}
	aiWriteResult(c, aienums.MsgDeleted)
}

func (h *Handle) SetProviderStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.SetStatusReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	req.UpdateBy = userID(c)
	provider, err := h.svc.SetProviderStatus(c.Request.Context(), &req)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	c.HTML(http.StatusOK, aiProviderCardName, aiProviderCardData(c, *provider))
}

func (h *Handle) ListModels(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		aiErrExit(c, aienums.Err(aienums.ErrInvalidParam))
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, gin.H{"providerId": provider.ID, "list": provider.Models})
}

func (h *Handle) SaveModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.SaveModelsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		aiWriteBadInput(c)
		return
	}
	req.UpdateBy = userID(c)
	provider, err := h.svc.SaveModels(c.Request.Context(), &req)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	c.HTML(http.StatusOK, aiProviderCardName, aiProviderCardData(c, *provider))
}

func (h *Handle) RestoreDefaultModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.ProviderActionReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	req.UpdateBy = userID(c)
	provider, err := h.svc.RestoreDefaultModels(c.Request.Context(), &req)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	c.HTML(http.StatusOK, aiProviderCardName, aiProviderCardData(c, *provider))
}

func (h *Handle) FetchAvailableModels(c *gin.Context) {
	req, ok := binding.JSON[aidto.ProviderActionReq](c)
	if !ok {
		return
	}
	req.UpdateBy = userID(c)
	result, err := h.svc.FetchAvailableModels(c.Request.Context(), req)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsFetched, result)
}

// ModelCandidates 交候选清单：远端可用模型 + 目录里已有的 id。
//
// 与 fetch 的区别是**不落库**：这里只探一次远端、回事实，用户勾完再走 append。
// 服务端不再自己决定「弹窗怎么显示」—— 前端按 provider / candidates / existing 重绘。
func (h *Handle) ModelCandidates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.ProviderActionReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	req.UpdateBy = userID(c)
	result, err := h.svc.FetchModelCandidates(c.Request.Context(), &req)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	existing := make(map[string]bool, len(result.Existing))
	for _, id := range result.Existing {
		existing[id] = true
	}
	c.HTML(http.StatusOK, "admin/ai/provider_picker.html", aiFragmentData(c, gin.H{
		"Provider":   result.Provider,
		"Candidates": result.Candidates,
		"Existing":   existing,
	}))
}

// AppendModels 把勾选的模型追加进目录（全成或全败）。
//
// 去重 / 上限 / 乐观锁 / 「一个都没加」都归 service；这里只做绑定、调一次、出口。
func (h *Handle) AppendModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req aidto.AppendModelsReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	provider, _, err := h.svc.AppendModels(
		c.Request.Context(), req.ProviderID, req.Version, userID(c), req.ModelIDs)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	// 成功：模型目录换回服务端事实，并把候选弹窗宿主清空（弹窗随之关闭）。
	c.HTML(http.StatusOK, "admin/ai/ai_models_appended.html", aiFragmentData(c, gin.H{
		"Models": providerCardModels(c, *provider),
	}))
}

func bindID(c *gin.Context) (int64, error) {
	if id := parseInt64(c.Query("id")); id > 0 {
		return id, nil
	}
	if id := parseInt64(binding.Field(c, "id")); id > 0 {
		return id, nil
	}
	return 0, aienums.Err(aienums.ErrInvalidParam)
}

const (
	mcpProtocolVersion = "2024-11-05"

	mcpServerName = "go_wp"

	mcpServerVersion = "1.0"

	mcpMaxBodyBytes = 1 << 20
)

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

type AccountAuthorizer func(ctx context.Context, userID int64, perm permission.Perm) (bool, error)

const mcpEnabledTTL = 5 * time.Second

type McpEndpoint struct {
	tokens    *aiservice.AccessTokenService
	registry  *mcp.Registry
	recorder  *aiservice.ToolCallRecorder
	authorize AccountAuthorizer
	config    sysconfigcontract.ConfigReader

	enabledMu    sync.Mutex
	enabledValue bool
	enabledUntil time.Time
}

func NewMcpEndpoint(tokens *aiservice.AccessTokenService, registry *mcp.Registry, recorder *aiservice.ToolCallRecorder) *McpEndpoint {
	return &McpEndpoint{tokens: tokens, registry: registry, recorder: recorder, authorize: casbinAuthorizer}
}

func (e *McpEndpoint) SetAccountAuthorizer(a AccountAuthorizer) { e.authorize = a }

func (e *McpEndpoint) SetConfigReader(r sysconfigcontract.ConfigReader) { e.config = r }

func (e *McpEndpoint) enabled(ctx context.Context) bool {
	if e.config == nil {
		return false
	}
	e.enabledMu.Lock()
	defer e.enabledMu.Unlock()
	if time.Now().Before(e.enabledUntil) {
		return e.enabledValue
	}
	on := false
	if g, err := e.config.GetGroup(ctx, sysconfigcontract.GroupAI); err == nil && g != nil {
		if v, ok := g.Data[sysconfigcontract.KeyMCPEnabled].(bool); ok {
			on = v
		}
	}
	e.enabledValue, e.enabledUntil = on, time.Now().Add(mcpEnabledTTL)
	return on
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *McpEndpoint) Handle(c *gin.Context) {

	if !e.enabled(c.Request.Context()) {
		c.Status(http.StatusNotFound)
		return
	}

	identity, err := e.authenticate(c)
	if err != nil {

		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, gin.H{"error": response.MsgText(c, facing.Msg{Key: aienums.MsgMcpTokenInvalid, Fallback: "访问令牌无效"})})
		return
	}
	if e.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": response.MsgText(c, facing.Msg{Key: aienums.MsgMcpToolRunFailed, Fallback: "工具执行失败"})})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mcpMaxBodyBytes)
	req, err := binding.ParseJSON[rpcRequest](c)
	if err != nil {
		writeRPCError(c, nil, rpcParseError, "请求不是合法的 JSON-RPC 报文")
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		writeRPCError(c, req.ID, rpcInvalidRequest, "缺少 jsonrpc=2.0 或 method")
		return
	}

	notification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		writeRPCResult(c, req.ID, gin.H{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    gin.H{"tools": gin.H{"listChanged": false}},
			"serverInfo":      gin.H{"name": mcpServerName, "version": mcpServerVersion},
		})
	case "notifications/initialized", "notifications/cancelled":
		c.Status(http.StatusAccepted)
	case "ping":
		writeRPCResult(c, req.ID, gin.H{})
	case "tools/list":
		writeRPCResult(c, req.ID, gin.H{"tools": e.toolsFor(c.Request.Context(), identity)})
	case "tools/call":
		e.callTool(c, *req, identity)
	default:
		if notification {

			c.Status(http.StatusAccepted)
			return
		}
		writeRPCError(c, req.ID, rpcMethodNotFound, "不支持的方法："+req.Method)
	}
}

func (e *McpEndpoint) authenticate(c *gin.Context) (*aiservice.TokenIdentity, error) {
	if e.tokens == nil {
		return nil, aienums.Err(aienums.ErrTokenInvalid)
	}
	raw := strings.TrimSpace(c.GetHeader("Authorization"))

	const scheme = "bearer "
	if len(raw) < len(scheme) || !strings.EqualFold(raw[:len(scheme)], scheme) {
		return nil, aienums.Err(aienums.ErrTokenInvalid)
	}
	return e.tokens.Verify(c.Request.Context(), strings.TrimSpace(raw[len(scheme):]))
}

func (e *McpEndpoint) toolsFor(ctx context.Context, id *aiservice.TokenIdentity) []gin.H {
	out := make([]gin.H, 0, 8)
	for _, t := range e.registry.List() {
		if !e.allowed(ctx, id, t.Permission()) {
			continue
		}
		schema, err := t.SchemaJSON()
		if err != nil {

			logger.Scene("ai").With("tool", t.Name()).Error(err, "工具 schema 序列化失败，已跳过")
			continue
		}
		out = append(out, gin.H{
			"name":        t.Name(),
			"description": t.Description(),
			"inputSchema": json.RawMessage(schema),
		})
	}
	return out
}

func (e *McpEndpoint) allowed(ctx context.Context, id *aiservice.TokenIdentity, perm permission.Perm) bool {
	if id == nil || perm == permission.Exempt {
		return false
	}
	if !id.HasScope(string(perm)) {
		return false
	}
	if e.authorize == nil {
		return false
	}
	ok, err := e.authorize(ctx, id.UserID, perm)
	if err != nil {

		logger.Scene("ai").With("user", id.UserID).With("perm", string(perm)).Error(err, "MCP 工具权限判定失败")
		return false
	}
	return ok
}

func (e *McpEndpoint) callTool(c *gin.Context, req rpcRequest, id *aiservice.TokenIdentity) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(c, req.ID, rpcInvalidParams, "params 不是合法对象")
		return
	}
	tool, ok := e.registry.Lookup(strings.TrimSpace(params.Name))
	if !ok || !e.allowed(c.Request.Context(), id, tool.Permission()) {

		writeRPCError(c, req.ID, rpcInvalidParams, "工具不存在或当前凭证无权调用")
		return
	}

	start := time.Now()
	res, err := e.registry.Invoke(c.Request.Context(), tool.Name(), params.Arguments)
	e.record(c.Request.Context(), id, tool, params.Arguments, res.Text, err, time.Since(start))

	if err != nil {

		var argsErr *mcp.ArgsError
		text := response.MsgText(c, facing.Msg{Key: aienums.MsgMcpToolRunFailed, Fallback: "工具执行失败"})
		if errors.As(err, &argsErr) {
			text = argsErr.Msg
		}
		writeRPCResult(c, req.ID, gin.H{
			"content": []gin.H{{"type": "text", "text": text}},
			"isError": true,
		})
		return
	}
	writeRPCResult(c, req.ID, gin.H{
		"content": []gin.H{{"type": "text", "text": res.Text}},
		"isError": false,
	})
}

func (e *McpEndpoint) record(ctx context.Context, id *aiservice.TokenIdentity, tool mcp.Tool, args json.RawMessage, text string, err error, latency time.Duration) {
	if e.recorder == nil {
		return
	}
	status := aienums.ToolCallStatusOK
	if err != nil {
		var argsErr *mcp.ArgsError
		switch {
		case errors.As(err, &argsErr):
			status = aienums.ToolCallStatusArgsError
		default:
			status = aienums.ToolCallStatusFailed
		}
	}

	e.recorder.Record(ctx, &aimodel.AIToolCallLogEntity{
		SessionID:        0,
		UserID:           id.UserID,
		ToolName:         tool.Name(),
		ArgumentsSummary: aiservice.SummarizeForLog(string(args)),
		ResultSummary:    aiservice.SummarizeForLog(text),
		ResultLen:        int64(len([]rune(text))),
		Status:           string(status),
		ErrorKey:         aiservice.ToolErrorKeyOf(status),
		LatencyMs:        latency.Milliseconds(),
	})
}

func writeRPCResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": rawID(id), "result": result})
}

func writeRPCError(c *gin.Context, id json.RawMessage, code int, message string) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": rawID(id), "error": rpcError{Code: code, Message: message}})
}

func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return json.RawMessage(id)
}

type SessionHandle struct{ svc *aiservice.SessionService }

func NewSessionHandle(svc *aiservice.SessionService) *SessionHandle { return &SessionHandle{svc: svc} }

type sessionAppendBody struct {
	SessionKey  string         `json:"sessionKey"`
	SessionID   int64          `json:"sessionId"`
	Kind        string         `json:"kind"`
	Content     string         `json:"content"`
	Tokens      int64          `json:"tokens"`
	Meta        map[string]any `json:"meta"`
	Title       string         `json:"title"`
	ProviderKey string         `json:"providerKey"`
	ModelID     string         `json:"modelId"`
}

type sessionRenameBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`

	Status  *int16 `json:"status"`
	Version int64  `json:"version"`
}

type sessionFoldPlanBody struct {
	SessionID  int64 `json:"sessionId"`
	KeepRecent int   `json:"keepRecent"`
}

type sessionFoldBody struct {
	SessionID     int64  `json:"sessionId"`
	FromSeq       int64  `json:"fromSeq"`
	ToSeq         int64  `json:"toSeq"`
	Summary       string `json:"summary"`
	SummaryTokens int64  `json:"summaryTokens"`
}

func (h *SessionHandle) List(c *gin.Context) {
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "20"), 20)
	status := sessionIntOr(c.DefaultQuery("status", "-1"), -1)
	rows, total, err := h.svc.ListSessions(c.Request.Context(), c.Query("keyword"), status, page, size)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total, "page": page, "size": size})
}

func (h *SessionHandle) Get(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	detail, err := h.svc.GetSession(c.Request.Context(), id)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, detail)
}

func (h *SessionHandle) Events(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "50"), 50)
	rows, total, err := h.svc.ListEvents(c.Request.Context(), id, page, size)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total})
}

func (h *SessionHandle) Append(c *gin.Context) {
	body, ok := binding.JSON[sessionAppendBody](c)
	if !ok {
		return
	}
	res, err := h.svc.AppendEvent(c.Request.Context(), aidto.AppendEventReq{
		SessionKey:  body.SessionKey,
		SessionID:   body.SessionID,
		Kind:        body.Kind,
		Content:     body.Content,
		Tokens:      body.Tokens,
		Meta:        body.Meta,
		Title:       body.Title,
		ProviderKey: body.ProviderKey,
		ModelID:     body.ModelID,
		UserID:      userID(c),
	})
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionAppended, res)
}

// Send 往**已有会话**续写一条消息并等模型回复。
//
// 为什么不复用 /api/ai/chat：Chat 的语义是「开一段新对话」，它把 SessionID 清 0 再交给
// service。这里是「在这个会话里继续说」，sessionId 是必填入口参数 —— 两者混用会让一次
// 续写变成新会话，历史全丢。输入内容的校验、失败轮次标记、工具调用都在 service 里，
// 这里只绑定、调一次、出口。
func (h *SessionHandle) Send(c *gin.Context) {
	req, ok := binding.JSON[aidto.SendMessageReq](c)
	if !ok {
		return
	}
	req.UserID = userID(c)
	result, err := h.svc.SendMessage(c.Request.Context(), *req)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	// 走 SuccessMsg + facing.Msg：裸 key 交给 SuccessWithMessage 会被当作文案原样回显，
	// 只有命中词库时才是译文。既有收口是 facing.Msg 这一条。
	response.SuccessMsg(c, facing.Msg{Key: aienums.MsgSessionSent}, result)
}

func (h *SessionHandle) Rename(c *gin.Context) {
	body, ok := binding.JSON[sessionRenameBody](c)
	if !ok {
		return
	}
	status := int16(-1)
	if body.Status != nil {
		status = *body.Status
	}
	res, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:          body.ID,
		Title:       body.Title,
		ProviderKey: body.ProviderKey,
		ModelID:     body.ModelID,
		Status:      status,
		Version:     body.Version,
		UserID:      userID(c),
	})
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

func (h *SessionHandle) Archive(c *gin.Context) {
	body, ok := binding.JSON[sessionRenameBody](c)
	if !ok {
		return
	}
	archived := int16(aienums.SessionArchived)
	body.Status = &archived
	res, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      body.ID,
		Status:  archived,
		Version: body.Version,
		UserID:  userID(c),
	})
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

func (h *SessionHandle) FoldPlan(c *gin.Context) {
	body, ok := binding.JSON[sessionFoldPlanBody](c)
	if !ok {
		return
	}
	plan, err := h.svc.FoldPlan(c.Request.Context(), aidto.FoldPlanReq{
		SessionID:  body.SessionID,
		KeepRecent: body.KeepRecent,
	})
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, plan)
}

func (h *SessionHandle) Fold(c *gin.Context) {
	body, ok := binding.JSON[sessionFoldBody](c)
	if !ok {
		return
	}
	res, err := h.svc.Fold(c.Request.Context(), aidto.FoldReq{
		SessionID:     body.SessionID,
		FromSeq:       body.FromSeq,
		ToSeq:         body.ToSeq,
		Summary:       body.Summary,
		SummaryTokens: body.SummaryTokens,
		UserID:        userID(c),
	})
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionFolded, res)
}

func sessionIntOr(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

type TokenHandle struct {
	svc    *aiservice.AccessTokenService
	config sysconfigcontract.Service
}

func NewTokenHandle(svc *aiservice.AccessTokenService) *TokenHandle { return &TokenHandle{svc: svc} }

// SetConfigService 注入配置端口：对外接入点开关就存在 AI 配置组里。
func (h *TokenHandle) SetConfigService(cfg sysconfigcontract.Service) { h.config = cfg }

type tokenCreateBody struct {
	Name      string   `json:"name" form:"name"`
	Scopes    []string `json:"scopes" form:"scopes"`
	ExpiresAt string   `json:"expiresAt" form:"expiresAt"`
}

type tokenRevokeBody struct {
	ID int64 `json:"id" form:"id"`
}

type mcpToggleBody struct {
	Enabled string `json:"enabled" form:"enabled"`
}

func (h *TokenHandle) List(c *gin.Context) {
	if h.svc == nil {
		aiErrExit(c, aienums.Err(aienums.ErrInternal))
		return
	}

	req := &aidto.TokenListReq{
		All:    c.Query("all") == "1" || c.Query("all") == "true",
		Limit:  int(parseInt64(c.Query("limit"))),
		UserID: userID(c),
	}
	rows, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		aiErrExit(c, err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

func (h *TokenHandle) Create(c *gin.Context) {
	if h.svc == nil {
		aiErrExit(c, aienums.Err(aienums.ErrInternal))
		return
	}
	c.Header("Cache-Control", "no-store")
	var body tokenCreateBody
	if err := c.ShouldBindWith(&body, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &aidto.TokenCreateReq{
		Name:      body.Name,
		Scopes:    body.Scopes,
		ExpiresAt: body.ExpiresAt,

		UserID: userID(c),
	})
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	// 一次性明文只在本响应里出现一次：模板把它写进结果区，之后任何接口都不会再回它。
	rows, err := h.svc.List(c.Request.Context(), &aidto.TokenListReq{All: true, UserID: userID(c)})
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	c.HTML(http.StatusOK, "admin/ai/mcp_token_created.html", aiFragmentData(c, gin.H{
		"Token":  res.Token,
		"Tokens": toAnyRows(tokenRows(rows)),
	}))
}

func (h *TokenHandle) Revoke(c *gin.Context) {
	if h.svc == nil {
		aiErrExit(c, aienums.Err(aienums.ErrInternal))
		return
	}
	c.Header("Cache-Control", "no-store")
	var body tokenRevokeBody
	if err := c.ShouldBindWith(&body, ginbinding.FormPost); err != nil || body.ID <= 0 {
		// id 是必填入口参数：缺了是参数错误（400），不能落进「找不到这个 id」那条路。
		// 传一个不存在的 id 仍然走 404 —— 两种情况的语义不同，前端要能分辨。
		aiWriteBadInput(c)
		return
	}
	item, err := h.svc.RevokeWithFact(c.Request.Context(), body.ID)
	if err != nil {
		aiWriteFail(c, err)
		return
	}
	// 撤销不删行：审计要能回答「什么时候撤的」。回同一份行模板把状态换成已撤销事实。
	c.HTML(http.StatusOK, "admin/ai/mcp_token_row.html", aiFragmentData(c, gin.H{
		"Token": tokenRows([]aidto.TokenItem{item})[0],
	}))
}

// SetMcpEnabled 开关对外接入点。
//
// 读-改-写整组配置是业务写入，放 service（AccessTokenService.SetMcpEnabled）；
// 这里只绑定、调一次、出口。开关的成败用两个既有的词条交代，不拼句子。
func (h *TokenHandle) SetMcpEnabled(c *gin.Context) {
	if h.svc == nil || h.config == nil {
		aiErrExit(c, aienums.Err(aienums.ErrInternal))
		return
	}
	c.Header("Cache-Control", "no-store")
	var body mcpToggleBody
	if err := c.ShouldBindWith(&body, ginbinding.FormPost); err != nil {
		aiWriteBadInput(c)
		return
	}
	enabled := strings.TrimSpace(body.Enabled) == "1"
	if err := h.svc.SetMcpEnabled(c.Request.Context(), h.config, enabled, userID(c)); err != nil {
		aiErrExit(c, err)
		return
	}
	key := aienums.McpSwitchOffDone
	if enabled {
		key = aienums.McpSwitchOnDone
	}
	c.HTML(http.StatusOK, "admin/ai/mcp_switch_result.html", aiFragmentData(c, gin.H{
		"Key":     key,
		"Enabled": enabled,
	}))
}

const (
	fabImageMaxCount = 4

	fabImageMaxBytes = 4 << 20

	fabImageDataPrefix = "data:image/"
)

type fabImageError struct {
	Key string
}

func parseFabImages(rawImages, rawLabels string) ([]string, []string, *fabImageError) {
	rawImages = strings.TrimSpace(rawImages)
	if rawImages == "" {
		return nil, nil, nil
	}
	var images []string
	if err := json.Unmarshal([]byte(rawImages), &images); err != nil {
		return nil, nil, &fabImageError{Key: aienums.MsgFabImageBadFormat}
	}

	images = nonEmptyStrings(images)
	if len(images) == 0 {
		return nil, nil, nil
	}
	if len(images) > fabImageMaxCount {
		return nil, nil, &fabImageError{Key: aienums.MsgFabImageTooMany}
	}
	for _, img := range images {
		if !strings.HasPrefix(img, fabImageDataPrefix) {
			return nil, nil, &fabImageError{Key: aienums.MsgFabImageBadFormat}
		}
		if len(img) > fabImageMaxBytes {
			return nil, nil, &fabImageError{Key: aienums.MsgFabImageTooLarge}
		}
	}

	var labels []string
	if strings.TrimSpace(rawLabels) != "" {

		_ = json.Unmarshal([]byte(rawLabels), &labels)
		labels = nonEmptyStrings(labels)
	}

	labels = alignLabels(labels, len(images))
	return images, labels, nil
}

func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func alignLabels(labels []string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		if i < len(labels) {
			out[i] = labels[i]
			continue
		}
		out[i] = fabImageFallbackLabel
	}
	return out
}

const fabImageFallbackLabel = "图片"

func fabImageRequest(req *aidto.SendMessageReq, images, labels []string) {
	req.Images = images
	req.ImageLabels = labels
}

func casbinAuthorizer(_ context.Context, userID int64, perm permission.Perm) (bool, error) {
	routes := permission.RoutesOf(perm)
	if len(routes) == 0 {

		logger.Scene("ai").With("perm", string(perm)).Warn("权限点没有声明路由，工具调用按无权限处理")
		return false, nil
	}
	for _, r := range routes {
		ok, err := builtin.EnforceForUser(userID, r.Path, r.Method)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

type toolProvider struct {
	runner *mcp.Runner
}

func (p *toolProvider) Specs() []aidto.ToolSpec {
	tools := p.runner.Tools()
	if len(tools) == 0 {
		return nil
	}
	out := make([]aidto.ToolSpec, 0, len(tools))
	for _, t := range tools {
		raw, err := t.SchemaJSON()
		if err != nil {

			logger.Scene("ai").With("tool", t.Name()).Error(err, "工具 schema 序列化失败，本轮不暴露该工具")
			continue
		}
		out = append(out, aidto.ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: raw})
	}
	return out
}

func (p *toolProvider) Run(ctx context.Context, userID int64, name, arguments string) (aiservice.ToolRunResult, error) {
	res, err := p.runner.Run(ctx, userID, name, arguments)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {

			return aiservice.ToolRunResult{}, ctxErr
		}
		text, status := failureText(name, err)
		return aiservice.ToolRunResult{Text: text, Status: status}, nil
	}

	return aiservice.ToolRunResult{Text: res.Text, Status: aienums.ToolCallStatusOK, Data: res.Data}, nil
}

func failureText(tool string, err error) (string, aienums.ToolCallStatus) {
	var argsErr *mcp.ArgsError
	if errors.As(err, &argsErr) {
		return argsErr.Msg, aienums.ToolCallStatusArgsError
	}
	var permErr *mcp.PermissionError
	if errors.As(err, &permErr) {
		return aienums.ErrToolForbidden, aienums.ToolCallStatusForbidden
	}

	logger.Scene("ai").With("tool", tool).Error(err, "工具执行失败")
	return aienums.ErrToolRunFailed, aienums.ToolCallStatusFailed
}
