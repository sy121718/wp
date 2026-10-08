package aiservice

// 会话层与配置层（ai_provider）刻意不互相依赖：会话只记 provider_key / model_id 两个字符串，
// 由调用方传进来。这样两层可以各自演进而不会互相等待。

// SessionUsageOf 算聚合（这条会话在某家模型上总共烧了多少），
// SessionCallsOf 取流水（每一次调用各自多久、多少 token、成没成）。两者消费同一批会话 id，
// 查询、口径与展示位置都不同。

// 与 ai_chat.go 的分工：那边是「打一次上游」（provider → HTTP → 文本），
// 这边是「在一条会话里说一句话」（定位会话 → 上下文投影 → 调上游 → 落库）。
// 两者只通过下面这个窄接口相连，会话层不 import 配置层的任何具体类型。
//
// 带工具的轮次在本文件里编排：模型要求调工具 → 执行 → 结果回灌 → 再打一次，
// 直到它给出正文或撞上轮次上限。事件日志是本层的真源，所以每一对
// 「调用 / 结果」都各落一条事件（kind=tool，靠 meta.phase 区分）。

// 折叠**不改写历史**：它往事件日志里追加三条事件 ——
//
//	compact_start（记账）、compact_summary（surface_op=replace，带被替换的序号区间）、compact_end（记账）。
//
// 投影看到 replace 指令后，把 [replace_from_seq, replace_to_seq] 整段从「当前上下文」里盖掉，
// 换上摘要那一格；被盖掉的原文**仍在事件日志里**，随时可展开、可审计、可重放。
//
// 净收益口径（docs/16 §2）：折叠本身就花 token（写摘要 + 打断前缀缓存），
// 所以只折「已经不新鲜且足够大」的段；净收益为负时 FoldPlan 直接给出 Worthwhile=false。

// 为什么要它：悬浮球与概览页的提问框都是**单次问答**的渲染形态 —— 一次回答画在
// 一个节点里，关掉面板 / 刷新页面就只剩空框。用户看到的是「搜索引擎」，而服务端
// 那边其实一直是同一条会话在续写（fabSessionKey 固定、历史进 stablePrefix），
// 模型记得上一轮，界面上却看不出来。这个函数就是把「服务端已有的历史」交给界面。
//
// 只读、不建会话：找不到键就回空（首访时用户还没问过任何问题）。

// 这是整个会话层唯一的真相变换。规则三条（docs/16 §1）：
//
//  1. surface_op=append 的事件进入上下文；compact_start / compact_end 是折叠的记账事件，不进；
//  2. surface_op=replace 的事件是一个折叠块：它在上下文里占一格，并把
//     [replace_from_seq, replace_to_seq] 整段盖掉；
//  3. 落在任何折叠区间里的事件（含更早的折叠块自己）在上下文里消失 —— 但它们在事件日志里原样
//     保留，随时可展开、可审计、可重放。
//
// 为什么宁可每次重算也不落一份投影库：落库的投影是第二个真源，一旦与日志分叉就没人知道该信谁。
// 事件量按会话算是几千条级别、投影是一次线性扫描，重算比维护一致性便宜得多。

// 为什么这一步在**会话层**而不在工具里：工具拿不到调用者身份（`mcp.Runner.Run` 的
// userID 只用于权限判定，不进 handler），而每个数据源都要过一次权限判定 ——
// 权限问的是「这个账号能不能读这张表」，不是「这个工具有没有这个能力」。
// 把取数放在有身份的这一层，权限、审计与结果剪枝都自然复用工具那条链。
//
// 逐块取数时会**再进一次** ToolProvider.Run（每个 source 一次）。这是刻意的：
// 一次 ui_render 里的 N 个积木 = N 次权限判定 + N 次工具执行，
// 而不是「一次性放行整张图」。模型多要一块就多判一次，代价只有一次查库。

// 两件事放在一起是因为它们同源：**模型看到的工具结果**与**审计记下的那一条**
// 出自同一次调用，剪枝结果与审计摘要必须一起算，否则会出现「审计说 320 字、
// 模型实际看到 4000 字」这类对不上的账。
//
// 写入纪律完全沿用 ai_call_log（537）：协程异步写、脱离请求 ctx、独立超时、panic 不外溢。
// 唯一区别是工具调用发生在**一轮对话的中间**：如果同步写，用户等的是「工具耗时 + 两次落库」，
// 而工具本来就可能慢，所以这里的不阻塞更重要。

// 为什么 adapter 在 service 而不在 model：`mcp.IdempotencyStore` 的返回类型是
// `mcp.Result`，而 model 层不该认识工具层（那是它的上层）。service 在这里的角色
// 是**装配方**——它同时看得见 model 与 mcp，转换就在这里发生一次。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/mcp"
	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/internal/uispec"
	"go_wp/pkg/logger"
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
	// chat 是「打一次模型」的能力，由装配层用 SetChatPort 注入（见本文件的 SendMessage）。
	// 用接口而非 *Service：会话层不依赖配置层的具体类型，未注入时 SendMessage 回业务错误而不是崩。
	chat ChatPort
	// callLog 是调用流水（ai_call_log）的读取端口，装配层用 SetCallLogReader 注入（见 SessionCallsOf）。
	// 未注入时 SessionCallsOf 回空集合：悬浮卡少一段，而不是整页崩。
	callLog CallLogReader
	// tools 是「模型可调用工具」的能力，装配层用 SetToolProvider 注入（见本文件的 SendMessage）。
	// 未注入 = 不带工具的一问一答：工具是可选增强，缺了不该让普通对话也用不了。
	tools ToolProvider
	// toolCalls 是工具调用流水的记录器，装配层用 SetToolCallLogWriter 注入
	// （见 ai_tool.go）。未注入时审计是空操作 —— 审计缺失不该让对话失败。
	toolCalls *ToolCallRecorder
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
			Meta:           map[string]any(rows[i].Meta),
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

