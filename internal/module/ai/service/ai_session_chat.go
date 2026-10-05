// ai_session_chat.go — 会话页「发消息」：把一次对话落成事件流里的记录。
//
// 与 ai_chat.go 的分工：那边是「打一次上游」（provider → HTTP → 文本），
// 这边是「在一条会话里说一句话」（定位会话 → 上下文投影 → 调上游 → 落库）。
// 两者只通过下面这个窄接口相连，会话层不 import 配置层的任何具体类型。
//
// 带工具的轮次在本文件里编排：模型要求调工具 → 执行 → 结果回灌 → 再打一次，
// 直到它给出正文或撞上轮次上限。事件日志是本层的真源，所以每一对
// 「调用 / 结果」都各落一条事件（kind=tool，靠 meta.phase 区分）。
package aiservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/internal/uispec"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// chatTitleRunes 用首条用户消息派生会话标题时最多取多少个字符。
//
// 会话头要有标题才能进列表，而发消息这条路没有单独的「起个名字」步骤，
// 就取消息开头一段 —— 与前端「首条消息即标题」的习惯一致。
const chatTitleRunes = 60

// maxToolRounds 一次发消息里最多允许几轮「模型要求调工具」。
//
// 必须有上限：模型陷入「调工具 → 结果不满意 → 再调同一个」时，没有上限就是
// 一次请求把额度烧光，而用户看到的是界面一直转。4 轮足够覆盖
// 「查一个维度 → 发现要换区间 → 再查一次 → 作答」这类真实链路。
const maxToolRounds = 4

// 工具事件的两种相位（kind 恒为 tool，用 meta.phase 区分）。
//
// 两者分开是因为**谁写的**不同：调用是模型要求的，结果是系统执行的。
// 合成一条会让审计看不出「模型要了什么」与「系统给了什么」的差别 ——
// 而排查「它为什么查了这个」时，这正是唯一想知道的事。
const (
	toolPhaseCall   = "call"
	toolPhaseResult = "result"
)

// ChatPort 会话层需要的「打一次模型」能力，由装配层注入。
//
// 用接口而不是 *Service：会话层与配置层刻意不互相依赖（见 ai_session_service.go 头注释），
// 装配处一行 SetChatPort 把两者接上，两层的编译期依赖保持单向。
type ChatPort interface {
	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
}

// ToolRunResult 一次工具执行的结论。
//
// 为什么要分类而不是只回文本：文本是**给模型看的**（成功是结果、失败是一句能力范围内的交代），
// 而审计要的是**给运维与安全看的**结论 —— 「模型参数给错」和「这个账号没权限」
// 在文本上都是「工具执行失败」，混在一起就答不出「被拒了多少次」。
type ToolRunResult struct {
	// Text 回给模型 / 落进工具事件的文本。
	Text string
	// Status 结论分类（见 aienums.ToolCallStatus）。装配层留空时按失败记 —— 拿不准就别记成功。
	Status aienums.ToolCallStatus
	// Data 供**渲染**用的结构化结果：不进模型上下文，只落进工具事件的 meta.render。
	//
	// 与 Text 刻意分开，而且是**单向**的：Text 进模型，Data 不进。反过来（把结构或数字
	// 塞进 Text）会让模型把它当成自己已知的事实复述出去 —— 于是「数字只来自查询」这条约束
	// 在下一轮就失效了，而页面上看起来一切正常。
	Data any
}

// ToolProvider 会话层需要的「工具清单 + 执行」能力，由装配层注入。
//
// 未注入 = 不带工具的一问一答。这与 ChatPort 未注入时的处理**刻意不同**：
// 没有对话能力时这条路根本走不通（回错误），而没有工具只是少了一种能力 ——
// 把「可选增强缺失」也判成失败，会让没接工具的部署连普通对话都用不了。
type ToolProvider interface {
	// Specs 当前可用的工具声明。
	//
	// 每次发消息重新取，而不是装配期缓存一份：装配顺序决定了工具注册发生在
	// 本服务构造之后（各模块自己装配），缓存会让先装配的模块永远看不到后注册的工具。
	Specs() []aidto.ToolSpec
	// Run 执行一次工具调用。
	//
	// 契约：**业务性失败也必须以文本返回**（越权、参数不合法、查库失败）——
	// 那些话要由模型转述给用户（「你没有权限查订单」是用户能得到的最好回答）。
	// error 只用于「这轮对话不该继续」（上下文取消），由本层上抛。
	Run(ctx context.Context, userID int64, name, arguments string) (ToolRunResult, error)
}

