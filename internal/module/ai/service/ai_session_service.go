// ai_session_service.go — AI 会话层的用例编排：构造、错误哨兵与「会话头」这一件事。
//
// 拆分线（同 internal/module/CLAUDE.md）：事件追加在 ai_session_append.go，
// 投影在 ai_session_project.go，折叠在 ai_session_compact.go；本文件只放共享面与会话头用例。
//
// 会话层与配置层（ai_provider）刻意不互相依赖：会话只记 provider_key / model_id 两个字符串，
// 由调用方传进来。这样两层可以各自演进而不会互相等待。
package aiservice

import (
	"context"
	"errors"
	"strings"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/pkg/utils"
)

// 会话层错误哨兵：**取值是 i18n key**（登记在 enums），不是中文原文。
//
// 为什么必须是 key 形态：pkg/response.ErrorAuto 按「值像不像 key」判定业务错误 ——
// 中文原文会被判成内部错误而返回 500 + 通用文案；而且文案还要能按请求语言翻译。
// 面向用户的中文兜底统一登记在 aienums.FacingMessages（页面侧渲染用），service 不碰文案。
var (
	// ErrSessionNotFound 会话不存在。
	ErrSessionNotFound = errors.New(aienums.ErrSessionNotFound)
	// ErrSessionKeyMissing 追加事件时既没给会话键也没给会话 ID。
	ErrSessionKeyMissing = errors.New(aienums.ErrSessionKeyMissing)
	// ErrSessionArchived 会话已归档，拒绝写入。
	ErrSessionArchived = errors.New(aienums.ErrSessionArchived)
	// ErrSessionConflict 乐观锁版本不符（会话在本次读取之后被别人改过）。
	ErrSessionConflict = errors.New(aienums.ErrSessionConflict)
	// ErrSessionProviderMismatch 同一会话键被用于不同的供应商 / 模型。
	ErrSessionProviderMismatch = errors.New(aienums.ErrSessionProviderMismatch)
	// ErrEventKindInvalid 事件类别不在白名单。
	ErrEventKindInvalid = errors.New(aienums.ErrEventKindInvalid)
	// ErrEventContentEmpty 事件正文为空。
	ErrEventContentEmpty = errors.New(aienums.ErrEventContentEmpty)
	// ErrFoldRangeInvalid 折叠区间不合法（越界、倒置或与已有折叠重叠）。
	ErrFoldRangeInvalid = errors.New(aienums.ErrFoldRangeInvalid)
	// ErrFoldSummaryEmpty 折叠摘要为空。
	ErrFoldSummaryEmpty = errors.New(aienums.ErrFoldSummaryEmpty)
)

// 会话层的默认口径（数值集中在这里，避免散落在各文件里各写一遍）。
const (
	// DefaultKeepRecent 默认保留最近多少条不进折叠区间（尾部工作集）。
	DefaultKeepRecent = 12
	// NudgeGrowthTokens 上下文每涨这么多个 token 提醒一次「该压了」（docs/16 §3 的软提醒）。
	NudgeGrowthTokens = 50000
	// MinFoldSegmentTokens 待折叠段小于这个量时不值得压：摘要本身要花 token，
	// 折叠还会打断前缀缓存（docs/16 §3.1：短会话不值得压）。
	MinFoldSegmentTokens = 12000
	// summaryRatioPct 估算摘要体积占原文的百分比（保守取 10）。
	summaryRatioPct = 10
	// minSummaryTokens 摘要的体积下限（再短的段也要留一条能读的摘要）。
	minSummaryTokens = 200
)

// SessionService 会话层用例编排。
type SessionService struct {
	model *aimodel.SessionModel
	// chat 是「打一次模型」的能力，由装配层用 SetChatPort 注入（见 ai_session_chat.go）。
	// 用接口而非 *Service：会话层不依赖配置层的具体类型，未注入时 SendMessage 回业务错误而不是崩。
	chat ChatPort
	// callLog 是调用流水（ai_call_log）的读取端口，装配层用 SetCallLogReader 注入（见 ai_session_calls.go）。
	// 未注入时 SessionCallsOf 回空集合：悬浮卡少一段，而不是整页崩。
	callLog CallLogReader
	// tools 是「模型可调用工具」的能力，装配层用 SetToolProvider 注入（见 ai_session_chat.go）。
	// 未注入 = 不带工具的一问一答：工具是可选增强，缺了不该让普通对话也用不了。
	tools ToolProvider
}