// AppendEvent 追加一条事件：定位或新建会话 → 分配序号 → 写事件 → 推进会话计量，全部在一个事务里。
//
// 四条保证：
//
//  1. 序号由事务内的 UPDATE … RETURNING 分配，并发追加不会撞号，回滚也不留空号；
//  2. 事件与计量（head_seq / context_tokens）同进同退，不会出现「日志里有、计数没动」；
//  3. 首次追加时**建会话与写事件同事务**：事务失败不会留下「0 事件空会话」脏数据；
//  4. **只有 append 一个写入口** —— 事件是 append-only 的，改写历史一律靠新事件表达
//     （surface_op=replace，见 Fold），谁都不能 UPDATE/DELETE 已有事件。
func (s *SessionService) AppendEvent(ctx context.Context, req aidto.AppendEventReq) (*aidto.AppendEventResult, error) {
	// 第一关卡：没有身份就不写事件。会话是「谁在什么时候说了什么」的记录，
	// 允许匿名写入等于允许往别人的审计流水里塞内容。
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

	// 并发首次追加同一个会话键时，两条事务会各自查不到对方、各自 INSERT，只有一条能过唯一约束。
	// 撞了就把整个事务重跑一次 —— 第二次能查到对方建的会话，走正常路径，不再插入。
	// 只重试一次：第二次还撞说明这不是「同一个键的并发首次」（比如调用方复用了别人的键），
	// 继续重试只会掩盖问题。
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

// appendInTx 单个事务内的追加流程：会话（查找或新建）与事件、计量同进同退。
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
		// 归档检查放在事务内读：归档是运维动作、撞车窗口极小，但既然事务已经开了，
		// 顺手在同一快照里判掉比在事务外多读一次更一致。
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
			// 这一条是谁写的：会话头那份 create_by 记的是「谁开的会话」，
			// 一条会话可以被多个账号续写，审计要追到每一条的发起人（535 起落库）。
			UserID: req.UserID,
			// 同一次追加里带上「这一条是谁产生的」：用量按供应商/模型拆开时只能靠事件上的这两列
			// （会话头那份会在换模型后被覆盖）。调用方不知道就留空，统计时归入「未记录」。
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

// ensureSessionTx 事务内定位本次写入的会话：给了 ID 就用 ID；否则按会话键取
// （命中做绑定一致性校验），不存在就**在同一事务里**新建。
//
// 建会话与写事件同事务：首次追加失败时整条回滚，库上不留「0 事件空会话」（评审 13）。
func (s *SessionService) ensureSessionTx(ctx context.Context, tx *gorm.DB, req aidto.AppendEventReq) (*aimodel.AISessionEntity, error) {
	if req.SessionID > 0 {
		sess, err := s.model.FindSessionByIDTx(tx, req.SessionID)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, ErrSessionNotFound
		}
		return sess, nil
	}

	key := strings.TrimSpace(req.SessionKey)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	cur, err := s.model.FindSessionByKeyTx(tx, key)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		if err := checkSessionBinding(cur, req.ProviderKey, req.ModelID); err != nil {
			return nil, err
		}
		return cur, nil
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = key
	}
	// 列默认值只在 SQL 直插时生效：gorm 会把零值一并写进去，所以这里显式给出每个带默认值的列。
	// 尤其是 status —— 它的 0 是「归档」，漏掉就会把新会话建成归档态。
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

// sessionCallPreview 悬浮卡里显示的最近调用条数。
//
// 5 条是「一停就能看完」的上限：悬浮卡不是详情页，再多就得滚动，而滚动区在鼠标移开时
// 会消失 —— 那反而看不成。所以总条数另行给出，读的人知道自己是看到了全部还是片段。
const sessionCallPreview = 5

// CallLogReader 会话层需要的调用流水读取能力（由 model 实现，装配期注入）。
//
// 用窄接口而不是直接持 *aimodel.CallLogModel：会话层对这张表只要「批量取最近几条」这一件事，
// 端口形状把能力收窄到这一件事上（用例也能塞一个假读取器）。
type CallLogReader interface {
	RecentCallsBySession(ctx context.Context, sessionIDs []int64, perSession int) ([]aimodel.SessionCallLogRow, error)
}

// SetCallLogReader 注入调用流水读取端口；未注入时 SessionCallsOf 回空（不 panic）。
func (s *SessionService) SetCallLogReader(r CallLogReader) { s.callLog = r }

// SessionCallsOf 批量取一组会话的调用流水预览（最近若干条 + 每条会话的总条数）。
//
// 一次查完整页（见 model.RecentCallsBySession）：列表 20 行逐行查就是 20 次往返，
// 而这块数据是「鼠标一停就要看到」的。
// 失败向上返回：调用方（页面）自己决定是整块不显示还是给一句提示。
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

// callRowOf 把一条流水翻成展示形状。
//
// 失败原因的取法：ErrorKey 是 i18n key（哨兵值），ErrorText 取 FacingMessages 的中文兜底 ——
// 模板写 tr(key, fallback)，与页面其它错误提示走同一条链。不在白名单里的 key
// （理论上不会有，callErrorKey 已经归口过）兜底成「调用失败」，绝不让一个裸 key 上页面。
func callRowOf(row aimodel.SessionCallLogRow) aidto.SessionCallRow {
	ok := row.Status == string(aienums.CallStatusOK)
	out := aidto.SessionCallRow{
		Time:        row.CreateTime.Format("01-02 15:04"),
		ProviderKey: row.ProviderKey,
		ModelID:     row.ModelID,
		LatencyText: formatLatency(row.LatencyMs),
		Tokens:      row.TotalTokens,
		TokensText:  formatTokens(row.TotalTokens),
		OK:          ok,
	}
	if !ok {
		// status 不是 ok 却没记错误原因（历史上可能被写进默认空值）：归口到 ErrInternal，
		// 而不是把一个空 key 交给模板 —— tr("") 的行为取决于 i18n 实现，不该让它决定页面长什么样。
		key := strings.TrimSpace(row.ErrorKey)
		if key == "" {
			key = aienums.ErrInternal
		}
		out.ErrorKey = key
		if text, has := aienums.FacingText(key); has {
			out.ErrorText = text
		} else {
			out.ErrorText = "调用失败"
		}
	}
	return out
}

