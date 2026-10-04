// ai_session_append.go — 追加事件：会话层唯一的写入口。
package aiservice

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
)

// AppendEvent 追加一条事件：定位或新建会话 → 分配序号 → 写事件 → 推进会话计量，全部在一个事务里。
//
// 四条保证：
//
//  1. 序号由事务内的 UPDATE … RETURNING 分配，并发追加不会撞号，回滚也不留空号；
//  2. 事件与计量（head_seq / context_tokens）同进同退，不会出现「日志里有、计数没动」；
//  3. 首次追加时**建会话与写事件同事务**：事务失败不会留下「0 事件空会话」脏数据；
//  4. **只有 append 一个写入口** —— 事件是 append-only 的，改写历史一律靠新事件表达
//     （surface_op=replace，见 ai_session_compact.go），谁都不能 UPDATE/DELETE 已有事件。
func (s *SessionService) AppendEvent(ctx context.Context, req aidto.AppendEventReq) (*aidto.AppendEventResult, error) {
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
			Meta:          aimodel.JSONMap(req.Meta),
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