// SetChatPort 注入对话能力；未注入时 SendMessage 回 ErrSessionChatUnavailable（不 panic）。
func (s *SessionService) SetChatPort(p ChatPort) { s.chat = p }

// SetToolProvider 注入工具能力；未注入时按「不带工具」处理（见 ToolProvider 注释）。
func (s *SessionService) SetToolProvider(p ToolProvider) { s.tools = p }

// 发消息这条路的错误哨兵（取值同样是 i18n key，口径见 ai_session_service.go 头注释）。
var (
	// ErrUserRequired AI 调用的第一关卡：没有身份就不受理。
	//
	// 放在 service 而不是中间件：AI 的页面路由虽然都挂了登录态，但调用方不止一个
	// （页面表单 / 会话键续写 / 将来的对外接口），把判据放在**唯一写入口**上，
	// 新增调用方不会漏掉这一关。fail closed：UserID <= 0 一律拒。
	ErrUserRequired = errors.New(aienums.ErrUserRequired)
	// ErrSessionChatUnavailable 装配层没有接上对话能力。
	ErrSessionChatUnavailable = errors.New(aienums.ErrSessionChatUnavailable)
	// ErrSessionChatInputRequired 消息正文为空。
	ErrSessionChatInputRequired = errors.New(aienums.ErrSessionChatInputRequired)
	// ErrSessionChatModelRequired 没给供应商或模型。
	ErrSessionChatModelRequired = errors.New(aienums.ErrSessionChatModelRequired)
	// ErrSessionChatEmptyReply 上游回了空文本（不把空回复写成一条空事件）。
	ErrSessionChatEmptyReply = errors.New(aienums.ErrSessionChatEmptyReply)
	// ErrSessionToolRoundsExceeded 工具调用轮次超限（模型可能陷入了自我循环）。
	ErrSessionToolRoundsExceeded = errors.New(aienums.ErrSessionToolRoundsExceeded)
)