// formatLatency 把毫秒翻成短文本。
//
// 阈值取 1 秒：低于 1 秒的调用用毫秒读更准（「480ms」比「0.5s」有信息量），
// 高于 1 秒的用秒读更省心（「2.9s」比「2900ms」一眼看得出量级）。
func formatLatency(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// chatTitleRunes 用首条用户消息派生会话标题时最多取多少个字符。
//
// 会话头要有标题才能进列表，而发消息这条路没有单独的「起个名字」步骤，
// 就取消息开头一段 —— 与前端「首条消息即标题」的习惯一致。
const chatTitleRunes = 60

// maxToolRounds 一次发消息里最多允许几轮「模型要求调工具」。
//
// 必须有上限：模型陷入「调工具 → 结果不满意 → 再调同一个」时，没有上限就是
// 一次请求把额度烧光，而用户看到的是界面一直转。
//
// 取 10 而不是 4：本仓的上游**一次只要求调一个工具**（不并行），所以「找商品 →
// 看商品详情 → 查库存 → 建单 → 作答」这种很普通的代客下单要占满 5 轮。4 轮时
// 真机上出现过「工具全部成功执行、库存与订单都已落库，界面却报提问失败」——
// 循环到上限退出，用户拿到的是一句失败，而他真正的订单已经建好了。
//
// 最后一轮的兜底见循环里的 roundTail：到上限时不再给工具，强制它用已有结果作答，
// 宁可答案不完整，也不能让一个已经改动了数据的请求以「失败」收场。
const maxToolRounds = 10

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
// 用接口而不是 *Service：会话层与配置层刻意不互相依赖（见本文件头注释），
// 装配处一行 SetChatPort 把两者接上，两层的编译期依赖保持单向。
type ChatPort interface {
	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
	// ChatStream 走 SSE 流式并把每个增量回调出去。
	//
	// 放在**同一个接口**而不是新开一个：会话层要么全程流式、要么全程非流式，
	// 两个端口会让「注入了一个、漏了另一个」变成本可以编译通过的错误，
	// 而它的症状是流式路径静默退化成非流式（用户看到「一直没有字，最后一次性冒出来」）。
	ChatStream(ctx context.Context, req *aidto.ChatReq, onDelta func(StreamDelta)) (*aidto.ChatResult, error)
}

// StreamEvent 流式过程中推给调用方的一条事件。
//
// 分 Kind 而不是只给字符串：消费者对三类的处理完全不同（思考过程进折叠区、
// 正文进回答区、工具进展进状态行），合成一类再让调用方猜，等于把分类知识
// 复制到每个消费者里。
type StreamEvent struct {
	// Kind 取值见 streamEvent* 常量。
	Kind string
	// Text 本条的文本（工具事件里是「正在查询 xxx…」这类整句）。
	Text string
}

// 流式事件的类别。
const (
	// streamEventReasoning 思考过程增量。
	streamEventReasoning = "reasoning"
	// streamEventText 正文增量。
	streamEventText = "text"
	// streamEventTool 工具进展（整句，不是增量）。
	//
	// 为什么要推它：一次任务里最长的等待往往发生在「模型要求调工具 → 结果回来」这段，
	// 那段时间没有正文也没有思考过程，用户只能看到一个没反应的界面。
	streamEventTool = "tool"
)

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

// 发消息这条路的错误哨兵（取值同样是 i18n key，口径见本文件头注释）。
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
	return s.sendMessage(ctx, req, nil)
}

// SendMessageStream 与 SendMessage 同一套语义，只是把过程中的增量推给 emit。
//
// emit 为 nil 时行为与 SendMessage 完全一致（但**仍走流式通道**）——
// 两条路径共用下面的实现，所以「流式下工具调用丢了」这类只在一条路径上出现的
// 缺陷没有生存空间。
//
// emit 由调用方在自己的 goroutine 里同步调用，必须足够快（写响应流），
// 不得阻塞、不得回头调用本服务。
func (s *SessionService) SendMessageStream(ctx context.Context, req aidto.SendMessageReq, emit func(StreamEvent)) (*aidto.SendMessageResult, error) {
	return s.sendMessage(ctx, req, emit)
}

