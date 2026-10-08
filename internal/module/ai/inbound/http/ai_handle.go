package aihttp

// 只做绑定与响应：文案一律取 enums，业务判断在 service。参数可走 body（json）或 Query（form），
// 与 ai_handle.go 的其它接口同口径（路由只用 GET / POST）。

// 与非流式的 /admin/ai/ask 的关系：**同一个 defaultModel、同一个 composeFabInput、
// 同一个会话**，差别只在「怎么把过程交给浏览器」。刻意不复制这两段逻辑：
// 它们决定了「模型看到的当前页面是什么」，复制一份必然在下次改页面上下文时漏掉一边，
// 而漏掉的症状是「用流式时模型不知道你在哪个页面」。
//
// 为什么用 SSE 而不是 WebSocket：这是单向的服务器推送，SSE 是它的原生形态 ——
// 浏览器自动重连、事件分隔符由协议定义、HTTP/2 下还能多路复用。WebSocket 要
// 自己定义分帧与心跳，换不来任何这里需要的东西。

// 只做绑定与响应：文案一律取 enums，业务判断在 service。列表 / 查询走 GET + Query 参数，
// 写入走 POST（路由里没有路径参数）。

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

// 与配置面的 ai_handle.go 分开：那是「供应商与模型目录」，这是「会话与事件日志」，
// 两个聚合的消费方与权限面都不一样（本文件按会话资源声明权限点：ai:session_* 一族）。

// 三个入口，一码一路由（与同模块其它接口同口径）：
//
//	GET  /api/ai/token/list   列表（all=1 看全站）
//	POST /api/ai/token/create 签发（**响应里带一次性明文**）
//	POST /api/ai/token/revoke 撤销（不删行）
//
// 明文只出现在 create 的响应里：列表接口永远不回明文与哈希，
// 因为列表要渲染进后台页面，而「页面能看到的」等于「能被截屏带走的」。

// 只接受 **data URI**，不接受 http(s) 地址。理由不是省事，是安全：
// 一个用户可控的 URL 交给「服务端替你去取」的链路，等于把内网地址与云元数据
// （169.254.169.254）也变成可读对象 —— 而这条链路（浏览器 → 本服务 → 上游模型）
// 根本不需要本服务去**取**图：前端读文件转 base64 就够了，图从来不必
// 经过我们这一跳。收下 URL 只会凭空多出一个 SSRF 面。
//
// 校验的粒度按「能不能安全地转发给上游」定：前缀、张数、单张体积。
// 内容本身不解析（我们不是图片处理器），上游的模型会看到它是不是图。

// 落在装配层（inbound/http）而不是 service：会话层不该认识 internal/mcp，也不该认识 Casbin，
// 而这两个恰好都是「接线」才知道的东西。换一套工具实现（例如将来由插件提供的工具）
// 只需在这里再包一层，会话层一行都不用动。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/ai/contract"
	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/internal/module/ai/service"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// Chat POST /api/ai/chat → 一次对话，回文本。
//
// 绑定失败归口 ErrInvalidParam；供应商不存在归 404，其余业务错误归 400。
// 上游报文的原文不出现在响应里（service 侧只回 enums key，原文进日志）。
func (h *Handle) Chat(c *gin.Context) {
	var req aidto.ChatReq
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	// 归属由服务端定，**不采信请求体**：调用流水里的「谁调用的」如果来自请求参数，
	// 任何人都能把别人的名字写进审计流水。SessionID 一律清零 —— 这条路由不属于任何会话
	// （会话页的发消息走 SendMessage，那边由会话层填自己正在续写的那条）。
	req.UserID = userID(c)
	req.SessionID = 0
	result, err := h.svc.Chat(c.Request.Context(), &req)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, aiservice.ErrProviderNotFound) {
			code = http.StatusNotFound
		}
		response.ErrorAuto(c, code, "ai", err)
		return
	}
	response.Success(c, result)
}

