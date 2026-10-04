// ai_mcp_endpoint.go — POST /mcp：外部 harness / AI 接入点（MCP over JSON-RPC 2.0）。
//
// 为什么落在这里而不是 internal/mcp：`internal/mcp` 是**工具基座**（schema / 校验 / 注册表 / runner），
// 它刻意不认识 HTTP、也不认识 gin；本文件是它的**出站壳**，负责协议、身份与响应格式。
// 工具的实现仍然只在各模块的 `inbound/mcp` 里。
//
// 传输形态：**无状态 streamable HTTP 的简化版** —— 一次 POST 一个 JSON-RPC 请求，一次响应了结，
// 不用 SSE、不发 `Mcp-Session-Id`。取舍写在这里免得被当成缺失：
//
//	· 工具调用是「查一次数、答一次」，没有服务端主动推送的需求（`listChanged` 恒为 false）；
//	· 无会话状态意味着任何实例都能处理任何请求（将来多副本部署不需要粘性会话）；
//	· 代价是拿不到「服务端通知」这类能力 —— 真需要时再引入 SSE，不影响现在的工具定义。
//
// 身份：`Authorization: Bearer <PAT>`（令牌表见迁移 542）。**匿名一律拒**：
// 这个端点直接暴露业务数据，没有「先连上再谈权限」这回事。
//
// 权限是**两关**，缺一不可：
//
//	令牌声明的 scope   ∩   归属账号在 Casbin 里仍然拥有的权限
//
// 只判第一关 = 令牌写成什么就能干什么（账号被降权后令牌还有效）；
// 只判第二关 = 任何登录的人的令牌都能干他本人的全部事（令牌失去了「收窄」的意义）。
// 两关都过才执行，任一不过就当作「这个工具不存在」（见 tools/call 的注释）。
package aihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
)

const (
	// mcpProtocolVersion 本端声明的协议版本（MCP 客户端按此协商）。
	mcpProtocolVersion = "2024-11-05"
	// mcpServerName 服务端标识（客户端会在界面上显示它）。
	mcpServerName = "go_wp"
	// mcpServerVersion 服务端版本标识。
	mcpServerVersion = "1.0"
	// mcpMaxBodyBytes 请求体上限：MCP 请求都很小（参数里可能有长文本，但不需要兆级）。
	mcpMaxBodyBytes = 1 << 20
)

// JSON-RPC 2.0 错误码（只列本端点会回的几个）。
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// AccountAuthorizer 判定归属账号在权限系统里是否仍有某权限点。
//
// 做成端口而不是直接调 Casbin：本端点要在**没有 Casbin** 的环境里也能被测
// （端点的职责是协议与两关判定，而 Casbin 是装配期接上的），
// 与 ToolProvider / ScopeValidator 同一纪律 —— 装配层才知道用哪套权限系统。
type AccountAuthorizer func(ctx context.Context, userID int64, perm permission.Perm) (bool, error)

// mcpEnabledTTL 开关判定的缓存时长。
//
// 为什么缓存：这是每个请求都要做的一次判断，而它改动的频率是「人手点一下」。
// 5 秒意味着关掉开关后最坏 5 秒生效 —— 比一次库查询便宜得多，也不至于让「刚关掉」看起来没生效。
// 不做推送式即时失效：多副本部署下那需要一条广播通道，收益配不上复杂度。
const mcpEnabledTTL = 5 * time.Second

// McpEndpoint 外部接入点。
type McpEndpoint struct {
	tokens    *aiservice.AccessTokenService
	registry  *mcp.Registry
	recorder  *aiservice.ToolCallRecorder
	authorize AccountAuthorizer
	config    sysconfigcontract.ConfigReader

	// 开关判定的短缓存（见 mcpEnabledTTL）。锁只护这两个字段，不进任何 I/O 的临界区
	// —— 锁里做的唯一一件事是读配置，而它是我们自己的库查询。
	enabledMu    sync.Mutex
	enabledValue bool
	enabledUntil time.Time
}

// NewMcpEndpoint 构造；tokens / registry 任一为 nil 时端点回 503（装配缺陷要看得见，而不是静默空工具集）。
func NewMcpEndpoint(tokens *aiservice.AccessTokenService, registry *mcp.Registry, recorder *aiservice.ToolCallRecorder) *McpEndpoint {
	return &McpEndpoint{tokens: tokens, registry: registry, recorder: recorder, authorize: casbinAuthorizer}
}

// SetAccountAuthorizer 替换账号权限判定端口（默认走 Casbin；用例与测试环境注入替身）。
func (e *McpEndpoint) SetAccountAuthorizer(a AccountAuthorizer) { e.authorize = a }

// SetConfigReader 注入配置读取口。
//
// **不注入 = 关闭**：外部接入点没有「默认开着」这一说。装配漏了这一句，
// 结果是「端点不可达」，而不是「端点对全网可达」—— 这两种失败方式的代价差着量级。
func (e *McpEndpoint) SetConfigReader(r sysconfigcontract.ConfigReader) { e.config = r }

// enabled 读「对外接入点是否启用」。
//
// fail closed 的四处：没注入读取口 / 组读不到 / 组里没有这个键 / 值不是布尔 true —— 全按关闭。
// 任何一处「读不出来就当成开着」的写法，都会把一次配置读取故障变成一次对外暴露。
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