func (s *SessionService) sendMessage(ctx context.Context, req aidto.SendMessageReq, emit func(StreamEvent)) (*aidto.SendMessageResult, error) {
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
		Meta:        userEventMeta(&req),
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
	// 逐轮收集思考过程。**不是只留最后一轮**：一次要跑几个工具的任务里，
	// 「它为什么选这个工具」恰恰在前面几轮的思考里，末轮只有收尾。
	var reasonings []string
	specs := s.toolSpecs()
	for round := 0; round < maxToolRounds; round++ {
		// 图片只在**第一轮**带：后面几轮是工具往返，模型已经看过这些图，
		// 每轮重发一遍会让一次多工具任务的输入凭空多出几份图片的体积。
		var roundImages []string
		if round == 0 {
			roundImages = req.Images
		}
		msgs := stablePrefix(history, roundImages)
		msgs = append(msgs, rounds...)

		// 末轮收回工具：这一步是**兜底而不是优化**。
		//
		// 已经执行过的工具都改过了真实数据（订单建了、库存动了），此时若因为
		// 「模型还想再查一次」就整轮报错，用户看到的是失败，而数据已经变了 ——
		// 他没法从界面上知道到底发生了什么，也不知道要不要重来。收回工具后
		// 模型只能用现有结果说话，最差是答案含糊，不会让改动无法解释。
		roundSpecs := specs
		if round == maxToolRounds-1 {
			roundSpecs = nil
		}

		// 带上会话与发起人：调用流水（ai_call_log）靠这两个字段回答
		// 「谁在什么时候烧了谁家的 token」，而它们只有这里知道
		//（出站层只认识 provider/model 两个字符串）。
		chatReq := &aidto.ChatReq{
			ProviderKey:     providerKey,
			Model:           model,
			Messages:        msgs,
			Tools:           roundSpecs,
			MaxOutputTokens: req.MaxOutputTokens,
			SessionID:       sessionID,
			UserID:          req.UserID,
		}
		// 有 emit 就走流式。**两族的增量在这里翻译成同一种事件**：
		// 协议差异（chat 的 delta 与 responses 的事件名）已经在出站层抹平，
		// 会话层不该再认识它们。
		chatRes, err := s.chatWithEmit(ctx, chatReq, emit)
		if err != nil {
			return nil, err
		}
		if chatRes == nil {
			return nil, ErrSessionChatEmptyReply
		}
		if r := strings.TrimSpace(chatRes.Reasoning); r != "" {
			reasonings = append(reasonings, r)
		}

		// 没有工具调用 = 这一轮就是最终回答。
		if len(chatRes.ToolCalls) == 0 {
			output := strings.TrimSpace(chatRes.Output)
			think := strings.Join(reasonings, "\n\n")
			if output == "" {
				// 模型只给了思考过程、没给正文。把思考当成回答交出去，而不是报「空回答」。
				//
				// 实测过一次：上游返回了 221 个输出 token，全部落在思考段里，
				// 正文是空的；那时用户看到的是「这次没能拿到回答，请稍后再试」——
				// 明明有内容，却被当成失败。把思考铺出来至少让人看到它在想什么，
				// 并且**这一段确实来自模型**，不是我们编的。
				// 只有连思考都没有时，才真的是空回复。
				if strings.TrimSpace(think) == "" {
					return nil, ErrSessionChatEmptyReply
				}
				// 已经当正文了就不再重复写进 meta.reasoning。
				return s.finishReply(ctx, sessionID, req, providerKey, model, think, userRes, input,
					toolEvents, "")
			}
			return s.finishReply(ctx, sessionID, req, providerKey, model, output, userRes, input,
				toolEvents, think)
		}

		// 有工具调用：逐个执行并把「调用 / 结果」成对落库。
		for _, call := range chatRes.ToolCalls {
			// 先推一条进展再落库：跑工具是整条链路里最长的等待，
			// 而那段时间既没有正文也没有思考过程（模型此刻无话可说）。
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

	// 到这里说明末轮（已收回工具）之后模型还是空的、或又要求了工具。
	// 事件全部保留 —— 审计要看到它试了什么、改了什么；但这条消息没有答案。
	return nil, ErrSessionToolRoundsExceeded
}

// finishReply 落 assistant 事件并组装返回（带工具循环的正常出口）。
//
// reasoning 是这一轮多轮往返里模型给出的思考过程（可能为空：并非所有上游都返回）。
// 它写进事件的 meta 而不是 content —— content 是**模型对用户说的话**，
// 两者混在一起会让对话历史里凭空多出一段「模型说自己想过了什么」的伪上下文。
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
	// 带上 providerKey/model：这条回复是这两家产生的（529 起事件自带来源），
	// 用量按供应商/模型拆开时靠的就是它，而不是会话头那份（换模型时会被覆盖）。
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

// chatWithEmit 按有没有 emit 选择流式或非流式出站，并把增量翻成会话层事件。
//
// emit 为 nil 时退回非流式：非流式那条路径是长时间验证过的（含 usage 上报口径），
// 没有消费者要看增量时不必让它多绕一圈 SSE 解析。两条路径的结果结构完全同形
// （都汇合到 ProtocolReply），所以上层逻辑不必知道走的是哪条。
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

// emitToolEvent 把「模型要求调某个工具」翻成一条给人看的进展。
//
// 只说工具名与参数摘要，不说「正在思考」—— 用户此刻想知道的是「它在查什么」，
// 而这句话里最有信息量的就是工具名。
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

// eventMetaReasoning 事件 meta 里放思考过程的键。
//
// 与渲染数据（meta["render"]）并列存在同一个 meta 对象里：两个键互不影响，
// 读侧各取各的。用常量而不是字面量，是因为它有两个消费者（会话详情页与悬浮球），
// 拼错一处不会报错、只会让那一边永远看不到思考过程。
const eventMetaReasoning = "reasoning"

// eventMetaUserText 事件 meta 里放「用户原话」的键（见 userEventMeta）。
const eventMetaUserText = "userText"

// eventMetaImageLabels 事件 meta 里放「本轮带了哪些图」的键（标识数组，如文件名）。
//
// 存标识而不是发给模型的 data URI：data URI 是 base64 的像素，落进事件日志
// 会把库撑大好几个数量级，而它想表达的只有「这一轮有图」这一件事。
const eventMetaImageLabels = "imageLabels"

// userEventMeta 给用户事件附上原话（与注入过上下文的 Content 分开放）。
//
// 两者相同或调用方没给原话时返回 nil —— meta 不是空的就不写，
// 免得每条事件都挂一个没有信息的对象。
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
func stablePrefix(history string, images []string) []aidto.ChatMessage {
	// 目录为空时**不占一条消息**：空 system 消息在部分上游会被当成无效消息拒掉，
	// 而在没有手册时（例如裁剪过的部署）它本身就是多余的。
	msgs := []aidto.ChatMessage{{Role: roleSystem, Content: aiprompt.SiteRules()}}
	if catalog := aiprompt.ManualCatalog(); catalog != "" {
		msgs = append(msgs, aidto.ChatMessage{Role: roleSystem, Content: catalog})
	}
	// 图片挂在本轮那条 user 消息上（不是拼进 history 文本）：协议上的图片是
	// content 分片，拼成文字只能是路径，而模型读不出路径里的像素。
	return append(msgs, aidto.ChatMessage{Role: roleUser, Content: history, Images: images})
}

// 折叠建议的机器可读理由（不是给人看的文案，展示文案由前端按值取 i18n key）。
const (
	// FoldReasonOK 该段值得折。
	FoldReasonOK = "ok"
	// FoldReasonNoSegment 可见上下文还不够长，没有可折的段。
	FoldReasonNoSegment = "no_segment"
	// FoldReasonShortSegment 待折段太小：摘要开销会吃掉收益。
	FoldReasonShortSegment = "short_segment"
	// FoldReasonNotPositive 净收益不为正。
	FoldReasonNotPositive = "not_positive"
)

// excerptLimit 建议里回带的原文上限（按 rune 计）：够写摘要即可，不必把整段历史搬给前端。
const excerptLimit = 4000

// FoldPlan 计算建议折叠区间与净收益；不写任何东西。
//
// 区间口径：可见投影里**保留最近 KeepRecent 项**作为尾部工作集，前面那些就是待折段。
// 这里用的是投影后的可见项（已被折叠盖掉的项不参与），所以连续多次折叠不会重复折同一段。
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

// Fold 提交一次折叠：一个事务里落 compact_start / compact_summary / compact_end 三条事件，
// 然后用**绝对值**重算 context_tokens 并让 compact_count +1。
//
// 为什么重算是绝对值而不是增量：折叠把 N 条换成 1 条，增量算不出正确结果；
// 而且重算读的是同一事务内的事件（含刚写的三条），不会漏掉并发追加进来的内容。
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
		// 区间合法性：必须落在尚未被折叠盖掉的可见区间里，且不与已有折叠区间相交。
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

		// 重算：投影 + 求和都在同一事务内，看到的是含本次三条事件的最新日志。
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

// foldRangeValid 判区间是否可折：两端都必须存在、未被折叠盖掉，且区间内不含已有折叠指令。
func foldRangeValid(events []aimodel.AIEventEntity, from, to int64) bool {
	var (
		hasFrom bool
		hasTo   bool
	)
	for i := range events {
		e := events[i]
		if e.Seq >= from && e.Seq <= to {
			// 区间里不能再含一条 replace 指令（同一条不能再折）。
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
	// 端点本身不能被已有折叠盖掉（否则它已经不在当前上下文里了）。
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

// projectedTokens 事件序列 → 当前上下文的 token 总和（与投影同一套规则，但不构造中间切片）。
func projectedTokens(events []aimodel.AIEventEntity) int64 {
	return projectTokens(projectEvents(events))
}

// joinExcerpt 把待折段的可见项拼成纯文本建议（超长按 rune 截断，不切坏 UTF-8）。
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

// dialogueTurnLimit 默认回填的轮数上限（一轮 = 一条提问 + 一条回答）。
// 再多也没有意义：回填是给人扫一眼「我们聊到哪了」，不是审计工具。
const dialogueTurnLimit = 12

// RecentDialogue 取会话键下最近的若干轮对话，按时间**正序**返回。
//
// 会话不存在或没有提问时返回 nil（不是错误）—— 首访就走这条路。
func (s *SessionService) RecentDialogue(ctx context.Context, key string, limit int) ([]aidto.DialogueTurn, error) {
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
	if sess == nil {
		return nil, nil
	}

	// 一轮至少两条事件（user + assistant），实际还会夹着工具事件；按 4 倍取够。
	// 倒序取是为了「最近的优先」：会话很长时也要能立刻看到最新的那几轮。
	rows, _, err := s.model.ListEventsDesc(ctx, sess.ID, 0, limit*4)
	if err != nil {
		return nil, err
	}

	turns := make([]aidto.DialogueTurn, 0, limit*2)
	// 倒序取回来的行要翻回正序，界面才是「从上往下读」的时间线。
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
			// 原话优先：Content 是注入过页面上下文的那份，界面回填只该显示用户敲进去的。
			turn.Text = strings.TrimSpace(v)
		}
		turns = append(turns, turn)
	}
	return tailDialogueTurns(turns, limit), nil
}

// imageLabelsOf 从事件 meta 里取图片标识。
//
// meta 是 JSON 往返过的，数组元素只会是 any（不是 string）—— 直接断言 []string
// 永远失败，而失败的方向正是「历史里明明有图却一张都不显示」。
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

// tailDialogueTurns 只保留最后 limit 轮（从末尾往前数 limit 个 user 行）。
//
// 按「轮」而不是按「条」截断：截出半轮（只有提问没有回答）会让界面显示一个
// 永远等不到回答的气泡，看起来像卡住了。
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
		// 记住**第 limit 轮的那个提问**的位置：保留要从它开始，
		// 而不是从它的下一行（那会把这一轮的提问切掉，留下一个没有问题的回答）。
		start = i
	}
	return turns[start:]
}

// project 取整段历史并投影。
func (s *SessionService) project(ctx context.Context, sessionID int64) ([]aidto.SessionItem, error) {
	events, err := s.model.ListEventsFrom(ctx, sessionID, 1, 0)
	if err != nil {
		return nil, err
	}
	return projectEvents(events), nil
}

// foldSpan 一个折叠区间（[from, to] 闭区间）。
type foldSpan struct{ from, to int64 }

// projectEvents 把事件序列投影成当前上下文（纯函数：给同样的日志永远算出一样的上下文）。
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

	// 排序键不是事件的 seq：折叠块（surface_op=replace）在日志里排在它盖掉的区间**之后**，
	// 但它在上下文里顶替的是那个区间的位置。用 replace_from_seq 当排序键，摘要块才会落在
	// 区间原来的位置；用 e.Seq 会让折叠后的摘要跑到上下文末尾（回归：2026-10 实测）。
	type placed struct {
		order int64
		item  aidto.SessionItem
	}
	placedItems := make([]placed, 0, len(events))
	for i := range events {
		e := events[i]
		switch {
		case e.SurfaceOp == string(aienums.SurfaceReplace):
			// 折叠块自己：它在上下文里占一格。
			placedItems = append(placedItems, placed{order: e.ReplaceFromSeq, item: sessionItemOf(e, true)})
		case foldedAway(e.Seq):
			// 已被某次折叠盖掉：原文留在日志里，不进上下文。
		case e.Kind == string(aienums.EventKindCompactStart), e.Kind == string(aienums.EventKindCompactEnd):
			// 折叠的记账事件：它不是对话内容。
		default:
			placedItems = append(placedItems, placed{order: e.Seq, item: sessionItemOf(e, false)})
		}
	}
	sort.SliceStable(placedItems, func(i, j int) bool { return placedItems[i].order < placedItems[j].order })

	out := make([]aidto.SessionItem, 0, len(placedItems))
	for i := range placedItems {
		out = append(out, placedItems[i].item)
	}
	return out
}

