package aiservice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/pkg/uispec"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var (
	ErrSessionNotFound = aienums.Err(aienums.ErrSessionNotFound)

	ErrSessionKeyMissing = aienums.Err(aienums.ErrSessionKeyMissing)

	ErrSessionArchived = aienums.Err(aienums.ErrSessionArchived)

	ErrSessionConflict = aienums.Err(aienums.ErrSessionConflict)

	ErrSessionProviderMismatch = aienums.Err(aienums.ErrSessionProviderMismatch)

	ErrEventKindInvalid = aienums.Err(aienums.ErrEventKindInvalid)

	ErrEventContentEmpty = aienums.Err(aienums.ErrEventContentEmpty)

	ErrFoldRangeInvalid = aienums.Err(aienums.ErrFoldRangeInvalid)

	ErrFoldSummaryEmpty = aienums.Err(aienums.ErrFoldSummaryEmpty)
)

const (
	DefaultKeepRecent = 12

	NudgeGrowthTokens = 50000

	MinFoldSegmentTokens = 12000

	summaryRatioPct = 10

	minSummaryTokens = 200
)

type SessionService struct {
	model *aimodel.SessionModel

	chat ChatPort

	callLog CallLogReader

	tools ToolProvider

	toolCalls *ToolCallRecorder
}

func NewSessionService(m *aimodel.SessionModel) *SessionService { return &SessionService{model: m} }

func (s *SessionService) EnsureSession(ctx context.Context, key, providerKey, modelID, title string, userID int64) (*aimodel.AISessionEntity, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	if cur, err := s.model.FindSessionByKey(ctx, key); err != nil {
		return nil, err
	} else if cur != nil {

		if cur.CreateBy != userID {
			return nil, ErrSessionNotFound
		}

		if err := checkSessionBinding(cur, providerKey, modelID); err != nil {
			return nil, err
		}
		return cur, nil
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = key
	}

	e := &aimodel.AISessionEntity{
		SessionKey:  key,
		Title:       title,
		ProviderKey: strings.TrimSpace(providerKey),
		ModelID:     strings.TrimSpace(modelID),
		Status:      int16(aienums.SessionActive),
		NextSeq:     1,
		Version:     1,
		CreateBy:    userID,
		UpdateBy:    userID,
	}
	if err := s.model.CreateSession(ctx, e); err != nil {
		if again, findErr := s.model.FindSessionByKey(ctx, key); findErr == nil && again != nil {
			return again, nil
		}
		return nil, err
	}
	return e, nil
}

func (s *SessionService) ListSessions(ctx context.Context, keyword string, status, page, size int) ([]aidto.Session, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	rows, total, err := s.model.ListSessions(ctx, strings.TrimSpace(keyword), status, (page-1)*size, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]aidto.Session, 0, len(rows))
	for i := range rows {
		out = append(out, sessionDTO(&rows[i]))
	}
	return out, total, nil
}

func (s *SessionService) GetSession(ctx context.Context, id int64) (*aidto.SessionDetail, error) {
	sess, err := s.model.FindSessionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionNotFound
	}
	items, err := s.project(ctx, id)
	if err != nil {
		return nil, err
	}
	detail := &aidto.SessionDetail{
		Session:      sessionDTO(sess),
		Items:        items,
		VisibleCount: len(items),
	}
	for i := range items {
		detail.VisibleTokens += items[i].Tokens
	}
	return detail, nil
}

func (s *SessionService) RenameSession(ctx context.Context, req aidto.RenameSessionReq) (*aidto.Session, error) {
	fields := map[string]any{}
	if t := strings.TrimSpace(req.Title); t != "" {
		fields["title"] = t
	}
	if p := strings.TrimSpace(req.ProviderKey); p != "" {
		fields["provider_key"] = p
	}
	if m := strings.TrimSpace(req.ModelID); m != "" {
		fields["model_id"] = m
	}
	if req.Status >= 0 {
		fields["status"] = req.Status
	}
	if len(fields) == 0 {

		return s.getSessionDTO(ctx, req.ID)
	}
	affected, err := s.model.UpdateSessionWithVersion(ctx, req.ID, req.Version, fields, req.UserID)
	if err != nil {
		return nil, err
	}
	if affected == 0 {

		if _, err = s.getSessionDTO(ctx, req.ID); err != nil {
			return nil, err
		}
		return nil, ErrSessionConflict
	}
	return s.getSessionDTO(ctx, req.ID)
}

func (s *SessionService) ListEvents(ctx context.Context, sessionID int64, page, size int) ([]aidto.SessionEventItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	rows, total, err := s.model.ListEventsDesc(ctx, sessionID, (page-1)*size, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]aidto.SessionEventItem, 0, len(rows))
	for i := range rows {
		out = append(out, aidto.SessionEventItem{
			Seq:            rows[i].Seq,
			Kind:           rows[i].Kind,
			SurfaceOp:      rows[i].SurfaceOp,
			ReplaceFromSeq: rows[i].ReplaceFromSeq,
			ReplaceToSeq:   rows[i].ReplaceToSeq,
			Content:        rows[i].Content,
			ContentTokens:  rows[i].ContentTokens,
			CreateTime:     utils.JSONTime(rows[i].CreateTime),
			Meta:           eventMetaForReader(rows[i].Meta),
		})
	}
	return out, total, nil
}

// eventMetaForReader 交出「可以出接口的」事件 meta。
//
// 为什么需要这一层：meta 会随 GET /api/ai/session/events 原样进 JSON 响应。里面的
// replyError 是我们自己的**诊断字段** —— 新写入只放错误 key，但历史行里存过 cause.Error()
// 原文（URL、回显片段）。直接整份透传，等于让存量数据继续泄漏内部细节。
//
// 处理范围刻意只限这一个诊断字段：tool / reasoning / userText / imageLabels 是业务内容，
// 原样保留；不在这里做通用的 meta 白名单（那会把「哪些键算业务」变成第二处需要维护的真相）。
func eventMetaForReader(meta aimodel.JSONMap) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	if _, ok := out[eventMetaReplyError]; ok {
		out[eventMetaReplyError] = stableReplyErrorKey(out[eventMetaReplyError])
	}
	return out
}

// stableReplyErrorKey 把 meta 里的 replyError 归一成对外可用的错误 key。
//
// 存量行里可能是任意原文，也可能是 `ai.err.xxx|<tail>` 这种「key + 参数原文」的形态
// （facing 的业务 key 正则允许 `(\|.*)?`，整串会被判成合法业务 key）。所以这里**先按
// 第一个 `|` 切**，只取 key 段再校验 —— 否则历史行里的 tail 会跟着整段透传出去，
// 那正是要被堵的口。切出来的 key 不是我们声明的形状就归 ErrInternal，不去猜原文。
func stableReplyErrorKey(v any) string {
	raw, _ := v.(string)
	if raw == "" {
		return aienums.ErrInternal
	}
	key, _, _ := strings.Cut(raw, "|")
	key = strings.TrimSpace(key)
	if key == "" {
		return aienums.ErrInternal
	}
	if facing.IsBusinessKey(key) {
		return key
	}
	return aienums.ErrInternal
}