// rpcRequest 一条 JSON-RPC 请求。ID 用 RawMessage 原样保留（数字/字符串都要能回对）。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcError JSON-RPC 错误体。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Handle POST /mcp。
func (e *McpEndpoint) Handle(c *gin.Context) {
	// 开关先于认证：关着的时候连「令牌对不对」都不回答。
	// 回 404 而不是 403 —— 探测者不该从响应里知道「这里有个可以打开的东西」。
	if !e.enabled(c.Request.Context()) {
		c.Status(http.StatusNotFound)
		return
	}
	// 认证：先于协议解析。未认证的请求不该得到「方法不存在」这类协议层信息。
	identity, err := e.authenticate(c)
	if err != nil {
		// 对外只回一种说法（401 + 与令牌无关的文案）：区分「不存在 / 已撤销 / 已过期」
		// 等于告诉爆破者哪一半猜对了（与 AccessTokenService.Verify 同口径）。
		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, gin.H{"error": facingKey(aienums.ErrTokenInvalid)})
		return
	}
	if e.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": facingKey(aienums.ErrToolRunFailed)})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mcpMaxBodyBytes)
	var req rpcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeRPCError(c, nil, rpcParseError, "请求不是合法的 JSON-RPC 报文")
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		writeRPCError(c, req.ID, rpcInvalidRequest, "缺少 jsonrpc=2.0 或 method")
		return
	}

	// 通知（没有 id）不产生响应体：MCP 的 notifications/* 走这一支，
	// 回一个 JSON 反而会让客户端以为收到了应答。
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
		e.callTool(c, req, identity)
	default:
		if notification {
			// 未知通知按协议静默忽略（回错误反而干扰客户端）。
			c.Status(http.StatusAccepted)
			return
		}
		writeRPCError(c, req.ID, rpcMethodNotFound, "不支持的方法："+req.Method)
	}
}

// authenticate 从 Authorization 头取 PAT 并校验。
func (e *McpEndpoint) authenticate(c *gin.Context) (*aiservice.TokenIdentity, error) {
	if e.tokens == nil {
		return nil, errors.New(aienums.ErrTokenInvalid)
	}
	raw := strings.TrimSpace(c.GetHeader("Authorization"))
	// 只接受 Bearer（大小写不敏感，RFC 6750 的 scheme 是大小写不敏感的）。
	const scheme = "bearer "
	if len(raw) < len(scheme) || !strings.EqualFold(raw[:len(scheme)], scheme) {
		return nil, errors.New(aienums.ErrTokenInvalid)
	}
	return e.tokens.Verify(c.Request.Context(), strings.TrimSpace(raw[len(scheme):]))
}

// toolsFor 列出这个令牌**实际能用**的工具（scope ∩ 账号权限）。
//
// 为什么不列出全部再把调用打回：外部 AI 看到「有 20 个工具」却只能用其中 2 个，
// 会反复尝试、反复失败，把上下文浪费在无效往返上。看不到即不存在，是这里最省事也最诚实的做法。
func (e *McpEndpoint) toolsFor(ctx context.Context, id *aiservice.TokenIdentity) []gin.H {
	out := make([]gin.H, 0, 8)
	for _, t := range e.registry.List() {
		if !e.allowed(ctx, id, t.Permission()) {
			continue
		}
		schema, err := t.SchemaJSON()
		if err != nil {
			// schema 序列化不了的工具不能声明出去（客户端会拿它校验参数）。
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

// allowed 两关判定：令牌声明了该权限点 + 归属账号在 Casbin 里仍然拥有它。
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
		// 判定本身出错时按无权限处理（fail closed），只记日志。
		logger.Scene("ai").With("user", id.UserID).With("perm", string(perm)).Error(err, "MCP 工具权限判定失败")
		return false
	}
	return ok
}

// callTool 执行一次工具调用。
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
		// 无权限与不存在**回同一句话**：区分它们就等于让外部探测出「本站有这么个工具，
		// 只是你这个令牌没权限」。对真正需要知道的人，管理页里有完整清单。
		writeRPCError(c, req.ID, rpcInvalidParams, "工具不存在或当前凭证无权调用")
		return
	}

	start := time.Now()
	res, err := e.registry.Invoke(c.Request.Context(), tool.Name(), params.Arguments)
	e.record(c.Request.Context(), id, tool, params.Arguments, res.Text, err, time.Since(start))

	if err != nil {
		// 工具本身的失败也走 200 + isError：MCP 协议里「工具执行失败」是**正常应答**，
		// 客户端要把这句话交给模型（它能据此换条件重试），而不是当成传输层故障。
		var argsErr *mcp.ArgsError
		text := facingKey(aienums.ErrToolRunFailed)
		if errors.As(err, &argsErr) {
			text = argsErr.Msg // 参数错的具体说明对模型最有用（缺了哪个字段、类型不对）
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

// record 落一条工具调用流水（外部调用 session_id = 0）。
//
// 外部调用同样进审计表：安全要回答的问题（谁在用这把令牌、调了什么、被拒了几次）
// 与会话内调用是同一组，分成两张表就答不利索。
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
	// 结论分类与审计字段的拼装只有这一处（会话内那条路用 aiservice.NewToolCallEntry，
	// 这里多一个「会话 = 0」的差异，其余口径同源）。
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

// writeRPCResult 写一条成功响应（id 原样回传）。
func writeRPCResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": rawID(id), "result": result})
}

// writeRPCError 写一条错误响应。id 为空（解析失败）时按协议回 null。
func writeRPCError(c *gin.Context, id json.RawMessage, code int, message string) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": rawID(id), "error": rpcError{Code: code, Message: message}})
}

// rawID 把请求 id 原样回传；缺失时回 null（JSON-RPC 对无法解析 id 的请求就是这么规定的）。
func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return json.RawMessage(id)
}