// sessionItemOf 事件 → 投影项。
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

// projectTokens 一组投影项的 token 总和。
func projectTokens(items []aidto.SessionItem) int64 {
	var total int64
	for i := range items {
		total += items[i].Tokens
	}
	return total
}

// renderMetaLimit 工具事件里 render 结构的落库上限（字节）。
//
// 超过就整块不落库（页面退化成只显示正文），而不是截断 —— 半个 JSON 反序列化必然失败，
// 存下去只会把「为什么这块没渲染」变成一个查不出来的问题。
const renderMetaLimit = 64 << 10

// uiSpecResolver 把「已注册工具」适配成 uispec 的数据源。
type uiSpecResolver struct {
	provider ToolProvider
	userID   int64
}

// Resolve 执行一次数据源取数。
//
// 参数直接按工具的 JSON Schema 传（spec 的 params 本来就是「这个数据源的参数」），
// limit 只在 > 0 时补进去 —— 补一个 0 会让「工具自己的默认值」被显式 0 覆盖掉。
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
	// 业务性失败（越权 / 参数不合法 / 查库失败）在这里是**没有 error 的**：
	// 装配层按契约把它们翻成文本 + 状态码返回。所以这里还要看状态码，
	// 只判 error 会把「你没有权限查订单」当成一次成功取数。
	if res.Status != aienums.ToolCallStatusOK {
		return nil, errors.New(res.Text)
	}
	if res.Data == nil {
		return nil, fmt.Errorf("数据源 %s 没有可渲染的结构", source)
	}
	return res.Data, nil
}