// FailedRound 是一轮「用户说了话、模型没答上来」的定位事实。
//
// 只给定位（会话 + 序号）与失败 key，不给正文，也不给原文：重试时正文从会话投影里取，
// 这里多带一份就成了第二份真源；原文里有上游 URL 与回显片段，只该进日志。
type FailedRound struct {
	SessionID int64
	Seq       int64
	Reason    string
}

// FailedRounds 列出某会话里外部调用失败的轮次，供补偿/运维重试。
//
// 这是 replyState 标记的对外出口：标记写在用户事件 meta 上，读侧按需扫，
// 不在会话列表里做全表配对（那会把每一次列表查询都变成 N+1）。
func (s *SessionService) FailedRounds(ctx context.Context, sessionID int64, limit int) ([]FailedRound, error) {
	if sessionID <= 0 {
		return nil, ErrSessionNotFound
	}
	rows, err := s.model.ListFailedRounds(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]FailedRound, 0, len(rows))
	for i := range rows {
		// 与 Events 出口同一套归一：存量行里可能是原文，这里也只交稳定 key。
		out = append(out, FailedRound{
			SessionID: rows[i].SessionID,
			Seq:       rows[i].Seq,
			Reason:    stableReplyErrorKey(rows[i].Meta[eventMetaReplyError]),
		})
	}
	return out, nil
}

func (s *SessionService) getSessionDTO(ctx context.Context, id int64) (*aidto.Session, error) {
	sess, err := s.model.FindSessionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionNotFound
	}
	out := sessionDTO(sess)
	return &out, nil
}

func sessionDTO(e *aimodel.AISessionEntity) aidto.Session {
	return aidto.Session{
		ID:            e.ID,
		SessionKey:    e.SessionKey,
		Title:         e.Title,
		ProviderKey:   e.ProviderKey,
		ModelID:       e.ModelID,
		Status:        e.Status,
		EventCount:    e.NextSeq - 1,
		CompactCount:  e.CompactCount,
		ContextTokens: e.ContextTokens,
		Version:       e.Version,
		CreateTime:    utils.JSONTime(e.CreateTime),
		UpdateTime:    utils.JSONTime(e.UpdateTime),
	}
}

func estimateTokens(text string) int64 {
	n := len(strings.TrimSpace(text))
	if n <= 0 {
		return 0
	}
	return int64((n + 2) / 3)
}

func estimateSummaryTokens(segmentTokens int64) int64 {
	v := segmentTokens * summaryRatioPct / 100
	if v < minSummaryTokens {
		return minSummaryTokens
	}
	return v
}

func checkSessionBinding(cur *aimodel.AISessionEntity, providerKey, modelID string) error {
	if pk := strings.TrimSpace(providerKey); pk != "" && pk != cur.ProviderKey {
		return ErrSessionProviderMismatch
	}
	if mid := strings.TrimSpace(modelID); mid != "" && mid != cur.ModelID {
		return ErrSessionProviderMismatch
	}
	return nil
}

func (s *SessionService) AppendEvent(ctx context.Context, req aidto.AppendEventReq) (*aidto.AppendEventResult, error) {

	if req.UserID <= 0 {
		return nil, ErrUserRequired
	}
	kind := strings.TrimSpace(req.Kind)
	if !aienums.IsValidEventKind(aienums.EventKind(kind)) {
		return nil, ErrEventKindInvalid
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrEventContentEmpty
	}
	if req.SessionID <= 0 && strings.TrimSpace(req.SessionKey) == "" {
		return nil, ErrSessionKeyMissing
	}

	tokens := req.Tokens
	if tokens <= 0 {
		tokens = estimateTokens(content)
	}

	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.appendInTx(ctx, req, kind, content, tokens)
		if err == nil {
			return out, nil
		}
		if attempt == 0 && isDuplicateKeyErr(err) {
			continue
		}
		return nil, err
	}
	return nil, ErrSessionConflict
}

func (s *SessionService) appendInTx(ctx context.Context, req aidto.AppendEventReq, kind, content string, tokens int64) (*aidto.AppendEventResult, error) {
	var (
		seq       int64
		sessionID int64
	)
	err := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		sess, err := s.ensureSessionTx(ctx, tx, req)
		if err != nil {
			return err
		}

		if sess.Status != int16(aienums.SessionActive) {
			return ErrSessionArchived
		}
		sessionID = sess.ID

		got, found, err := s.model.NextSeqTx(tx, sess.ID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		seq = got
		if err := s.model.InsertEventTx(tx, &aimodel.AIEventEntity{
			SessionID:     sess.ID,
			Seq:           got,
			Kind:          kind,
			SurfaceOp:     string(aienums.SurfaceAppend),
			Content:       content,
			ContentTokens: tokens,

			UserID: req.UserID,

			ProviderKey: req.ProviderKey,
			ModelID:     req.ModelID,
			Meta:        aimodel.JSONMap(req.Meta),
		}); err != nil {
			return err
		}
		return s.model.BumpAfterAppendTx(tx, sess.ID, got, tokens, req.UserID)
	})
	if err != nil {
		return nil, err
	}

	head, err := s.getSessionDTO(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &aidto.AppendEventResult{Seq: seq, Session: *head}, nil
}

func (s *SessionService) ensureSessionTx(ctx context.Context, tx *gorm.DB, req aidto.AppendEventReq) (*aimodel.AISessionEntity, error) {
	// 两条入口（按 id / 按 key）都要：①行锁 ②归属校验。
	//
	// 缺一不可的原因不同：
	//   · 不加锁 —— 追加靠 NextSeq 递增定序，两个并发追加会读到同一个 seq；
	//   · 不校验归属 —— key 分支是真正的绕过口：FAB 的 key 形如 admin-fab:<uid>、
	//     会话 key 又是「客户端给什么就存什么」，猜得到就能往别人的会话里追加事件。
	//
	// 归属口径与 EnsureSession / RecentDialogue 一致：会话属于创建人。对外统一回
	// 「会话不存在」，不泄漏「这条会话存在但不是你的」。
	if req.SessionID > 0 {
		sess, err := s.model.LockSessionByIDTx(tx, req.SessionID)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, ErrSessionNotFound
		}
		if sess.CreateBy != req.UserID {
			return nil, ErrSessionNotFound
		}
		return sess, nil
	}

	key := strings.TrimSpace(req.SessionKey)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	cur, err := s.model.LockSessionByKeyTx(tx, key)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		if cur.CreateBy != req.UserID {
			return nil, ErrSessionNotFound
		}
		if err := checkSessionBinding(cur, req.ProviderKey, req.ModelID); err != nil {
			return nil, err
		}
		return cur, nil
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = key
	}

	e := &aimodel.AISessionEntity{
		SessionKey:  key,
		Title:       title,
		ProviderKey: strings.TrimSpace(req.ProviderKey),
		ModelID:     strings.TrimSpace(req.ModelID),
		Status:      int16(aienums.SessionActive),
		NextSeq:     1,
		Version:     1,
		CreateBy:    req.UserID,
		UpdateBy:    req.UserID,
	}
	if err := s.model.CreateSessionTx(tx, e); err != nil {
		return nil, err
	}
	return e, nil
}