// SendMessage 在一条会话里说一句话：写 user 事件 → 取上下文投影 → 打模型（含工具往返）→ 写 assistant 事件。
//
// 顺序上有三条刻意的选择：
//  1. 用户输入**先落库**再打模型：上游超时或报错时用户写的东西不丢（事件日志是真源，
//     刷新页面后仍能看到自己发过什么）；
//  2. 上下文从**投影**取而不是从原始事件取：折叠生效之后，模型看到的就是折叠后的视图，
//     与页面上「当前上下文」显示的内容一致 —— 可视化与真实输入不能是两份东西；
//  3. 模型回复落成新的 assistant 事件而不是覆盖任何东西：append-only。
//
// 失败语义：模型调用失败或回复为空时，user 事件已经落库（这是有意的），assistant 事件不写。
// 工具往返的事件**已经落下的部分不回滚** —— 审计要能看到它试过什么。
func (s *SessionService) SendMessage(ctx context.Context, req aidto.SendMessageReq) (*aidto.SendMessageResult, error) {
	// 第一关卡：没有身份就不准调用模型。放在最前面（比装配检查还前）——
	// 「谁在调用」是这个功能的准入条件，不是事后的记账字段。
	if req.UserID <= 0 {
		return nil, ErrUserRequired
	}
	if s.chat == nil {
		return nil, ErrSessionChatUnavailable
	}
	providerKey := strings.TrimSpace(req.ProviderKey)
	model := strings.TrimSpace(req.Model)
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return nil, ErrSessionChatInputRequired
	}
	if providerKey == "" || model == "" {
		return nil, ErrSessionChatModelRequired
	}

	// ① 定位会话：给了 ID 就用它（存在性与归档由 AppendEvent 把关）；只给会话键时续写或新建。
	sessionID := req.SessionID
	if sessionID <= 0 {
		key := strings.TrimSpace(req.SessionKey)
		if key == "" {
			return nil, ErrSessionKeyMissing
		}
		head, err := s.EnsureSession(ctx, key, providerKey, model, chatTitleFromInput(input), req.UserID)
		if err != nil {
			return nil, err
		}
		sessionID = head.ID
	}

	// ② 用户输入落库（一并带上 provider/model：走会话键续写时它们参与绑定一致性校验）。
	userRes, err := s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID:   sessionID,
		Kind:        string(aienums.EventKindUser),
		Content:     input,
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     model,
	})
	if err != nil {
		return nil, err
	}
	sessionID = userRes.Session.ID

	// ③ 取投影拼上下文（投影里已经包含刚落的这条输入，不再重复拼一次）。
	//
	// 这一步只做一次：本轮的「工具往返」不走投影，而是作为原生消息追加在历史之后
	//（下一轮请求的 messages）。让工具往返绕开投影是有意的 —— 投影在**当前轮**还没重算，
	// 而工具调用与结果必须**成对**出现在同一次请求里（docs/16 §2.3），
	// 从投影里捞要么漏掉刚写的调用、要么把上一轮的往返重复一遍。
	items, err := s.project(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	history := buildChatInput(items, input)

	// ④ 带工具的循环。
	rounds := make([]aidto.ChatMessage, 0, 8)
	toolEvents := make([]aidto.SessionEventItem, 0, 4)
	specs := s.toolSpecs()
	for round := 0; round < maxToolRounds; round++ {
		msgs := stablePrefix(history)
		msgs = append(msgs, rounds...)

		// 带上会话与发起人：调用流水（ai_call_log）靠这两个字段回答
		// 「谁在什么时候烧了谁家的 token」，而它们只有这里知道
		//（出站层只认识 provider/model 两个字符串）。
		chatRes, err := s.chat.Chat(ctx, &aidto.ChatReq{
			ProviderKey:     providerKey,
			Model:           model,
			Messages:        msgs,
			Tools:           specs,
			MaxOutputTokens: req.MaxOutputTokens,
			SessionID:       sessionID,
			UserID:          req.UserID,
		})
		if err != nil {
			return nil, err
		}
		if chatRes == nil {
			return nil, ErrSessionChatEmptyReply
		}

		// 没有工具调用 = 这一轮就是最终回答。
		if len(chatRes.ToolCalls) == 0 {
			output := strings.TrimSpace(chatRes.Output)
			if output == "" {
				return nil, ErrSessionChatEmptyReply
			}
			return s.finishReply(ctx, sessionID, req, providerKey, model, output, userRes, input, toolEvents)
		}

		// 有工具调用：逐个执行并把「调用 / 结果」成对落库。
		for _, call := range chatRes.ToolCalls {
			callRes, err := s.appendToolEvent(ctx, sessionID, req, providerKey, model, call, toolPhaseCall, "", nil)
			if err != nil {
				return nil, err
			}
			runRes := s.runTool(ctx, sessionID, req.UserID, call)
			resultRes, err := s.appendToolEvent(ctx, sessionID, req, providerKey, model, call, toolPhaseResult, runRes.Text, runRes.Data)
			if err != nil {
				return nil, err
			}
			// 回灌给模型的往返：assistant 要求调用 + tool 给出结果。
			// 两条必须一起追加（缺 assistant 那条会被上游判成「结果没有对应的调用」）。
			rounds = append(rounds,
				aidto.ChatMessage{Role: roleAssistant, ToolCalls: []aidto.ToolCall{call}},
				aidto.ChatMessage{Role: roleTool, ToolCallID: call.ID, Name: call.Name, Content: runRes.Text},
			)
			toolEvents = append(toolEvents,
				sentEventItem(callRes, string(aienums.EventKindTool), toolCallText(call)),
				sentEventItem(resultRes, string(aienums.EventKindTool), runRes.Text),
			)
		}
	}

	// 到达上限：本轮已发生的事件全部保留（审计要看到它试了什么），但这条消息没有答案。
	return nil, ErrSessionToolRoundsExceeded
}