// fabStreamChunk 推给浏览器的一条 SSE 数据。
//
// 字段名刻意短（每次增量都要序列化一遍）：一次回答可能有几百条。
type fabStreamChunk struct {
	// Kind 取值：reasoning | text | tool | done | error。
	//
	// 与 aiservice.StreamEvent 的 Kind 一一对应，另加两个只有传输层才有的：
	// done（带最终结果）与 error（把失败说成人话）。这样浏览器只需认识一套词。
	Kind string `json:"kind"`
	// Text 本条的文本。
	Text string `json:"text,omitempty"`
	// Answer 只在 done 时给：模型这一轮的完整正文（浏览器用它替换掉逐字拼出来的那份，
	// 两者**不应该有差别** —— 有差别说明增量累积漏了或多了，那本身就是缺陷信号）。
	Answer string `json:"answer,omitempty"`
	// Note 只在 done 时给：截断提示、查了几次数据这类附注（整句由服务端拼好）。
	Note string `json:"note,omitempty"`
}

// FabAskStream POST /admin/ai/ask/stream → SSE 流式回答。
//
// 状态码一律 200（连同错误）：SSE 的客户端把非 2xx 当成传输失败，拿不到我们
// 精心写的错误文案；把失败做成一条 kind=error 的事件，浏览器才能把它显示在
// 面板里（与会话页 / 悬浮球非流式路径同一条口径）。
func (h *SessionPageHandle) FabAskStream(c *gin.Context) {
	providerKey, model, ok := h.defaultModel(c.Request.Context())
	if !ok {
		writeFabStreamError(c, fabText(aienums.MsgFabNoModel))
		return
	}
	input := strings.TrimSpace(c.PostForm("input"))
	if input == "" {
		writeFabStreamError(c, fabText(aienums.MsgFabEmptyInput))
		return
	}
	truncated := false
	if runes := []rune(input); len(runes) > fabInputLimit {
		input = string(runes[:fabInputLimit])
		truncated = true
	}

	// 图片：前端把文件读成 data URI 传上来（服务端不去取任何地址，理由见 ai_fab_image.go）。
	images, imageLabels, imgErr := parseFabImages(c.PostForm("images"), c.PostForm("imageLabels"))
	if imgErr != nil {
		writeFabStreamError(c, fabText(imgErr.Key))
		return
	}

	// 先写头再写第一个字节：不预热的话 gin 会等第一次 Write 才发头，
	// 而那之前的几十毫秒浏览器拿不到任何响应（表现为「点了没反应」）。
	// X-Accel-Buffering: no 关掉反向代理的响应缓冲 —— 开着的话整个流会被
	// 代理攒到最后一次性下发，逐字效果完全消失（而这在本地直连时看不出来）。
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
			// 浏览器关掉了面板 / 断开了：写不进去只意味着没人听了，
			// 不是错误。中断由下面的 ctx 取消传导到上游。
			return
		}
		c.Writer.Flush()
	}

	res, err := h.svc.SendMessageStream(c.Request.Context(), aidto.SendMessageReq{
		SessionKey:  fabSessionKey,
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
		flush(fabStreamChunk{Kind: "error", Text: fabText(aienums.MsgFabFailed)})
		return
	}

	answer := strings.TrimSpace(res.AssistantEvent.Content)
	note := fabDoneNote(res, truncated)
	flush(fabStreamChunk{Kind: "done", Answer: answer, Note: note})
}

// fabDoneNote 拼收尾那句附注（没有可说的就回空串）。
func fabDoneNote(res *aidto.SendMessageResult, truncated bool) string {
	parts := make([]string, 0, 2)
	if truncated {
		parts = append(parts, fabText(aienums.MsgFabTruncated))
	}
	if n := len(res.ToolEvents); n > 0 {
		parts = append(parts, fabText(aienums.MsgFabToolsPrefix)+strconv.Itoa(n)+fabText(aienums.MsgFabToolsSuffix))
	}
	return strings.Join(parts, " ")
}