const sessionCallPreview = 5

type CallLogReader interface {
	RecentCallsBySession(ctx context.Context, sessionIDs []int64, perSession int) ([]aimodel.SessionCallLogRow, error)
}

func (s *SessionService) SetCallLogReader(r CallLogReader) { s.callLog = r }

func (s *SessionService) SessionCallsOf(ctx context.Context, sessionIDs []int64) (map[int64]aidto.SessionCalls, error) {
	out := make(map[int64]aidto.SessionCalls, len(sessionIDs))
	if s.callLog == nil || len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := s.callLog.RecentCallsBySession(ctx, sessionIDs, sessionCallPreview)
	if err != nil {
		return out, err
	}
	for i := range rows {
		row := rows[i]
		item := out[row.SessionID]
		item.Total = row.Total
		item.Rows = append(item.Rows, callRowOf(row))
		out[row.SessionID] = item
	}
	return out, nil
}

func callRowOf(row aimodel.SessionCallLogRow) aidto.SessionCallRow {
	ok := row.Status == string(aienums.CallStatusOK)
	out := aidto.SessionCallRow{
		At:          utils.NewJSONTime(row.CreateTime),
		ProviderKey: row.ProviderKey,
		ModelID:     row.ModelID,
		LatencyMs:   row.LatencyMs,
		Tokens:      row.TotalTokens,
		OK:          ok,
	}
	if !ok {
		// 只给错误 key，不在这里取词：文案归模板用全局 t 取，
		// 缺词时由模板给的兜底句兜住（换语言要重算的成品句不进业务数据）。
		key := strings.TrimSpace(row.ErrorKey)
		if key == "" {
			key = aienums.ErrInternal
		}
		out.ErrorKey = key
	}
	return out
}

const chatTitleRunes = 60

const maxToolRounds = 10

const (
	toolPhaseCall   = "call"
	toolPhaseResult = "result"
)

type ChatPort interface {
	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)

	ChatStream(ctx context.Context, req *aidto.ChatReq, onDelta func(StreamDelta)) (*aidto.ChatResult, error)
}

type StreamEvent struct {
	Kind string

	Text string
}

const (
	streamEventReasoning = "reasoning"

	streamEventText = "text"

	streamEventTool = "tool"
)

type ToolRunResult struct {
	Text string

	Status aienums.ToolCallStatus

	Data any
}

type ToolProvider interface {
	Specs() []aidto.ToolSpec

	Run(ctx context.Context, userID int64, name, arguments string) (ToolRunResult, error)
}

func (s *SessionService) SetChatPort(p ChatPort) { s.chat = p }

func (s *SessionService) SetToolProvider(p ToolProvider) { s.tools = p }

var (
	ErrUserRequired = aienums.Err(aienums.ErrUserRequired)

	ErrSessionChatUnavailable = aienums.Err(aienums.ErrSessionChatUnavailable)

	ErrSessionChatInputRequired = aienums.Err(aienums.ErrSessionChatInputRequired)

	ErrSessionChatModelRequired = aienums.Err(aienums.ErrSessionChatModelRequired)

	ErrSessionChatEmptyReply = aienums.Err(aienums.ErrSessionChatEmptyReply)

	ErrSessionToolRoundsExceeded = aienums.Err(aienums.ErrSessionToolRoundsExceeded)
)

func (s *SessionService) SendMessage(ctx context.Context, req aidto.SendMessageReq) (*aidto.SendMessageResult, error) {
	return s.sendMessage(ctx, req, nil)
}

func (s *SessionService) SendMessageStream(ctx context.Context, req aidto.SendMessageReq, emit func(StreamEvent)) (*aidto.SendMessageResult, error) {
	return s.sendMessage(ctx, req, emit)
}