// NewSessionService 构造。
func NewSessionService(m *aimodel.SessionModel) *SessionService { return &SessionService{model: m} }

// EnsureSession 按会话键取会话；不存在就建一个。
//
// 会话键由调用方给（客户端自己的 conversation 标识）：同一个键永远落到同一个会话，
// 客户端重启、服务重启都不影响续写。并发新建交给唯一约束兜底 —— 撞了就把对方那条取回来用，
// 不把这种竞态当错误抛给调用方。
func (s *SessionService) EnsureSession(ctx context.Context, key, providerKey, modelID, title string, userID int64) (*aimodel.AISessionEntity, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	if cur, err := s.model.FindSessionByKey(ctx, key); err != nil {
		return nil, err
	} else if cur != nil {
		// 命中已有会话时校验绑定是否一致：同一个会话键被换到别的供应商 / 模型上，
		// 会让两个模型的历史与计量互相污染 —— 明确报错而不是静默复用。
		if err := checkSessionBinding(cur, providerKey, modelID); err != nil {
			return nil, err
		}
		return cur, nil
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = key
	}
	// 列默认值只在 SQL 直插时生效：gorm 会把零值一并写进去，所以这里显式给出每个带默认值的列。
	// 尤其是 status —— 它的 0 是「归档」，漏掉就会把新会话建成归档态。
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

// ListSessions 分页列出会话。status < 0 表示不过滤状态；page/size 越界时回落到默认值。
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

// GetSession 取会话详情：会话头 + 当前投影 + 计量。
//
// 投影实时算、不落库 —— 它就是事件日志的函数；落一份投影等于多造一个会跟日志分叉的真源。
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

// RenameSession 改标题 / 换当前模型 / 改状态（归档与恢复走同一个用例）。
//
// 乐观锁：版本不符返回 ErrSessionConflict，由 inbound 提示「请刷新后重试」。
// 空字符串与 -1 表示「这一项不改」—— dto 里没有指针，用哨兵值表达「不涉及」。
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
		// 什么都没改：直接把当前状态回给调用方，不白跑一次写。
		return s.getSessionDTO(ctx, req.ID)
	}
	affected, err := s.model.UpdateSessionWithVersion(ctx, req.ID, req.Version, fields, req.UserID)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		// 0 行有两个可能：会话不存在，或版本不符。分开报，前端才知道该「刷新」还是该「返回列表」。
		if _, err = s.getSessionDTO(ctx, req.ID); err != nil {
			return nil, err
		}
		return nil, ErrSessionConflict
	}
	return s.getSessionDTO(ctx, req.ID)
}

// ListEvents 取原始事件日志（压缩后的原文仍在这里，供展开与审计用）。
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
		})
	}
	return out, total, nil
}

// —— 内部工具 ——

// getSessionDTO 取一个会话并转成对外形状；不存在时返回 ErrSessionNotFound。
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

// sessionDTO 实体 → 对外形状。
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

// estimateTokens 估算文本 token 数：没有分词器时的保守口径。
//
// 按 UTF-8 字节数除以 3 近似（中文一字 3 字节、英文一词若干字节）。宁可高估：
// 高估只会让折叠更早触发，代价是多花一点摘要开销；低估会让会话涨过模型上下文窗口。
func estimateTokens(text string) int64 {
	n := len(strings.TrimSpace(text))
	if n <= 0 {
		return 0
	}
	return int64((n + 2) / 3)
}

// estimateSummaryTokens 估算摘要体积（写摘要之前只能估，所以按比例取，并保底）。
func estimateSummaryTokens(segmentTokens int64) int64 {
	v := segmentTokens * summaryRatioPct / 100
	if v < minSummaryTokens {
		return minSummaryTokens
	}
	return v
}

// checkSessionBinding 校验调用方给出的 (providerKey, modelID) 与已有会话是否一致。
//
// 空值表示「本次不指定」—— 追加事件的普通调用不带这两项时不做校验；
// 一旦带了就必须与库里一致，不一致即 ErrSessionProviderMismatch（拒绝静默合并，评审 12）。
func checkSessionBinding(cur *aimodel.AISessionEntity, providerKey, modelID string) error {
	if pk := strings.TrimSpace(providerKey); pk != "" && pk != cur.ProviderKey {
		return ErrSessionProviderMismatch
	}
	if mid := strings.TrimSpace(modelID); mid != "" && mid != cur.ModelID {
		return ErrSessionProviderMismatch
	}
	return nil
}