// renderSpec 逐块取数并返回可渲染的视图。
//
// 不返回 error：单块失败不该让整条回答失败（uispec.Execute 的同一取舍），
// 调用方按 len(views) 判断「用户这一轮到底有没有东西看」。
func (s *SessionService) renderSpec(ctx context.Context, sessionID, userID int64, spec *uispec.Spec) []uispec.View {
	views, notes := uispec.Execute(ctx, spec, &uiSpecResolver{provider: s.tools, userID: userID})
	for _, n := range notes {
		if n.Kind == uispec.NoteOK {
			continue
		}
		// 失败原因只进日志：它可能带表名、参数与内部路径，而这段结构最终会进事件的 meta。
		logger.Scene("ai").With("session", sessionID).With("source", n.Source).
			With("kind", n.Kind).Warn("展示积木未渲染：" + n.Msg)
	}
	return views
}

const (
	// sessionTrendDaysDefault 趋势图默认窗口（天）。
	sessionTrendDaysDefault = 30
	// sessionTrendDaysMax 趋势窗口上限（天）。同一张图再宽下去每根柱子不到 2px，
	// 看不出形状反而更贵；要看更长的历史就靠筛选把区间切小分段看。
	sessionTrendDaysMax = 90
)

// ListSessionsFiltered 多条件分页列会话（口径与 ListSessions 一致，条件更多）。
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

// SessionUsageOf 指标卡（会话数 / 事件数 / 累计 token）。
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
		// 除法的守卫放在这里而不是让模板自己判：模板里每个用到均值的地方都得记得判一次。
		out.AvgTokens = raw.Tokens / raw.Sessions
	}
	out.TokensText = formatTokens(out.Tokens)
	out.AvgTokensText = formatTokens(out.AvgTokens)

	// —— docs/16 §3.1 的三个验收数字 ——
	//
	// 命中率的分母只取**报了缓存字段**的那些调用（CachedInputTokens）：
	// 把没报的算进分母会让命中率凭空掉一大截，而「难看」这个症状会被归因到提示词上，
	// 没人会想到是上游没报。一条都没报时 HitRateReady=false，页面显示「—」而不是 0%。
	out.HitRateReady = raw.CachedInputTokens > 0
	out.HitRatePct = percentage(raw.CachedTokens, raw.CachedInputTokens)
	out.HitRateText = percentText(out.HitRatePct, out.HitRateReady)
	out.CachedCalls = raw.CachedCalls
	out.UsageReportedCalls = raw.UsageReportedCalls

	// 压缩开销：摘要占的上下文比重。分母用**事件**的 token 之和（不是调用流水）——
	// 这个数问的是「压缩后的上下文里，摘要本身占了多大一块」，所以两侧必须同源。
	out.CompactCostPct = percentage(raw.SummaryTokens, raw.Tokens)
	out.CompactCostText = percentText(out.CompactCostPct, raw.Tokens > 0)
	return out, nil
}