// finishReply 落 assistant 事件并组装返回（循环的唯二出口之一）。
func (s *SessionService) finishReply(
	ctx context.Context,
	sessionID int64,
	req aidto.SendMessageReq,
	providerKey, model, output string,
	userRes *aidto.AppendEventResult,
	input string,
	toolEvents []aidto.SessionEventItem,
) (*aidto.SendMessageResult, error) {
	// 带上 providerKey/model：这条回复是这两家产生的（529 起事件自带来源），
	// 用量按供应商/模型拆开时靠的就是它，而不是会话头那份（换模型时会被覆盖）。
	assistantRes, err := s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID:   sessionID,
		Kind:        string(aienums.EventKindAssistant),
		Content:     output,
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     model,
	})
	if err != nil {
		return nil, err
	}
	return &aidto.SendMessageResult{
		Session:        assistantRes.Session,
		UserEvent:      sentEventItem(userRes, string(aienums.EventKindUser), input),
		AssistantEvent: sentEventItem(assistantRes, string(aienums.EventKindAssistant), output),
		ToolEvents:     toolEvents,
	}, nil
}

// appendToolEvent 落一条工具事件；phase 为 toolPhaseCall 时正文是调用摘要。
func (s *SessionService) appendToolEvent(
	ctx context.Context,
	sessionID int64,
	req aidto.SendMessageReq,
	providerKey, model string,
	call aidto.ToolCall,
	phase, result string,
	data any,
) (*aidto.AppendEventResult, error) {
	content := toolCallText(call)
	if phase == toolPhaseResult {
		content = result
	}
	// meta 是**给机器看的**那半：投影只带 content（人读的部分），
	// 而「哪个工具、哪次调用」只有结构化字段能可靠表达（正文里解析出来的东西迟早会分叉）。
	meta := map[string]any{
		"phase":     phase,
		"callId":    call.ID,
		"tool":      call.Name,
		"arguments": call.Arguments,
	}
	// 可渲染的结构单独一个键（render）：页面侧只认它，不需要知道工具的种类。
	// 超限就不落库（页面会退化成只有正文），而不是截断 —— 半个 JSON 反序列化必然失败，
	// 存下去只会让「为什么这块没渲染」变成一个查不出来的问题。
	if phase == toolPhaseResult && data != nil {
		if raw, err := json.Marshal(data); err != nil {
			logger.Scene("ai").With("tool", call.Name).Warn("渲染数据序列化失败：" + err.Error())
		} else if len(raw) > renderMetaLimit {
			logger.Scene("ai").With("tool", call.Name).With("bytes", len(raw)).
				Warn("渲染数据超过上限，未落库（页面将只显示正文）")
		} else {
			meta["render"] = string(raw)
		}
	}
	return s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID:   sessionID,
		Kind:        string(aienums.EventKindTool),
		Content:     content,
		Meta:        meta,
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     model,
	})
}

// facingToolText 查面向用户的文案并保证拿到一个非空字符串。
//
// FacingText 回 (text, ok)：ok=false 时该回什么，各调用点写得都不一样（有的回空串、
// 有的回 key）。工具事件的正文**必须非空**（空串会被上游当成「没有内容」，
// 与「调用成功但什么都没返回」无法区分），所以这里统一兜底成 key 本身 ——
// 页面上出现一个 key 形态的字符串是明显的缺陷信号，比静默空串好排查。
func facingToolText(key string) string {
	if text, ok := aienums.FacingText(key); ok {
		return text
	}
	return key
}

// toolCallText 工具调用事件的人读正文。
func toolCallText(call aidto.ToolCall) string {
	args := strings.TrimSpace(call.Arguments)
	if args == "" || args == "{}" {
		return fmt.Sprintf("调用 %s", call.Name)
	}
	return fmt.Sprintf("调用 %s：%s", call.Name, args)
}

// toolSpecs 当前可用的工具声明；没接工具时回 nil（出站请求不带 tools 字段）。
func (s *SessionService) toolSpecs() []aidto.ToolSpec {
	if s.tools == nil {
		return nil
	}
	return s.tools.Specs()
}