// FabHistory GET /admin/ai/fab/history：把这条会话已有的对话交给界面。
//
// 存在的理由：悬浮球与概览页的提问框都是单次问答的渲染形态 —— 关掉面板、刷新页面、
// 切到另一个后台页，界面上就只剩一个空输入框，看起来像搜索引擎；而服务端一直是
// 同一条会话在续写（fabSessionKey 固定，历史进 stablePrefix）。模型记得上一轮，
// 用户却看不见，于是「它是个搜索框」这个印象来自界面而不是能力。
//
// 只读、不建会话：首访（还没问过任何问题）回空数组而不是 404。
func (h *SessionPageHandle) FabHistory(c *gin.Context) {
	turns, err := h.svc.RecentDialogue(c.Request.Context(), fabSessionKey, 0)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	if turns == nil {
		turns = []aidto.DialogueTurn{}
	}
	response.Success(c, gin.H{"items": turns})
}

// writeFabStreamError 以一条 error 事件结束（状态码仍是 200，理由见 FabAskStream）。
func writeFabStreamError(c *gin.Context, text string) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(fabStreamChunk{Kind: "error", Text: text})
	_, _ = c.Writer.Write([]byte("data: " + string(payload) + "\n\n"))
	c.Writer.Flush()
}

// Handle JSON 接口的处理器。
type Handle struct {
	svc aicontract.AIService
}

// NewHandle 构造处理器。
func NewHandle(svc aicontract.AIService) *Handle { return &Handle{svc: svc} }