// percentage 求分子占分母的百分比（一位小数）；分母 <= 0 时回 0。
//
// 回 0 而不是 NaN：NaN 会一路渲染成「NaN%」，且不报错、不进日志。
func percentage(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

// percentText 百分比展示串。ready=false 时回「—」。
//
// **不能用 0% 表示「没数据」**：0% 命中率是一个严重的信号（前缀每轮都在变），
// 而「这家没报这个字段」完全不是一回事。把两者显示成同一个字符，
// 等于把最有价值的一条观测信息抹掉。
func percentText(pct float64, ready bool) string {
	if !ready {
		return "—"
	}
	return strconv.FormatFloat(pct, 'f', 1, 64) + "%"
}

// SessionFilterOptions 筛选下拉的候选值（供应商 / 模型 / 创建人）。
//
// 取全量：筛成 A 之后下拉里还得有 B，否则筛一次就回不去。
func (s *SessionService) SessionFilterOptions(ctx context.Context) (aidto.SessionFilterOptions, error) {
	facets, err := s.model.SessionFacetsOf(ctx)
	if err != nil {
		return aidto.SessionFilterOptions{}, err
	}
	// append 到空切片而不是直接赋值：Pluck 在零行时给的是 nil，
	// 直接赋进 json 会变成 null，前端就得为 null 再写一遍判空。
	return aidto.SessionFilterOptions{
		Providers: append([]string{}, facets.Providers...),
		Models:    append([]string{}, facets.Models...),
		Creators:  append([]int64{}, facets.Creators...),
	}, nil
}

// 折线图坐标系与上限。viewBox 是固定值 + preserveAspectRatio="none"：图自适应容器宽度，
// 描边靠 CSS 的 vector-effect: non-scaling-stroke 保住 2px，不被横向拉伸带粗。
const (
	trendViewW = 1000
	trendViewH = 200
	// trendPadX 左右各留一点：x=0 的点描边有一半落在 viewBox 外，会被裁掉半条线。
	trendPadX = 6
	trendPadY = 12
	// trendSeriesMax 折线最多画几条：再多颜色就分不开、图例也压成一团。
	// 超出的不丢弃，合并成一条「其他」（丢掉会让图上总量对不上指标卡）。
	trendSeriesMax = 8
	// trendColorCount 调色板色数（--chart-c1..8）。
	trendColorCount = 8
)

// SessionTrend 折线图：按 (供应商, 模型) 分组的逐日 token。
//
// 为什么是折线而不是柱状：柱状图一天一根、每根只有一个高度，多序列只能堆叠或并排 ——
// 堆叠看不出单条趋势，并排又把一天切成一簇细柱。折线天然就是多序列的形态。
//
// 窗口规则与旧柱状图版一致：终点取筛选区间的结束日（缺省今天），起点往回推 trendDays-1 天，
// 但不早于筛选起点；筛选区间比窗口窄时以筛选为准（图与表说的是同一段时间）。
// 有数据的第一天之前不画 —— 那一段本来就是一片 0，画出来只会把真正的变化压成一条直线
// （实测「30 天窗口里只有一天有数据」时，旧版图上是 1 根柱 + 29 根看不见的底线，看着像坏了）。
func (s *SessionService) SessionTrend(ctx context.Context, f aidto.SessionQuery, trendDays int) (aidto.SessionTrend, error) {
	q := sessionQueryOf(f)
	span := trendDays
	if span < 1 || span > sessionTrendDaysMax {
		span = sessionTrendDaysDefault
	}

	end := dayStart(time.Now())
	if q.To != nil {
		end = *q.To
	}
	start := end.AddDate(0, 0, -(span - 1))
	if q.From != nil && q.From.After(start) {
		start = *q.From
	}
	if start.After(end) {
		// 筛选给了早于结束日的起点之类的手改区间：收敛成一天，不要造出空区间。
		start = end
	}

	rows, err := s.model.SessionTokenTrendBySeries(ctx, q, start, "day")
	if err != nil {
		return aidto.SessionTrend{}, err
	}
	// 用 rows[0] 而不是「最早的事件时间」—— 服务端不为此多查一次库。
	if len(rows) > 0 {
		if first := dayStart(rows[0].Day.UTC()); first.After(start) {
			start = first
		}
	}

	days := make([]time.Time, 0, span)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		days = append(days, cur)
	}
	dayIndex := make(map[string]int, len(days))
	for i := range days {
		dayIndex[days[i].Format("2006-01-02")] = i
	}

	// 按 (供应商, 模型) 归组。map 遍历顺序不定，所以另存一份出现顺序 ——
	// 同总量时的排序稳定性依赖它。
	type bucket struct {
		tokens []int64
		total  int64
		// count 只在「其他」那条上非零：并入的家数。
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
		// ::date 出来的是 UTC 零点，直接用本地时区格式化会偏移一天。
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

	// 总量降序：消耗最大的那条排最上面，也拿到调色板第 1 号色。
	sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].total > groups[order[j]].total })

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

	out := aidto.SessionTrend{
		From:     start.Format("2006-01-02"),
		To:       end.Format("2006-01-02"),
		Peak:     peak,
		PeakText: formatTokens(peak),
		Series:   make([]aidto.TrendSeries, 0, len(order)),
	}
	for i, key := range order {
		bk := groups[key]
		pts := make([]string, 0, len(days))
		var dotX, dotY int
		for di, v := range bk.tokens {
			// 单点时落正中：它没有「首尾」可言，贴左反而像被裁掉了。
			x := trendViewW / 2
			if len(days) > 1 {
				x = trendPadX + di*(trendViewW-2*trendPadX)/(len(days)-1)
			}
			y := trendViewH - trendPadY
			if peak > 0 {
				y = trendViewH - trendPadY - int(v*int64(trendViewH-2*trendPadY)/peak)
			}
			pts = append(pts, strconv.Itoa(x)+","+strconv.Itoa(y))
			dotX, dotY = x, y
		}
		// key 是 "provider\x00model"（「其他」那条是 "\x00other"）：在这里拆回字段，
		// 不另存一份 label —— 图例文案由模板按语言组装。
		parts := strings.SplitN(key, "\x00", 2)
		out.Series = append(out.Series, aidto.TrendSeries{
			ProviderKey: parts[0],
			ModelID:     parts[1],
			OtherCount:  bk.count,
			Total:       bk.total,
			TotalText:   formatTokens(bk.total),
			Color:       (i % trendColorCount) + 1,
			Points:      strings.Join(pts, " "),
			Single:      len(days) == 1,
			DotX:        dotX,
			DotY:        dotY,
		})
	}
	return out, nil
}

// sessionQueryOf dto 筛选条件 → model 查询条件。
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

// parseDayStart 把 yyyy-mm-dd 解析成本地时区当天零点；空串或格式不对返回 nil（= 不限）。
//
// 解析失败不报错：这三个字符来自 URL，手改错一个数字就让整页 500 不成比例；
// 按「不限」处理在页面上是看得见的（筛选栏那格还是空的），不会静默给出错结果。
func parseDayStart(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil
	}
	return &t
}

// dayStart 抹掉时分秒，只留本地日期。
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// normalizeSessionPage 归一化分页参数（口径与 ListSessions 一致）。
func normalizeSessionPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}

// formatTokens 把 token 数压成短文本（1234 → "1234"，12345 → "12.3K"，12345678 → "12.3M"）。
//
// 万以下给原数：看板上「事件数 4」「token 15」这种小数字用 K 表示会变成 0.0K，
// 比原数难读。原始整数仍随 dto 一起给出，模板要精确值就用整数字段。
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + "B"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// SessionTokenUsageOf 一个会话的 token 消耗快照（详情抽屉顶部那块数）。
//
// 两个口径来自两处：上下文与压缩次数读会话行（折叠后的当下状态），累计与明细按 kind
// 聚合事件表（历史总量）。它们本来就不是一个数 —— 折叠会让前者变小、后者不变。
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
		// 聚合失败不该让整个抽屉打不开：三个数照给，只是没有明细表。
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
	out.TotalText = formatTokens(total)
	out.HasBreakdown = len(out.Rows) > 0
	return out, nil
}

// SessionModelUsageOf 批量取一组会话的按 (供应商, 模型) 拆分，按会话 id 分组返回。
//
// 列表页一次拿整页：悬浮卡是「鼠标一停就要看到」的东西，逐行查就是 20 次往返，
// 而第 20 行的卡永远在等第 20 次查询。
// 未记录来源的那一组（529 之前写入的历史事件）标 Unrecorded —— 展示层据此显示「未记录」，
// 而不是把它当成「某个名字为空的供应商」。
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
			TokensText:  formatTokens(rows[i].Tokens),
			Unrecorded:  pk == "" && mid == "",
		})
	}
	return out, nil
}