// runTool 执行一次工具调用，把结果或失败文案回给模型，并落一条审计流水。
//
// 失败**不中断对话**：模型需要知道「这次没查到」，才能回答「查询失败，请稍后再试」
// 而不是自己编一个数字。所以业务性失败回一段归口文案继续往下走；
// 只有上下文取消这类「这轮对话本身不该继续」的错误才上抛。
//
// 失败原文一律只进日志：工具错误里可能带连接串、表名、内部路径，
// 而它会经模型的嘴出现在页面上。
//
// 审计在这里写而不是在装配层：本层**同时**看得到会话、账号、工具、结论与耗时，
// 且「模型看到的结果」正是在这里成形（剪枝后）—— 审计与上下文必须对同一份文本，
// 否则「审计说 320 字、模型看到 4000 字」这类账对不上。
//
// 注意异常路径的文案是固定的 ErrToolRunFailed，不按 status 挑：
// 「参数不合法」「没有权限」这两类由装配层给出**具体**文本（含缺了哪个字段、
// 缺哪个权限点），本层拿不到那些细节，硬挑一个笼统的译法反而把有用信息盖掉。
func (s *SessionService) runTool(ctx context.Context, sessionID, userID int64, call aidto.ToolCall) ToolRunResult {
	start := time.Now()
	// fail 是异常路径的统一出口：顺手把审计写了，
	// 免得下面几个提前 return 各写一遍（漏一个就少一条流水）。
	fail := func(status aienums.ToolCallStatus) ToolRunResult {
		text := facingToolText(aienums.ErrToolRunFailed)
		e := NewToolCallEntry(sessionID, userID, call.Name, call.Arguments, time.Since(start))
		e.Status = string(status)
		e.ErrorKey = ToolErrorKeyOf(status)
		e.ResultSummary = SummarizeForLog(text)
		e.ResultLen = int64(len([]rune(text)))
		s.toolCalls.Record(ctx, e)
		return ToolRunResult{Text: text, Status: status}
	}

	if s.tools == nil {
		return fail(aienums.ToolCallStatusFailed)
	}
	res, err := s.tools.Run(ctx, userID, call.Name, call.Arguments)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// 上下文已取消：这一轮不该继续，但也没必要把整个会话打断 ——
			// 回一句失败文案让上层照常收尾（用户在页面上看到的是「工具执行失败」而不是 500）。
			logger.Scene("ai").With("tool", call.Name).Error(ctxErr, "工具调用被取消")
			return fail(aienums.ToolCallStatusFailed)
		}
		logger.Scene("ai").With("tool", call.Name).With("user", userID).Error(err, "工具执行失败")
		return fail(aienums.ToolCallStatusFailed)
	}
	status := res.Status
	if !aienums.IsValidToolCallStatus(status) {
		// 装配层没给分类（或给了白名单外的值）：按失败记。拿不准就别记成功 ——
		// 审计表里每一条「成功」都应该是真的成功了。
		status = aienums.ToolCallStatusFailed
	}

	// 剪枝：结果进上下文之前先收一次，剪枝标记一并回给模型（见 pruneToolResult）。
	text, truncated := PruneToolResult(res.Text)
	// 空文本不能作为 tool 消息内容：上游会把它当成「没有内容」，
	// 而模型看到的是「调用成功了但什么都没返回」—— 与失败无法区分。
	if strings.TrimSpace(text) == "" {
		logger.Scene("ai").With("tool", call.Name).Warn("工具返回了空结果")
		return fail(status)
	}

	// 展示指令（ui_render）：**在这里**逐块取数，而不是在工具里 ——
	// 只有这一层知道调用者是谁（userID），而每个数据源都要过一次权限判定。
	// 权限问的是「这个账号能不能读这张表」，不是「这个工具有没有这个能力」，
	// 所以取数必须回到有身份的这一层来做。
	var data any
	if spec, ok := res.Data.(*uispec.Spec); ok {
		views := s.renderSpec(ctx, sessionID, userID, spec)
		if len(views) > 0 {
			data = views
		} else if len(spec.Blocks) > 0 {
			// 一块都没渲出来：必须让模型知道「用户这一轮什么都没看到」，
			// 否则它会以为图已经出好了，接着解释一张并不存在的表。
			text = strings.TrimSpace(text) + "\n（系统提示：这次的图表数据源都没有取到数据，用户看不到任何图表。）"
		}
	}

	e := NewToolCallEntry(sessionID, userID, call.Name, call.Arguments, time.Since(start))
	e.Status = string(status)
	e.ErrorKey = ToolErrorKeyOf(status)
	e.ResultSummary = SummarizeForLog(text)
	e.ResultLen = int64(len([]rune(res.Text)))
	e.Truncated = truncated
	s.toolCalls.Record(ctx, e)
	return ToolRunResult{Text: text, Status: status, Data: data}
}