// ListProviders GET /api/ai/provider/list → 供应商列表（含模型目录，不含密钥明文）。
func (h *Handle) ListProviders(c *gin.Context) {
	rows, err := h.svc.ListProviders(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "ai", err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

// GetProvider GET /api/ai/provider/get?id= → 单个供应商。
func (h *Handle) GetProvider(c *gin.Context) {
	id := parseInt64(c.Query("id"))
	if id <= 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.Success(c, provider)
}

// saveProviderBody POST /api/ai/provider/save 的请求体。
type saveProviderBody struct {
	ID          int64  `json:"id"`
	ProviderKey string `json:"providerKey"`
	DisplayName string `json:"displayName"`
	BaseURL     string `json:"baseUrl"`
	Protocol    string `json:"protocol"`
	APIKey      string `json:"apiKey"`
	Status      *int   `json:"status"`
	Sort        *int   `json:"sort"`
	Version     int64  `json:"version"`
}

// SaveProvider POST /api/ai/provider/save → 新建（id 缺省 / 0）或按版本号更新。
func (h *Handle) SaveProvider(c *gin.Context) {
	var body saveProviderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	req := &aidto.SaveProviderReq{
		ID:          body.ID,
		ProviderKey: body.ProviderKey,
		DisplayName: body.DisplayName,
		BaseURL:     body.BaseURL,
		Protocol:    body.Protocol,
		APIKey:      body.APIKey,
		Status:      body.Status,
		Version:     body.Version,
		UpdateBy:    userID(c),
	}
	if body.Sort != nil {
		req.Sort = *body.Sort
	}
	provider, err := h.svc.SaveProvider(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSaved, provider)
}

// DeleteProvider POST /api/ai/provider/delete?id= → 删除供应商（含模型目录）。
func (h *Handle) DeleteProvider(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	if err = h.svc.DeleteProvider(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgDeleted, nil)
}

// setStatusBody POST /api/ai/provider/status 的请求体。
type setStatusBody struct {
	ID      int64 `json:"id"`
	Status  int   `json:"status"`
	Version int64 `json:"version"`
}

// SetProviderStatus POST /api/ai/provider/status → 启停供应商。
func (h *Handle) SetProviderStatus(c *gin.Context) {
	var body setStatusBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.SetProviderStatus(c.Request.Context(), &aidto.SetStatusReq{
		ID:       body.ID,
		Status:   body.Status,
		Version:  body.Version,
		UpdateBy: userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgStatusChanged, provider)
}

// ListModels GET /api/ai/provider/models/list?id= → 某供应商的模型目录。
func (h *Handle) ListModels(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.GetProvider(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "ai", err)
		return
	}
	response.Success(c, gin.H{"providerId": provider.ID, "list": provider.Models})
}

// saveModelsBody POST /api/ai/provider/models/save 的请求体。
type saveModelsBody struct {
	ProviderID int64              `json:"providerId"`
	Version    int64              `json:"version"`
	Models     []aidto.ModelEntry `json:"models"`
}

// SaveModels POST /api/ai/provider/models/save → 整组保存模型目录。
func (h *Handle) SaveModels(c *gin.Context) {
	var body saveModelsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.SaveModels(c.Request.Context(), &aidto.SaveModelsReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		Models:     body.Models,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsSaved, provider)
}

// providerActionBody 「恢复默认模型」「获取可用模型」的请求体。
type providerActionBody struct {
	ProviderID int64 `json:"providerId"`
	Version    int64 `json:"version"`
}

// RestoreDefaultModels POST /api/ai/provider/models/restore → 用内置清单替换模型目录。
func (h *Handle) RestoreDefaultModels(c *gin.Context) {
	var body providerActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	provider, err := h.svc.RestoreDefaultModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsRestored, provider)
}

// FetchAvailableModels POST /api/ai/provider/models/fetch → 调 {base_url}/models 拉候选并合并。
func (h *Handle) FetchAvailableModels(c *gin.Context) {
	var body providerActionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	result, err := h.svc.FetchAvailableModels(c.Request.Context(), &aidto.ProviderActionReq{
		ProviderID: body.ProviderID,
		Version:    body.Version,
		UpdateBy:   userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgModelsFetched, result)
}

// bindID 绑定查询参数里的 id（Query 优先，兼容表单）。
func bindID(c *gin.Context) (int64, error) {
	if id := parseInt64(c.Query("id")); id > 0 {
		return id, nil
	}
	if id := parseInt64(c.PostForm("id")); id > 0 {
		return id, nil
	}
	return 0, errors.New(aienums.ErrInvalidParam)
}

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

// SessionHandle 会话层的 JSON 出口。
type SessionHandle struct{ svc *aiservice.SessionService }

// NewSessionHandle 构造。
func NewSessionHandle(svc *aiservice.SessionService) *SessionHandle { return &SessionHandle{svc: svc} }

// sessionAppendBody 追加事件的请求体。
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

// sessionRenameBody 改标题 / 换模型 / 归档的请求体。
type sessionRenameBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	// Status 缺省为 -1（不改状态）；0 归档、1 恢复。
	Status  *int16 `json:"status"`
	Version int64  `json:"version"`
}

// sessionFoldPlanBody 折叠建议的请求体。
type sessionFoldPlanBody struct {
	SessionID  int64 `json:"sessionId"`
	KeepRecent int   `json:"keepRecent"`
}

// sessionFoldBody 提交折叠的请求体。
type sessionFoldBody struct {
	SessionID     int64  `json:"sessionId"`
	FromSeq       int64  `json:"fromSeq"`
	ToSeq         int64  `json:"toSeq"`
	Summary       string `json:"summary"`
	SummaryTokens int64  `json:"summaryTokens"`
}

// List GET /api/ai/session/list：分页列出会话。
func (h *SessionHandle) List(c *gin.Context) {
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "20"), 20)
	status := sessionIntOr(c.DefaultQuery("status", "-1"), -1)
	rows, total, err := h.svc.ListSessions(c.Request.Context(), c.Query("keyword"), status, page, size)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total, "page": page, "size": size})
}

// Get GET /api/ai/session/get：会话详情（会话头 + 投影后的当前上下文）。
func (h *SessionHandle) Get(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	detail, err := h.svc.GetSession(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, detail)
}

// Events GET /api/ai/session/events：原始事件日志（压缩后的原文仍在这里，供展开与审计）。
func (h *SessionHandle) Events(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "50"), 50)
	rows, total, err := h.svc.ListEvents(c.Request.Context(), id, page, size)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total})
}