func (s *SessionService) sendMessage(ctx context.Context, req aidto.SendMessageReq, emit func(StreamEvent)) (*aidto.SendMessageResult, error) {

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

	userRes, err := s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID:   sessionID,
		Kind:        string(aienums.EventKindUser),
		Content:     input,
		Meta:        userEventMeta(&req),
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     model,
	})
	if err != nil {
		return nil, err
	}
	sessionID = userRes.Session.ID

	items, err := s.project(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	history := buildChatInput(items, input)

	rounds := make([]aidto.ChatMessage, 0, 8)
	toolEvents := make([]aidto.SessionEventItem, 0, 4)

	var reasonings []string
	specs := s.toolSpecs()
	for round := 0; round < maxToolRounds; round++ {

		var roundImages []string
		if round == 0 {
			roundImages = req.Images
		}
		msgs := stablePrefix(history, roundImages)
		msgs = append(msgs, rounds...)

		roundSpecs := specs
		if round == maxToolRounds-1 {
			roundSpecs = nil
		}

		chatReq := &aidto.ChatReq{
			ProviderKey:     providerKey,
			Model:           model,
			Messages:        msgs,
			Tools:           roundSpecs,
			MaxOutputTokens: req.MaxOutputTokens,
			SessionID:       sessionID,
			UserID:          req.UserID,
		}

		chatRes, err := s.chatWithEmit(ctx, chatReq, emit)
		if err != nil {
			s.markRoundFailed(ctx, sessionID, userRes, req, err)
			return nil, err
		}
		if chatRes == nil {
			s.markRoundFailed(ctx, sessionID, userRes, req, ErrSessionChatEmptyReply)
			return nil, ErrSessionChatEmptyReply
		}
		if r := strings.TrimSpace(chatRes.Reasoning); r != "" {
			reasonings = append(reasonings, r)
		}

		if len(chatRes.ToolCalls) == 0 {
			output := strings.TrimSpace(chatRes.Output)
			think := strings.Join(reasonings, "\n\n")
			if output == "" {

				if strings.TrimSpace(think) == "" {
					s.markRoundFailed(ctx, sessionID, userRes, req, ErrSessionChatEmptyReply)
					return nil, ErrSessionChatEmptyReply
				}

				return s.finishReply(ctx, sessionID, req, providerKey, model, think, userRes, input,
					toolEvents, "")
			}
			return s.finishReply(ctx, sessionID, req, providerKey, model, output, userRes, input,
				toolEvents, think)
		}

		for _, call := range chatRes.ToolCalls {

			emitToolEvent(emit, call)
			callRes, err := s.appendToolEvent(ctx, sessionID, req, providerKey, model, call, toolPhaseCall, "", nil)
			if err != nil {
				return nil, err
			}
			runRes := s.runTool(ctx, sessionID, req.UserID, call)
			resultRes, err := s.appendToolEvent(ctx, sessionID, req, providerKey, model, call, toolPhaseResult, runRes.Text, runRes.Data)
			if err != nil {
				return nil, err
			}

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

	return nil, ErrSessionToolRoundsExceeded
}

// markRoundFailed 把「这一轮外部调用没成功」记到本轮用户事件的 meta 上。
//
// 为什么写在这些点上而不是包一层事务：外部 HTTP 不能持 DB 事务（连接会被一次几秒到
// 几十秒的网络调用占住），所以这里只能是「另起一条短事务补记」——它本身也可能失败，
// 因此失败只记日志、绝不覆盖真正的业务错误：用户该看到的是「模型没答出来」，
// 不是「补记失败」。
func (s *SessionService) markRoundFailed(ctx context.Context, sessionID int64, res *aidto.AppendEventResult, req aidto.SendMessageReq, cause error) {
	if res == nil || s.model == nil {
		return
	}
	// meta 只放**稳定的错误事实**（错误 key），绝不放 cause.Error() 原文。
	//
	// 原因：ai_event.meta 会经由 GET /api/ai/session/events 原样出现在 JSON 响应里
	// （SessionEventItem.Meta）。上游原文里可能有 URL、模型名、甚至回显的密钥碎片，
	// 直接写进去就是把内部细节当接口返回。原文只进日志。
	if err := s.model.MarkEventMeta(ctx, sessionID, res.Seq, map[string]any{
		eventMetaReplyState: replyStateFailed,
		eventMetaReplyError: replyErrorKeyOf(cause),
	}); err != nil {
		logger.SceneCtx("ai", ctx).
			With("session_id", sessionID).
			With("seq", res.Seq).
			Error(err, "补记本轮失败状态失败（会话仍可用，只是少了重放线索）")
	}
	// 原始原因只进日志：排障要靠它，接口不靠它。
	if cause != nil {
		logger.SceneCtx("ai", ctx).
			With("session_id", sessionID).
			With("seq", res.Seq).
			Error(cause, "本轮外部调用失败")
	}
}

func (s *SessionService) finishReply(
	ctx context.Context,
	sessionID int64,
	req aidto.SendMessageReq,
	providerKey, model, output string,
	userRes *aidto.AppendEventResult,
	input string,
	toolEvents []aidto.SessionEventItem,
	reasoning string,
) (*aidto.SendMessageResult, error) {

	var meta map[string]any
	if strings.TrimSpace(reasoning) != "" {
		meta = map[string]any{eventMetaReasoning: reasoning}
	}
	assistantRes, err := s.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID:   sessionID,
		Kind:        string(aienums.EventKindAssistant),
		Content:     output,
		Meta:        meta,
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

func (s *SessionService) chatWithEmit(ctx context.Context, req *aidto.ChatReq, emit func(StreamEvent)) (*aidto.ChatResult, error) {
	if emit == nil {
		return s.chat.Chat(ctx, req)
	}
	return s.chat.ChatStream(ctx, req, func(d StreamDelta) {
		if d.Reasoning != "" {
			emit(StreamEvent{Kind: streamEventReasoning, Text: d.Reasoning})
		}
		if d.Text != "" {
			emit(StreamEvent{Kind: streamEventText, Text: d.Text})
		}
	})
}

func emitToolEvent(emit func(StreamEvent), call aidto.ToolCall) {
	if emit == nil {
		return
	}
	text := toolCallText(call)
	if strings.TrimSpace(text) == "" {
		return
	}
	emit(StreamEvent{Kind: streamEventTool, Text: text})
}

// eventMetaReplyState 标记「这一轮回复走到了什么状态」。
//
// 为什么需要它：一次 sendMessage = 先写用户事件（短事务）→ 外部 HTTP（不持事务）→
// 再写助手事件（短事务）。中间那段网络调用失败时，用户事件已经落库、助手事件没有，
// 会话里就留下一条「说了话但没人回」的孤证 —— 事后无法区分「模型没答」和「我们没写」。
// 把它记在 meta 里，重放/补偿能只挑这些轮次重试，不必重放整个会话。
const eventMetaReplyState = "replyState"

// replyStateFailed 表示本轮外部调用失败、助手事件缺席。
const replyStateFailed = "failed"

// eventMetaReplyError 是补记的失败原因 —— **只存错误 key，不存原文**。
//
// 它落在 ai_event.meta 里，而 GET /api/ai/session/events 会把 meta 原样交给调用方
// （SessionEventItem.Meta）。上游原文（URL、模型名、回显片段）一旦写进来就从接口泄出去，
// 所以这里只留稳定的 key：能据此判断「哪一类失败」，也说不出更多内部细节。
const eventMetaReplyError = "replyError"

// replyErrorKeyOf 把本轮失败归成稳定的错误 key。
//
// 已声明的业务错误给它的 key（facing.FactOf 认得出 ai.err.* 家族）；其它一律归 ErrInternal，
// 不把原文带出去。原文在 markRoundFailed 里另记一条日志。
func replyErrorKeyOf(cause error) string {
	if cause == nil {
		return aienums.ErrInternal
	}
	// FactOf 的签名是 (Fact, bool)：第二个返回值才是「这是不是一个可识别的错误事实」。
	// Fact.Key 在业务 key 形态下可能是整串（含 `|tail`），写入侧也只落 key 段：
	// 读侧按 `|` 切的那条规则与这里保持一致，两边不会对不上。
	if fact, ok := facing.FactOf(cause); ok && fact.Key != "" {
		key, _, _ := strings.Cut(fact.Key, "|")
		if key = strings.TrimSpace(key); key != "" {
			return key
		}
	}
	return aienums.ErrInternal
}

const eventMetaReasoning = "reasoning"

const eventMetaUserText = "userText"

const eventMetaImageLabels = "imageLabels"

func userEventMeta(req *aidto.SendMessageReq) map[string]any {
	meta := map[string]any{}
	if text := strings.TrimSpace(req.UserText); text != "" && text != strings.TrimSpace(req.Input) {
		meta[eventMetaUserText] = text
	}
	if len(req.ImageLabels) > 0 {
		meta[eventMetaImageLabels] = req.ImageLabels
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

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

	meta := map[string]any{
		"phase":     phase,
		"callId":    call.ID,
		"tool":      call.Name,
		"arguments": call.Arguments,
	}

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

func toolCallText(call aidto.ToolCall) string {
	args := strings.TrimSpace(call.Arguments)
	if args == "" || args == "{}" {
		return fmt.Sprintf("调用 %s", call.Name)
	}
	return fmt.Sprintf("调用 %s：%s", call.Name, args)
}

func (s *SessionService) toolSpecs() []aidto.ToolSpec {
	if s.tools == nil {
		return nil
	}
	return s.tools.Specs()
}

// ToolFailureFact 是一次工具失败的结构化事实。
//
// 为什么不是一句中文：同一个失败要同时被三方消费 ——
//
//	· 落库（审计/重放）：要能按 key 聚合、按 status 分档，中文句子做不到；
//	· 模型上下文：要能区分「没权限」「参数错」「服务端炸了」，界面文案把三者抹平；
//	· 界面：要按请求语言显示，只有拿 key 才能取对词。
//
// 三者共用一句中文，必然有至少一处是错的。
type ToolFailureFact struct {
	Key    string
	Tool   string
	Status string
}

// Summarize 是落库用的短摘要：结构化、可聚合，不含界面文案。
func (f ToolFailureFact) Summarize() string {
	return fmt.Sprintf("tool=%s status=%s error=%s", f.Tool, f.Status, f.Key)
}

// ModelText 是喂给模型的那一行：结构化到模型能据此决定下一步（换参数 / 换工具 / 告知用户），
// 但不含任何界面成品句。它会被写进事件正文，因此也必须是可复现的事实而不是译文。
func (f ToolFailureFact) ModelText() string {
	return fmt.Sprintf("tool %s failed: status=%s, error=%s", f.Tool, f.Status, f.Key)
}

func (s *SessionService) runTool(ctx context.Context, sessionID, userID int64, call aidto.ToolCall) ToolRunResult {
	start := time.Now()

	// fail 记录一次失败的调用。
	//
	// 三件事分开记，别混成一句话：
	//   · 落库的 ResultSummary 存**结构化失败事实**（key + 工具名），不是界面中文 ——
	//     中文是给人看的成品句，换语言要重算，存进事件日志就成了第二份真源；
	//   · 返回给模型的 Text 同样是结构化事实，不是「工具执行失败」这类界面句子 ——
	//     模型要据此判断「是权限不够还是参数错了」，给它一句界面文案等于什么都没说；
	//   · 界面怎么显示由 JSON 出口按 key 取词，那边才知道当前语言。
	fail := func(status aienums.ToolCallStatus) ToolRunResult {
		key := ToolErrorKeyOf(status)
		fact := ToolFailureFact{Key: key, Tool: call.Name, Status: string(status)}
		e := NewToolCallEntry(sessionID, userID, call.Name, call.Arguments, time.Since(start))
		e.Status = string(status)
		e.ErrorKey = key
		e.ResultSummary = fact.Summarize()
		e.ResultLen = int64(len([]rune(fact.Summarize())))
		s.toolCalls.Record(ctx, e)
		return ToolRunResult{Text: fact.ModelText(), Status: status}
	}
	// 没有工具提供者时不能假装工具跑了：这是配置/装配缺失，是失败而不是空结果。
	if s.tools == nil {
		return fail(aienums.ToolCallStatusFailed)
	}

	res, err := s.tools.Run(ctx, userID, call.Name, call.Arguments)
	if err != nil {
		logger.Scene("ai").With("tool", call.Name).Error(err, "工具执行失败")
		return fail(aienums.ToolCallStatusFailed)
	}

	status := res.Status
	if !aienums.IsValidToolCallStatus(status) {

		status = aienums.ToolCallStatusFailed
	}

	text, truncated := PruneToolResult(res.Text)

	if strings.TrimSpace(text) == "" {
		logger.Scene("ai").With("tool", call.Name).Warn("工具返回了空结果")
		return fail(status)
	}

	var data any
	if spec, ok := res.Data.(*uispec.Spec); ok {
		views := s.renderSpec(ctx, sessionID, userID, spec)
		if len(views) > 0 {
			data = views
		} else if len(spec.Blocks) > 0 {

			// 这句是给模型看的系统提示，不是界面文案：它必须说明「图表渲染了但数据源是空的」
			// 这个具体事实，模型才能决定换查询还是告诉用户没数据。写死中文在这里是可接受的
			// —— 它只进模型上下文、不进响应体、也不落库成展示字段；换成 key 反而要模型先学一套词表。
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

func chatTitleFromInput(input string) string {
	flat := strings.Join(strings.Fields(input), " ")
	runes := []rune(flat)
	if len(runes) > chatTitleRunes {
		return string(runes[:chatTitleRunes])
	}
	return flat
}

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

func stablePrefix(history string, images []string) []aidto.ChatMessage {

	msgs := []aidto.ChatMessage{{Role: roleSystem, Content: aiprompt.SiteRules()}}
	if catalog := aiprompt.ManualCatalog(); catalog != "" {
		msgs = append(msgs, aidto.ChatMessage{Role: roleSystem, Content: catalog})
	}

	return append(msgs, aidto.ChatMessage{Role: roleUser, Content: history, Images: images})
}

const (
	FoldReasonOK = "ok"

	FoldReasonNoSegment = "no_segment"

	FoldReasonShortSegment = "short_segment"

	FoldReasonNotPositive = "not_positive"
)

const excerptLimit = 4000

func (s *SessionService) FoldPlan(ctx context.Context, req aidto.FoldPlanReq) (*aidto.FoldPlanResult, error) {
	if req.SessionID <= 0 {
		return nil, ErrSessionNotFound
	}
	sess, err := s.model.FindSessionByID(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrSessionNotFound
	}
	items, err := s.project(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	keep := req.KeepRecent
	if keep <= 0 {
		keep = DefaultKeepRecent
	}
	out := &aidto.FoldPlanResult{SessionID: req.SessionID}
	if len(items) <= keep {
		out.Worthwhile = false
		out.Reason = FoldReasonNoSegment
		return out, nil
	}

	segment := items[:len(items)-keep]
	var visibleTokens, segmentTokens int64
	for i := range items {
		visibleTokens += items[i].Tokens
	}
	for i := range segment {
		segmentTokens += segment[i].Tokens
	}
	out.FromSeq = segment[0].Seq
	out.ToSeq = segment[len(segment)-1].Seq
	out.SegmentTokens = segmentTokens
	out.Excerpt = joinExcerpt(segment)

	summaryEst := estimateSummaryTokens(segmentTokens)
	out.EstimatedSave = segmentTokens - summaryEst
	out.EstimatedAfter = visibleTokens - out.EstimatedSave
	switch {
	case segmentTokens < MinFoldSegmentTokens:
		out.Worthwhile = false
		out.Reason = FoldReasonShortSegment
	case out.EstimatedSave <= 0:
		out.Worthwhile = false
		out.Reason = FoldReasonNotPositive
	default:
		out.Worthwhile = true
		out.Reason = FoldReasonOK
	}
	return out, nil
}

func (s *SessionService) Fold(ctx context.Context, req aidto.FoldReq) (*aidto.FoldResult, error) {
	if req.SessionID <= 0 {
		return nil, ErrSessionNotFound
	}
	if req.FromSeq <= 0 || req.ToSeq < req.FromSeq {
		return nil, ErrFoldRangeInvalid
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		return nil, ErrFoldSummaryEmpty
	}
	summaryTokens := req.SummaryTokens
	if summaryTokens <= 0 {
		summaryTokens = estimateTokens(summary)
	}
	marker := fmt.Sprintf("fold %d..%d", req.FromSeq, req.ToSeq)

	var (
		res       aidto.FoldResult
		sessionID = req.SessionID
	)
	err := s.model.Transaction(ctx, func(tx *gorm.DB) error {
		sess, err := s.model.FindSessionByIDTx(tx, sessionID)
		if err != nil {
			return err
		}
		if sess == nil {
			return ErrSessionNotFound
		}
		if sess.Status != int16(aienums.SessionActive) {
			return ErrSessionArchived
		}

		events, err := s.model.ListEventsFromTx(tx, sessionID, 1)
		if err != nil {
			return err
		}
		if !foldRangeValid(events, req.FromSeq, req.ToSeq) {
			return ErrFoldRangeInvalid
		}

		start, found, err := s.model.NextSeqTx(tx, sessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		sumSeq, found, err := s.model.NextSeqTx(tx, sessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		end, found, err := s.model.NextSeqTx(tx, sessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		res.StartSeq, res.SummarySeq, res.EndSeq = start, sumSeq, end

		rows := []aimodel.AIEventEntity{
			{SessionID: sessionID, Seq: start, Kind: string(aienums.EventKindCompactStart), SurfaceOp: string(aienums.SurfaceAppend), Content: marker},
			{
				SessionID: sessionID, Seq: sumSeq, Kind: string(aienums.EventKindCompactSummary),
				SurfaceOp: string(aienums.SurfaceReplace), ReplaceFromSeq: req.FromSeq, ReplaceToSeq: req.ToSeq,
				Content: summary, ContentTokens: summaryTokens,
			},
			{SessionID: sessionID, Seq: end, Kind: string(aienums.EventKindCompactEnd), SurfaceOp: string(aienums.SurfaceAppend), Content: marker},
		}
		for i := range rows {
			if err := s.model.InsertEventTx(tx, &rows[i]); err != nil {
				return err
			}
		}

		after, err := s.model.ListEventsFromTx(tx, sessionID, 1)
		if err != nil {
			return err
		}
		tokens := projectedTokens(after)
		if err := s.model.RecalcAfterCompactTx(tx, sessionID, tokens, req.UserID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	head, err := s.getSessionDTO(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	res.Session = *head
	return &res, nil
}

func foldRangeValid(events []aimodel.AIEventEntity, from, to int64) bool {
	var (
		hasFrom bool
		hasTo   bool
	)
	for i := range events {
		e := events[i]
		if e.Seq >= from && e.Seq <= to {

			if e.SurfaceOp == string(aienums.SurfaceReplace) {
				return false
			}
			if e.Kind == string(aienums.EventKindCompactStart) || e.Kind == string(aienums.EventKindCompactEnd) {
				return false
			}
		}
		if e.Seq == from {
			hasFrom = true
		}
		if e.Seq == to {
			hasTo = true
		}
	}
	if !hasFrom || !hasTo {
		return false
	}

	for i := range events {
		e := events[i]
		if e.SurfaceOp != string(aienums.SurfaceReplace) {
			continue
		}
		if (from >= e.ReplaceFromSeq && from <= e.ReplaceToSeq) || (to >= e.ReplaceFromSeq && to <= e.ReplaceToSeq) {
			return false
		}
	}
	return true
}

func projectedTokens(events []aimodel.AIEventEntity) int64 {
	return projectTokens(projectEvents(events))
}

func joinExcerpt(items []aidto.SessionItem) string {
	var b strings.Builder
	for i := range items {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(items[i].Content)
	}
	runes := []rune(b.String())
	if len(runes) > excerptLimit {
		return string(runes[:excerptLimit])
	}
	return string(runes)
}

const dialogueTurnLimit = 12

func (s *SessionService) RecentDialogue(ctx context.Context, key string, limit int, userID int64) ([]aidto.DialogueTurn, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	if limit < 1 || limit > dialogueTurnLimit {
		limit = dialogueTurnLimit
	}
	sess, err := s.model.FindSessionByKey(ctx, key)
	if err != nil {
		return nil, err
	}

	if sess == nil || sess.CreateBy != userID {
		return nil, nil
	}

	rows, _, err := s.model.ListEventsDesc(ctx, sess.ID, 0, limit*4)
	if err != nil {
		return nil, err
	}

	turns := make([]aidto.DialogueTurn, 0, limit*2)

	for i := len(rows) - 1; i >= 0; i-- {
		kind := rows[i].Kind
		if kind != string(aienums.EventKindUser) && kind != string(aienums.EventKindAssistant) {
			continue
		}
		text := strings.TrimSpace(rows[i].Content)
		if text == "" {
			continue
		}
		turn := aidto.DialogueTurn{Role: "user", Text: text}
		if kind == string(aienums.EventKindAssistant) {
			turn.Role = "assistant"
			if v, ok := rows[i].Meta[eventMetaReasoning].(string); ok {
				turn.Reasoning = v
			}
		} else if labels := imageLabelsOf(rows[i].Meta); len(labels) > 0 {
			turn.ImageLabels = labels
		}
		if v, ok := rows[i].Meta[eventMetaUserText].(string); ok && strings.TrimSpace(v) != "" {

			turn.Text = strings.TrimSpace(v)
		}
		turns = append(turns, turn)
	}
	return tailDialogueTurns(turns, limit), nil
}

func imageLabelsOf(meta map[string]any) []string {
	raw, ok := meta[eventMetaImageLabels].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func tailDialogueTurns(turns []aidto.DialogueTurn, limit int) []aidto.DialogueTurn {
	if len(turns) <= limit {
		return turns
	}
	seen := 0
	start := 0
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role != "user" {
			continue
		}
		seen++
		if seen > limit {
			break
		}

		start = i
	}
	return turns[start:]
}

func (s *SessionService) project(ctx context.Context, sessionID int64) ([]aidto.SessionItem, error) {
	events, err := s.model.ListEventsFrom(ctx, sessionID, 1, 0)
	if err != nil {
		return nil, err
	}
	return projectEvents(events), nil
}

type foldSpan struct{ from, to int64 }

func projectEvents(events []aimodel.AIEventEntity) []aidto.SessionItem {
	spans := make([]foldSpan, 0, 4)
	for i := range events {
		if events[i].SurfaceOp == string(aienums.SurfaceReplace) {
			spans = append(spans, foldSpan{events[i].ReplaceFromSeq, events[i].ReplaceToSeq})
		}
	}
	foldedAway := func(seq int64) bool {
		for _, sp := range spans {
			if seq >= sp.from && seq <= sp.to {
				return true
			}
		}
		return false
	}

	type placed struct {
		order int64
		item  aidto.SessionItem
	}
	placedItems := make([]placed, 0, len(events))
	for i := range events {
		e := events[i]
		switch {
		case e.SurfaceOp == string(aienums.SurfaceReplace):

			placedItems = append(placedItems, placed{order: e.ReplaceFromSeq, item: sessionItemOf(e, true)})
		case foldedAway(e.Seq):

		case e.Kind == string(aienums.EventKindCompactStart), e.Kind == string(aienums.EventKindCompactEnd):

		default:
			placedItems = append(placedItems, placed{order: e.Seq, item: sessionItemOf(e, false)})
		}
	}
	slices.SortStableFunc(placedItems, func(a, b placed) int { return cmp.Compare(a.order, b.order) })

	out := make([]aidto.SessionItem, 0, len(placedItems))
	for i := range placedItems {
		out = append(out, placedItems[i].item)
	}
	return out
}

func sessionItemOf(e aimodel.AIEventEntity, folded bool) aidto.SessionItem {
	item := aidto.SessionItem{
		Seq:     e.Seq,
		Kind:    e.Kind,
		Content: e.Content,
		Tokens:  e.ContentTokens,
		Folded:  folded,
	}
	if folded {
		item.FoldedFrom = e.ReplaceFromSeq
		item.FoldedTo = e.ReplaceToSeq
	}
	return item
}

func projectTokens(items []aidto.SessionItem) int64 {
	var total int64
	for i := range items {
		total += items[i].Tokens
	}
	return total
}

const renderMetaLimit = 64 << 10

type uiSpecResolver struct {
	provider ToolProvider
	userID   int64
}

func (r *uiSpecResolver) Resolve(ctx context.Context, source string, params map[string]string, limit int) (any, error) {
	if r.provider == nil {
		return nil, errors.New("没有可用的工具能力")
	}
	body := make(map[string]any, len(params)+1)
	for k, v := range params {
		body[k] = v
	}
	if limit > 0 {
		body["limit"] = limit
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	res, err := r.provider.Run(ctx, r.userID, source, string(raw))
	if err != nil {
		return nil, err
	}

	if res.Status != aienums.ToolCallStatusOK {
		return nil, errors.New(res.Text)
	}
	if res.Data == nil {
		return nil, fmt.Errorf("数据源 %s 没有可渲染的结构", source)
	}
	return res.Data, nil
}

func (s *SessionService) renderSpec(ctx context.Context, sessionID, userID int64, spec *uispec.Spec) []uispec.View {
	views, notes := uispec.Execute(ctx, spec, &uiSpecResolver{provider: s.tools, userID: userID})
	for _, n := range notes {
		if n.Kind == uispec.NoteOK {
			continue
		}

		logger.Scene("ai").With("session", sessionID).With("source", n.Source).
			With("kind", n.Kind).Warn("展示积木未渲染：" + n.Msg)
	}
	return views
}

const (
	sessionTrendDaysDefault = 30

	sessionTrendDaysMax = 90
)

func (s *SessionService) ListSessionsFiltered(ctx context.Context, f aidto.SessionQuery, page, size int) ([]aidto.Session, int64, error) {
	page, size = normalizeSessionPage(page, size)
	rows, total, err := s.model.ListSessionsFiltered(ctx, sessionQueryOf(f), (page-1)*size, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]aidto.Session, 0, len(rows))
	for i := range rows {
		out = append(out, sessionDTO(&rows[i]))
	}
	return out, total, nil
}

func (s *SessionService) SessionUsageOf(ctx context.Context, f aidto.SessionQuery) (aidto.SessionUsage, error) {
	raw, err := s.model.SessionUsageOf(ctx, sessionQueryOf(f))
	if err != nil {
		return aidto.SessionUsage{}, err
	}
	out := aidto.SessionUsage{
		Sessions: raw.Sessions,
		Events:   raw.Events,
		Tokens:   raw.Tokens,
		Compacts: raw.Compacts,
	}
	if raw.Sessions > 0 {

		out.AvgTokens = raw.Tokens / raw.Sessions
	}

	out.HitRateReady = raw.CachedInputTokens > 0
	out.HitRatePct = percentage(raw.CachedTokens, raw.CachedInputTokens)
	out.CachedCalls = raw.CachedCalls
	out.UsageReportedCalls = raw.UsageReportedCalls

	out.CompactCostPct = percentage(raw.SummaryTokens, raw.Tokens)
	return out, nil
}

func percentage(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func (s *SessionService) SessionFilterOptions(ctx context.Context) (aidto.SessionFilterOptions, error) {
	facets, err := s.model.SessionFacetsOf(ctx)
	if err != nil {
		return aidto.SessionFilterOptions{}, err
	}

	return aidto.SessionFilterOptions{
		Providers: append([]string{}, facets.Providers...),
		Models:    append([]string{}, facets.Models...),
		Creators:  append([]int64{}, facets.Creators...),
	}, nil
}

// trendSeriesMax 是「最多画几条线」的数据侧上限：超过就并成一条「其余」。
// 这是事实侧的收敛（否则一个面板要塞几十条序列），不是画布参数。
// 视图宽高、内边距、配色槽位都属于前端，这里不再有。
const trendSeriesMax = 8

func (s *SessionService) SessionTrend(ctx context.Context, f aidto.SessionQuery, trendDays int) (aidto.SessionTrend, error) {
	q := sessionQueryOf(f)
	span := trendDays
	if span < 1 || span > sessionTrendDaysMax {
		span = sessionTrendDaysDefault
	}

	// 结束日默认取 UTC 的今天，和桶键同一时区。用本地日期会让图表最后一天的
	// 桶和数据库切出来的桶错开一天（库里按 UTC 切，界面按本地取）。
	end := dayStartUTC(time.Now())
	if q.To != nil {
		end = *q.To
	}
	start := end.AddDate(0, 0, -(span - 1))
	if q.From != nil && q.From.After(start) {
		start = *q.From
	}
	if start.After(end) {

		start = end
	}

	rows, err := s.model.SessionTokenTrendBySeries(ctx, q, start, "day")
	if err != nil {
		return aidto.SessionTrend{}, err
	}

	days := make([]time.Time, 0, span)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		days = append(days, cur)
	}
	dayIndex := make(map[string]int, len(days))
	for i := range days {
		dayIndex[days[i].UTC().Format("2006-01-02")] = i
	}

	type bucket struct {
		tokens []int64
		total  int64

		count int
	}
	groups := make(map[string]*bucket)
	order := make([]string, 0)
	for i := range rows {
		key := rows[i].ProviderKey + "\x00" + rows[i].ModelID
		bk := groups[key]
		if bk == nil {
			bk = &bucket{tokens: make([]int64, len(days))}
			groups[key] = bk
			order = append(order, key)
		}

		if di, ok := dayIndex[rows[i].Day.UTC().Format("2006-01-02")]; ok {
			bk.tokens[di] += rows[i].Tokens
			bk.total += rows[i].Tokens
		}
	}

	var peak int64
	for _, key := range order {
		for _, v := range groups[key].tokens {
			if v > peak {
				peak = v
			}
		}
	}

	slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(groups[b].total, groups[a].total) })

	if len(order) > trendSeriesMax {
		rest := &bucket{tokens: make([]int64, len(days)), count: len(order) - trendSeriesMax + 1}
		for _, key := range order[trendSeriesMax-1:] {
			bk := groups[key]
			rest.total += bk.total
			for i := range bk.tokens {
				rest.tokens[i] += bk.tokens[i]
			}
		}
		order = append(order[:trendSeriesMax-1], "\x00other")
		groups["\x00other"] = rest
	}

	// 只交事实：窗口（UTC 日）、峰值、每个系列逐日的 token 数组与总量。
	// 折线坐标、点位置、配色、数字缩写都是显示格式 —— 归前端按 values 自己算。
	out := aidto.SessionTrend{
		From:   start.UTC().Format("2006-01-02"),
		To:     end.UTC().Format("2006-01-02"),
		Peak:   peak,
		Series: make([]aidto.TrendSeries, 0, len(order)),
	}
	for _, key := range order {
		bk := groups[key]
		parts := strings.SplitN(key, "\x00", 2)
		values := make([]int64, len(bk.tokens))
		copy(values, bk.tokens)
		out.Series = append(out.Series, aidto.TrendSeries{
			ProviderKey: parts[0],
			ModelID:     parts[1],
			OtherCount:  bk.count,
			Total:       bk.total,
			Values:      values,
		})
	}
	return out, nil
}

func sessionQueryOf(f aidto.SessionQuery) aimodel.SessionQuery {
	return aimodel.SessionQuery{
		Keyword:     strings.TrimSpace(f.Keyword),
		Status:      f.Status,
		ProviderKey: strings.TrimSpace(f.ProviderKey),
		ModelID:     strings.TrimSpace(f.ModelID),
		CreateBy:    f.CreateBy,
		From:        parseDayStart(f.From),
		To:          parseDayStart(f.To),
	}
}

func parseDayStart(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return nil
	}
	return &t
}

// dayStartUTC 取一个时刻所在 UTC 日期的零点。
//
// 趋势图的桶键、查询边界、显示的起止日期全部按 UTC —— 和数据库里
// 「date_trunc(... AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'」切出来的桶同一口径。
// 混用本地日期会让最后一天的桶和库里对不上（跨时区时整条曲线错位一天）。
func dayStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func normalizeSessionPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}

func (s *SessionService) SessionTokenUsageOf(ctx context.Context, id int64) (out aidto.SessionTokenUsage, err error) {
	detail, err := s.GetSession(ctx, id)
	if err != nil {
		return out, err
	}
	out.ContextTokens = detail.ContextTokens
	out.Compacts = int64(detail.CompactCount)
	out.Rows = []aidto.SessionTokenRow{}

	rows, err := s.model.SessionKindUsageOf(ctx, id)
	if err != nil {

		return out, err
	}
	var total int64
	for i := range rows {
		total += rows[i].Tokens
		out.Rows = append(out.Rows, aidto.SessionTokenRow{
			Kind:   rows[i].Kind,
			Events: rows[i].Events,
			Tokens: rows[i].Tokens,
		})
	}
	out.Total = total
	out.HasBreakdown = len(out.Rows) > 0
	return out, nil
}

func (s *SessionService) SessionModelUsageOf(ctx context.Context, sessionIDs []int64) (out map[int64][]aidto.SessionModelUsage, err error) {
	out = make(map[int64][]aidto.SessionModelUsage, len(sessionIDs))
	rows, err := s.model.SessionModelUsageOf(ctx, sessionIDs)
	if err != nil {
		return out, err
	}
	for i := range rows {
		pk := strings.TrimSpace(rows[i].ProviderKey)
		mid := strings.TrimSpace(rows[i].ModelID)
		out[rows[i].SessionID] = append(out[rows[i].SessionID], aidto.SessionModelUsage{
			ProviderKey: pk,
			ModelID:     mid,
			Events:      rows[i].Events,
			Tokens:      rows[i].Tokens,
			Unrecorded:  pk == "" && mid == "",
		})
	}
	return out, nil
}

const (
	toolResultLimit = 4000

	toolLogSummaryLimit = 400

	toolCallLogTimeout = 5 * time.Second
)

type ToolCallLogWriter interface {
	Insert(ctx context.Context, e *aimodel.AIToolCallLogEntity) error
}

type ToolCallRecorder struct {
	w ToolCallLogWriter
}

func NewToolCallRecorder(w ToolCallLogWriter) *ToolCallRecorder { return &ToolCallRecorder{w: w} }

func (r *ToolCallRecorder) Record(ctx context.Context, e *aimodel.AIToolCallLogEntity) {
	if r == nil || r.w == nil || e == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	base := context.WithoutCancel(ctx)
	writer := r.w
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Scene("ai").With("session", e.SessionID).With("panic", rec).
					Error(nil, "AI 工具调用流水写入协程发生 panic，已忽略")
			}
		}()
		wctx, cancel := context.WithTimeout(base, toolCallLogTimeout)
		defer cancel()
		if err := writer.Insert(wctx, e); err != nil {
			logger.Scene("ai").With("session", e.SessionID).With("tool", e.ToolName).
				Error(err, "AI 工具调用流水写入失败，已忽略")
		}
	}()
}

func (s *SessionService) SetToolCallLogWriter(w ToolCallLogWriter) {
	s.toolCalls = NewToolCallRecorder(w)
}

func NewToolCallEntry(sessionID, userID int64, toolName string, args string, latency time.Duration) *aimodel.AIToolCallLogEntity {
	return &aimodel.AIToolCallLogEntity{
		SessionID:        sessionID,
		UserID:           userID,
		ToolName:         strings.TrimSpace(toolName),
		ArgumentsSummary: SummarizeForLog(args),
		LatencyMs:        latency.Milliseconds(),
	}
}

func SummarizeForLog(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " "))
	return truncateRunes(s, toolLogSummaryLimit)
}

func PruneToolResult(text string) (string, bool) {
	if r := []rune(text); len(r) > toolResultLimit {
		return string(r[:toolResultLimit]) + truncationNotice(len(r)), true
	}
	return text, false
}

func truncationNotice(fullLen int) string {
	return "\n\n[结果过长已截断：完整结果共 " + strconv.Itoa(fullLen) + " 字，以上为前 " + strconv.Itoa(toolResultLimit) + " 字。需要更多请缩小查询范围后重新调用]"
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

func ToolErrorKeyOf(status aienums.ToolCallStatus) string {
	switch status {
	case aienums.ToolCallStatusForbidden:
		return aienums.ErrToolForbidden
	case aienums.ToolCallStatusFailed:
		return aienums.ErrToolRunFailed
	}
	return ""
}

type ToolIdempotencyStore struct {
	m *aimodel.ToolIdempotencyModel
}

func NewToolIdempotencyStore(m *aimodel.ToolIdempotencyModel) *ToolIdempotencyStore {
	return &ToolIdempotencyStore{m: m}
}

func (s *ToolIdempotencyStore) Lookup(ctx context.Context, tool, key string) (mcp.Result, bool, error) {
	if s == nil || s.m == nil {

		return mcp.Result{}, false, nil
	}
	row, ok, err := s.m.Lookup(ctx, tool, mcp.UserIDFrom(ctx), key)
	if err != nil || !ok {
		return mcp.Result{}, false, err
	}
	return mcp.Result{Text: row.ResultText}, true, nil
}

func (s *ToolIdempotencyStore) Save(ctx context.Context, tool, key string, res mcp.Result) error {
	if s == nil || s.m == nil {
		return nil
	}
	return s.m.Save(ctx, tool, mcp.UserIDFrom(ctx), key, res.Text)
}