// buildChatInput 把当前投影拼成一段纯文本发给上游。
//
// 格式选择「每项一行 `<role>: <内容>`」的三条理由：
//  1. 会话层不认任何 provider 的对话格式（docs/16 §7 的「不做 provider 专有格式持久化」），
//     拼装必须是纯文本，换供应商不用改任何持久化形状；
//  2. 折叠块与原始消息在这里长得一样（折叠留下的本来就是一段话），模型不必区分「这是摘要」；
//  3. 前缀稳定：投影不变则拼出来的文本逐字节不变，上游的前缀缓存才有意义（docs/16 §3）。
//
// 历史里的工具往返（kind=tool 的事件）就这样变成两行 `tool: ...`：
// 模型能看到「我之前查过什么」，而这一轮的往返走原生消息（见 SendMessage 步骤④）。
//
// 投影为空时退化成只发本条输入（正常路径下不会发生：调用方在此之前已经写了 user 事件）。
func buildChatInput(items []aidto.SessionItem, input string) string {
	var b strings.Builder
	for _, it := range items {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(it.Kind)
		if it.Folded {
			role = string(aienums.EventKindCompactSummary)
		}
		if role == "" {
			role = string(aienums.EventKindUser)
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return input
	}
	return strings.TrimRight(b.String(), "\n")
}

// chatTitleFromInput 用首条用户消息派生会话标题（压掉换行，按 rune 截断）。
func chatTitleFromInput(input string) string {
	flat := strings.Join(strings.Fields(input), " ")
	runes := []rune(flat)
	if len(runes) > chatTitleRunes {
		return string(runes[:chatTitleRunes])
	}
	return flat
}

// sentEventItem 把一次追加结果翻成「刚写下的那条事件」的对外形状。
//
// 时间取当下：事件是本次调用刚写进去的，AppendEventResult 只回序号与会话头，
// 这里不为了一个时间字段再回查一次事件日志。
func sentEventItem(res *aidto.AppendEventResult, kind, content string) aidto.SessionEventItem {
	item := aidto.SessionEventItem{
		Kind:          kind,
		SurfaceOp:     string(aienums.SurfaceAppend),
		Content:       content,
		ContentTokens: estimateTokens(content),
		CreateTime:    utils.JSONTime(time.Now()),
	}
	if res != nil {
		item.Seq = res.Seq
	}
	return item
}

// stablePrefix 构造请求的**稳定前缀**（docs/16 §3）：两段 system + 本轮输入。
//
// 抽成函数是为了能被单测直接断言（同一会话两次请求逐字节一致），而不必去跑一次真实的
// 上游调用。**顺序即契约**：
//  1. system —— 常驻规则（ai/prompt 包，编译进二进制，内容恒定）；
//  2. system —— 手册目录（同样恒定：它是从手册文件本身生成的，不手写）；
//  3. user —— 「历史 + 本轮输入」拼成的一条消息。
//
// **目录进前缀、正文按需取**（guide 工具）：手册正文加起来体积可观，而一次对话通常只
// 碰到一两个领域。全量进前缀会让每轮都为所有领域付费，而手册正是会被频繁修订的那类文本
// —— 改一次就作废一次缓存。
//
// 两段的**内容**在同一会话里逐字节不变（历史只追加、规则是常量），provider 侧才能命中
// 前缀缓存。**不要**往这里拼时间戳 / 用户名 / 会话 id / 模型名：那会让每轮都 miss 一次
// 整段前缀，而症状只是账单变贵 —— 没有任何报错，也没有任何页面会显示异常。
//
// 本轮的工具往返（rounds）**不在**前缀里：它每轮都在变，属于尾部。
func stablePrefix(history string) []aidto.ChatMessage {
	// 目录为空时**不占一条消息**：空 system 消息在部分上游会被当成无效消息拒掉，
	// 而在没有手册时（例如裁剪过的部署）它本身就是多余的。
	msgs := []aidto.ChatMessage{{Role: roleSystem, Content: aiprompt.SiteRules()}}
	if catalog := aiprompt.ManualCatalog(); catalog != "" {
		msgs = append(msgs, aidto.ChatMessage{Role: roleSystem, Content: catalog})
	}
	return append(msgs, aidto.ChatMessage{Role: roleUser, Content: history})
}