// Append POST /api/ai/session/append：追加一条事件（会话层唯一的写入口）。
func (h *SessionHandle) Append(c *gin.Context) {
	var body sessionAppendBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
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
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionAppended, res)
}

// Rename POST /api/ai/session/rename：改标题 / 换当前模型（乐观锁）。
func (h *SessionHandle) Rename(c *gin.Context) {
	var body sessionRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
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
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

// Archive POST /api/ai/session/archive：归档（status=0）。
//
// 单独开一个端点而不是让调用方自己拼 status：归档是「只往前、不回头地停写」这个语义，
// 把它做成具名动作，接口读起来才是那个意思。
func (h *SessionHandle) Archive(c *gin.Context) {
	var body sessionRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
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
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

// FoldPlan POST /api/ai/session/fold/plan：算建议折叠区间与净收益（只读）。
func (h *SessionHandle) FoldPlan(c *gin.Context) {
	var body sessionFoldPlanBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	plan, err := h.svc.FoldPlan(c.Request.Context(), aidto.FoldPlanReq{
		SessionID:  body.SessionID,
		KeepRecent: body.KeepRecent,
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, plan)
}

// Fold POST /api/ai/session/fold：提交一次折叠（区间 + 摘要）。
func (h *SessionHandle) Fold(c *gin.Context) {
	var body sessionFoldBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
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
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionFolded, res)
}

// sessionIntOr 解析整数，失败时回落默认值（查询参数不受信）。
func sessionIntOr(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

// TokenHandle 令牌接口的处理器。
type TokenHandle struct {
	svc *aiservice.AccessTokenService
}

// NewTokenHandle 构造处理器；svc 为 nil 时各接口回 500（装配缺陷要看得见）。
func NewTokenHandle(svc *aiservice.AccessTokenService) *TokenHandle { return &TokenHandle{svc: svc} }

// tokenCreateBody 签发请求体。归属账号**不在**请求体里（服务端从登录态取）。
type tokenCreateBody struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expiresAt"`
}

// tokenRevokeBody 撤销请求体。
type tokenRevokeBody struct {
	ID int64 `json:"id"`
}

// List GET /api/ai/token/list?all=&limit= → 令牌列表。
func (h *TokenHandle) List(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	// all 的权限判定在 service（它决定查全站还是只查自己）：页面路由与 API 用的是同一个权限点，
	// 但「能看全站」这件事需要 AITokenList 之外的判断时，改动点只有 service 一处。
	req := &aidto.TokenListReq{
		All:    c.Query("all") == "1" || c.Query("all") == "true",
		Limit:  int(parseInt64(c.Query("limit"))),
		UserID: userID(c),
	}
	rows, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"list": rows})
}

// Create POST /api/ai/token/create → 签发一把令牌（响应里的 token 是**唯一一次**明文）。
func (h *TokenHandle) Create(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	var body tokenCreateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &aidto.TokenCreateReq{
		Name:      body.Name,
		Scopes:    body.Scopes,
		ExpiresAt: body.ExpiresAt,
		// 归属账号由服务端定，不采信请求体（否则令牌可以挂到别人名下，审计失去意义）。
		UserID: userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, res)
}

// Revoke POST /api/ai/token/revoke → 撤销一把令牌（不删行）。
func (h *TokenHandle) Revoke(c *gin.Context) {
	if h.svc == nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, aienums.ErrInternal)
		return
	}
	var body tokenRevokeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), body.ID); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"id": body.ID})
}

const (
	// fabImageMaxCount 一次提问最多带几张图。
	//
	// 上限存在的原因是上下文：每张图都会以 base64 形态进请求（体积约为原图的 4/3），
	// 张数不设限时一条提问可以轻易把输入推到几十万 token，而失败形态是
	// 「上游报上下文超限」，用户完全看不出是自己粘了太多图。
	fabImageMaxCount = 4
	// fabImageMaxBytes 单张图的 data URI 长度上限（约等于 3MB 原图）。
	//
	// 按**编码后**长度算：真正占上下文与内存的是 base64 那一串。
	fabImageMaxBytes = 4 << 20
	// fabImageDataPrefix 合法的 data URI 前缀。
	fabImageDataPrefix = "data:image/"
)