const (
	// toolResultLimit 交给模型的工具结果长度上限（字符，按 rune 计）。
	//
	// 为什么必须剪：工具结果是**进稳定前缀之后**的动态内容，一次几万字的列表会把
	// 上下文挤满，且每一轮工具往返都要重发（自第三次起每次都在为同一份大结果付输入费）。
	// 这个值与会话压缩的 excerptLimit 同量级，量级一致才好估算单轮上下文。
	//
	// 工具侧不该指望这里兜底：能把输出做小的工具（分页、只回摘要）仍应自己做小，
	// 剪枝是最后一道闸，不是输出设计。
	toolResultLimit = 4000

	// toolLogSummaryLimit 审计摘要的长度上限（与 ai_tool_call_log 的 VARCHAR(500) 匹配，
	// 留出截断标记与省略号的余量）。
	toolLogSummaryLimit = 400

	// toolCallLogTimeout 单条工具流水的写入上限（独立于工具耗时：工具跑完还要留出落库时间）。
	toolCallLogTimeout = 5 * time.Second
)

// ToolCallLogWriter 工具调用流水的写入端口（由 model 实现，装配期注入）。
//
// 与 CallLogWriter 同形（都是「追加一条」这一件事）：端口形状把能力收窄，
// 用例也能换一个记录器进来断言写了什么。
type ToolCallLogWriter interface {
	Insert(ctx context.Context, e *aimodel.AIToolCallLogEntity) error
}

// ToolCallRecorder 工具调用流水的记录器。
//
// 独立成类型而不是挂在 SessionService 上：**外部 /mcp 调用也要记同一张表**
// （session_id = 0），而那条路根本没有会话 —— 把记录能力从会话里拆出来，
// 两条路才共用同一套「摘要口径 + 异步纪律 + panic 兜底」。
type ToolCallRecorder struct {
	w ToolCallLogWriter
}

// NewToolCallRecorder 构造；w 为 nil 时 Record 是空操作（审计缺失不该让调用失败）。
func NewToolCallRecorder(w ToolCallLogWriter) *ToolCallRecorder { return &ToolCallRecorder{w: w} }

// Record 异步落一条流水。丢弃条件与 panic 兜底同 logCallAsync。
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

// SetToolCallLogWriter 注入工具调用流水写入端口；未注入时审计是空操作（不 panic）。
func (s *SessionService) SetToolCallLogWriter(w ToolCallLogWriter) {
	s.toolCalls = NewToolCallRecorder(w)
}

// NewToolCallEntry 组装一条工具调用流水（调用方只需填结论相关的字段）。
//
// 固定填的几项：会话、账号、工具名、耗时。参数与结果摘要都在这里统一截断 ——
// 装配点不重复这件事，否则两处口径迟早会分叉。
func NewToolCallEntry(sessionID, userID int64, toolName string, args string, latency time.Duration) *aimodel.AIToolCallLogEntity {
	return &aimodel.AIToolCallLogEntity{
		SessionID:        sessionID,
		UserID:           userID,
		ToolName:         strings.TrimSpace(toolName),
		ArgumentsSummary: SummarizeForLog(args),
		LatencyMs:        latency.Milliseconds(),
	}
}

// SummarizeForLog 把一段文本压成审计摘要：折叠换行、去掉首尾空白、超长截断。
//
// 折叠换行是必要的：摘要列是 VARCHAR，多行 JSON 直接塞进去在列表里会把一行撑成一片。
func SummarizeForLog(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " "))
	return truncateRunes(s, toolLogSummaryLimit)
}

// PruneToolResult 把工具结果剪到上下文可承受的长度。
//
// 返回值 truncated 表示**是否发生了截断**：审计要记这一列，因为模型当时看到的
// 是剪枝后的版本，排查「模型为什么没用上完整数据」时得先知道这件事。
//
// 截断标记必须留给模型看（它是结果的一部分）：只说「已截断」会让模型以为数据就这么多，
// 说清「原长多少」它才知道可以换个更窄的条件再调一次。
func PruneToolResult(text string) (string, bool) {
	if r := []rune(text); len(r) > toolResultLimit {
		return string(r[:toolResultLimit]) + truncationNotice(len(r)), true
	}
	return text, false
}

// truncationNotice 截断标记（中文，进上下文给模型看）。
func truncationNotice(fullLen int) string {
	return "\n\n[结果过长已截断：完整结果共 " + strconv.Itoa(fullLen) + " 字，以上为前 " + strconv.Itoa(toolResultLimit) + " 字。需要更多请缩小查询范围后重新调用]"
}

// truncateRunes 按 rune 截断（不切断多字节字符）。
func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

// ToolErrorKeyOf 把结论分类映射到 i18n key（不需要面向用户文案时为空串）。
//
// 分类到 key 的映射只在这里：调用方（runTool）只报分类，不拼 key，
// 免得同一个结论在几处各写一个 key 然后慢慢分叉。
//
// args_error 刻意**不映射**：参数错是模型自己改参就能重试的事，
// 面向用户的文案（「工具执行失败」）在这里会误导 —— 用户什么都没做错。
// 后台要看这一类，读 status 列即可（它本来就是分类真源）。
func ToolErrorKeyOf(status aienums.ToolCallStatus) string {
	switch status {
	case aienums.ToolCallStatusForbidden:
		return aienums.ErrToolForbidden
	case aienums.ToolCallStatusFailed:
		return aienums.ErrToolRunFailed
	}
	return ""
}

// ToolIdempotencyStore 实现 mcp.IdempotencyStore。
type ToolIdempotencyStore struct {
	m *aimodel.ToolIdempotencyModel
}

// NewToolIdempotencyStore 构造。
func NewToolIdempotencyStore(m *aimodel.ToolIdempotencyModel) *ToolIdempotencyStore {
	return &ToolIdempotencyStore{m: m}
}

// Lookup 取同一次意图上次的结果。
func (s *ToolIdempotencyStore) Lookup(ctx context.Context, tool, key string) (mcp.Result, bool, error) {
	if s == nil || s.m == nil {
		// 没接台账时**不阻断**写操作：接入缺失是装配问题，而把它变成
		// 「所有写工具都不可用」会让排查方向指向工具本身。
		// 代价是这段时间没有幂等保护 —— 这是有意的取舍，见装配处的断言。
		return mcp.Result{}, false, nil
	}
	row, ok, err := s.m.Lookup(ctx, tool, key)
	if err != nil || !ok {
		return mcp.Result{}, false, err
	}
	return mcp.Result{Text: row.ResultText}, true, nil
}

// Save 记下这次成功的结果。
func (s *ToolIdempotencyStore) Save(ctx context.Context, tool, key string, res mcp.Result) error {
	if s == nil || s.m == nil {
		return nil
	}
	return s.m.Save(ctx, tool, key, res.Text)
}
