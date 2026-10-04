// ai_session_chat.go — 会话页「发消息」：把一次对话落成事件流里的两条记录。
//
// 与 ai_chat.go 的分工：那边是「打一次上游」（provider → HTTP → 文本），
// 这边是「在一条会话里说一句话」（定位会话 → 上下文投影 → 调上游 → 落库）。
// 两者只通过下面这个窄接口相连，会话层不 import 配置层的任何具体类型。
package aiservice

import (
	"context"
	"errors"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/utils"
)

// chatTitleRunes 用首条用户消息派生会话标题时最多取多少个字符。
//
// 会话头要有标题才能进列表，而发消息这条路没有单独的「起个名字」步骤，
// 就取消息开头一段 —— 与前端「首条消息即标题」的习惯一致。
const chatTitleRunes = 60

// ChatPort 会话层需要的「打一次模型」能力，由装配层注入。
//
// 用接口而不是 *Service：会话层与配置层刻意不互相依赖（见 ai_session_service.go 头注释），
// 装配处一行 SetChatPort 把两者接上，两层的编译期依赖保持单向。
type ChatPort interface {
	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
}

// SetChatPort 注入对话能力；未注入时 SendMessage 回 ErrSessionChatUnavailable（不 panic）。
func (s *SessionService) SetChatPort(p ChatPort) { s.chat = p }

// 发消息这条路的错误哨兵（取值同样是 i18n key，口径见 ai_session_service.go 头注释）。
var (
	// ErrSessionChatUnavailable 装配层没有接上对话能力。
	ErrSessionChatUnavailable = errors.New(aienums.ErrSessionChatUnavailable)
	// ErrSessionChatInputRequired 消息正文为空。
	ErrSessionChatInputRequired = errors.New(aienums.ErrSessionChatInputRequired)
	// ErrSessionChatModelRequired 没给供应商或模型。
	ErrSessionChatModelRequired = errors.New(aienums.ErrSessionChatModelRequired)
	// ErrSessionChatEmptyReply 上游回了空文本（不把空回复写成一条空事件）。
	ErrSessionChatEmptyReply = errors.New(aienums.ErrSessionChatEmptyReply)
)

// SendMessage 在一条会话里说一句话：写 user 事件 → 取上下文投影 → 打一次模型 → 写 assistant 事件。
//
// 顺序上有三条刻意的选择：
//  1. 用户输入**先落库**再打模型：上游超时或报错时用户写的东西不丢（事件日志是真源，
//     刷新页面后仍能看到自己发过什么）；
//  2. 上下文从**投影**取而不是从原始事件取：折叠生效之后，模型看到的就是折叠后的视图，
//     与页面上「当前上下文」显示的内容一致 —— 可视化与真实输入不能是两份东西；
//  3. 模型回复落成新的 assistant 事件而不是覆盖任何东西：append-only。
//
// 失败语义：模型调用失败或回复为空时，user 事件已经落库（这是有意的），assistant 事件不写。
func (s *SessionService) SendMessage(ctx context.Context, req aidto.SendMessageReq) (*aidto.SendMessageResult, error) {
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
	items, err := s.project(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// ④ 打一次模型。
	chatRes, err := s.chat.Chat(ctx, &aidto.ChatReq{
		ProviderKey:     providerKey,
		Model:           model,
		Input:           buildChatInput(items, input),
		MaxOutputTokens: req.MaxOutputTokens,
	})
	if err != nil {
		return nil, err
	}
	output := ""
	if chatRes != nil {
		output = strings.TrimSpace(chatRes.Output)
	}
	if output == "" {
		return nil, ErrSessionChatEmptyReply
	}

	// ⑤ 模型回复落成 assistant 事件。
	assistantRes, err := s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID: sessionID,
		Kind:      string(aienums.EventKindAssistant),
		Content:   output,
		UserID:    req.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &aidto.SendMessageResult{
		Session:        assistantRes.Session,
		UserEvent:      sentEventItem(userRes, string(aienums.EventKindUser), input),
		AssistantEvent: sentEventItem(assistantRes, string(aienums.EventKindAssistant), output),
	}, nil
}

// buildChatInput 把当前投影拼成一段纯文本发给上游。
//
// 格式选择「每项一行 `<role>: <内容>`」的三条理由：
//  1. 会话层不认任何 provider 的对话格式（docs/16 §7 的「不做 provider 专有格式持久化」），
//     拼装必须是纯文本，换供应商不用改任何持久化形状；
//  2. 折叠块与原始消息在这里长得一样（折叠留下的本来就是一段话），模型不必区分「这是摘要」；
//  3. 前缀稳定：投影不变则拼出来的文本逐字节不变，上游的前缀缓存才有意义（docs/16 §3）。
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