// fabImageError 图片校验失败的原因（取值为 aienums 的 MsgFab* 常量）。
type fabImageError struct {
	Key string
}

// parseFabImages 解析前端随提问带上来的图片。
//
// 两个入参都是 JSON 数组文本（form 里传数组只能是 JSON 字符串）：
// images 是 data URI 数组，labels 是与之一图对一图的标识（文件名）。
// 返回的 aidto.SendMessageReq 片段里，Images 给模型、ImageLabels 给人看。
//
// 校验失败时**整批拒绝**（返回错误）而不是丢掉坏的那几张：悄悄少发一张图的
// 症状是「模型说看不到第二张」，用户不会想到是服务端替他做了取舍。
func parseFabImages(rawImages, rawLabels string) ([]string, []string, *fabImageError) {
	rawImages = strings.TrimSpace(rawImages)
	if rawImages == "" {
		return nil, nil, nil
	}
	var images []string
	if err := json.Unmarshal([]byte(rawImages), &images); err != nil {
		return nil, nil, &fabImageError{Key: aienums.MsgFabImageBadFormat}
	}
	// 空数组与没有图是同一件事，不必让调用方再判一次。
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
		// 标识解不出来不算错误：它只影响界面上那行小字，图本身照发。
		_ = json.Unmarshal([]byte(rawLabels), &labels)
		labels = nonEmptyStrings(labels)
	}
	// 标识与图**一图对一图**：数量对不上时补齐（截断多余的 / 用占位补缺的），
	// 否则界面会把 A 图的名字画在 B 图下面 —— 那种错位比没有名字更难发现。
	labels = alignLabels(labels, len(images))
	return images, labels, nil
}

// nonEmptyStrings 去掉空白项（前端可能因为一次粘贴里的换行产生空串）。
func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// alignLabels 把标识数组对齐到 n 个（多余的截掉，缺少的补占位）。
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

// fabImageFallbackLabel 没有文件名时界面上的占位标识。
//
// 用固定的中文占位而不是空串：空串会让界面画出「📎 图片：」这样一句没头没尾的话，
// 看起来像渲染坏了。
const fabImageFallbackLabel = "图片"

// fabImageRequest 给 SendMessageReq 填上图片相关的三个字段（images / labels）。
func fabImageRequest(req *aidto.SendMessageReq, images, labels []string) {
	req.Images = images
	req.ImageLabels = labels
}

// casbinAuthorizer 按**权限点**判权限：把权限点换算成声明它的路由，再走与页面中间件
// 同一套 Casbin 策略（obj=路径, act=HTTP 方法）。
//
// 为什么不能拿权限点当 obj 直接 Enforce：策略里存的是路由路径（`/api/order/list`），
// 权限点（`order:list`）只是**声明侧的标识**。拿权限点当 obj 会永远匹配不到任何策略，
// 于是所有人调用所有工具都被判越权 —— 一个「看起来像权限配错了」的全量故障。
//
// 一个权限点通常有若干条路由（一条 API + 后台页面路由）：**任一条通过即可**。
// 反过来（要求全部通过）会把「页面路由漏 seed」之类历史问题变成工具不可用。
func casbinAuthorizer(_ context.Context, userID int64, perm permission.Perm) (bool, error) {
	routes := permission.RoutesOf(perm)
	if len(routes) == 0 {
		// 权限点没有任何路由声明：这是装配缺陷。按无权限处理（fail closed），
		// 并把问题暴露在日志里 —— 静默放行会让「工具权限没接上」表现成「AI 什么都能查」。
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

// toolProvider 把 *mcp.Runner 适配成 aiservice.ToolProvider。
type toolProvider struct {
	runner *mcp.Runner
}

// Specs 工具声明。
//
// **每次重新转换**，不在装配期缓存一份：各模块自己装配，AI 模块的装配通常排在
// 领域模块（订单、分析）之前，缓存会让先装配的那一刻看到「一个工具都没有」，
// 而此后永远不会刷新 —— 表现是「AI 明明配了工具却从不调用」。
// 工具数量是个位数，每次重转的成本可以忽略。
func (p *toolProvider) Specs() []aidto.ToolSpec {
	tools := p.runner.Tools()
	if len(tools) == 0 {
		return nil
	}
	out := make([]aidto.ToolSpec, 0, len(tools))
	for _, t := range tools {
		raw, err := t.SchemaJSON()
		if err != nil {
			// 跳过这一个，而不是整轮不带工具：一个工具的参数声明序列化失败，
			// 不该让其它工具一起失效。兜底成空 schema 更糟 —— 模型会一直用空参数调它，
			// 而每次都被参数校验拒掉，看起来像这个工具自己坏了。
			logger.Scene("ai").With("tool", t.Name()).Error(err, "工具 schema 序列化失败，本轮不暴露该工具")
			continue
		}
		out = append(out, aidto.ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: raw})
	}
	return out
}

// Run 执行一次工具调用。
//
// 契约（见 aiservice.ToolProvider）：**业务性失败以文本返回**（err = nil），
// 让模型能转述给用户或据此改正；error 只用于「这轮对话不该继续」。
// 这里把所有执行错误都翻成文本，因为它们无一例外都属于「模型该知道、用户该被告知」那一类。
func (p *toolProvider) Run(ctx context.Context, userID int64, name, arguments string) (aiservice.ToolRunResult, error) {
	res, err := p.runner.Run(ctx, userID, name, arguments)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// 上下文取消由服务层处理（它知道该不该上抛），这里不吞。
			return aiservice.ToolRunResult{}, ctxErr
		}
		text, status := failureText(name, err)
		return aiservice.ToolRunResult{Text: text, Status: status}, nil
	}
	// 成功：结果文本由服务层剪枝后进上下文，这里不预判长度。
	// Data 一并带出去 —— 它是**渲染**用的结构（如 uispec.Spec），不进模型上下文，
	// 由会话层在拿到身份之后决定要不要据此取数。
	return aiservice.ToolRunResult{Text: res.Text, Status: aienums.ToolCallStatusOK, Data: res.Data}, nil
}

// failureText 把执行错误翻成「能回给模型的一句话」+「给审计的结论分类」。
//
// 分档的判据是「模型拿这句话能做什么」：
//
//	· 参数错误 → **原样回**（字段名与缺项模型自己能看懂，据此改正并重试）；
//	· 越权     → 明确的「没有权限」（用户需要知道这不是故障，而是账号权限问题）；
//	· 其它     → 统一的失败文案。原文可能带表名、连接串、内部路径，
//	             而它最终会经模型的嘴出现在页面上。
//
// 分类必须与文案同源返回（一个 error → 一对结果）：分两次判断会让
// 「文案说没权限、审计记成失败」这种事在改动中悄悄发生。
func failureText(tool string, err error) (string, aienums.ToolCallStatus) {
	var argsErr *mcp.ArgsError
	if errors.As(err, &argsErr) {
		return argsErr.Msg, aienums.ToolCallStatusArgsError
	}
	var permErr *mcp.PermissionError
	if errors.As(err, &permErr) {
		return facingKey(aienums.ErrToolForbidden), aienums.ToolCallStatusForbidden
	}
	// 未知工具名归到这里：模型拼错了名字，对它来说「这次没查成」就够了 ——
	// 不必告诉用户「工具 orders_sumary 不存在」（那是模型的问题，不是他的）。
	logger.Scene("ai").With("tool", tool).Error(err, "工具执行失败")
	return facingKey(aienums.ErrToolRunFailed), aienums.ToolCallStatusFailed
}

// facingKey 查面向用户的文案并保证非空（未登记时回 key 本身，缺陷可见）。
func facingKey(key string) string {
	if text, ok := aienums.FacingText(key); ok {
		return text
	}
	return key
}
