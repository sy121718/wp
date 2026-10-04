// ai_session_compact.go — 折叠：把一段已经不新鲜的上下文换成一条摘要。
//
// 折叠**不改写历史**：它往事件日志里追加三条事件 ——
//
//	compact_start（记账）、compact_summary（surface_op=replace，带被替换的序号区间）、compact_end（记账）。
//
// 投影看到 replace 指令后，把 [replace_from_seq, replace_to_seq] 整段从「当前上下文」里盖掉，
// 换上摘要那一格；被盖掉的原文**仍在事件日志里**，随时可展开、可审计、可重放。
//
// 净收益口径（docs/16 §2）：折叠本身就花 token（写摘要 + 打断前缀缓存），
// 所以只折「已经不新鲜且足够大」的段；净收益为负时 FoldPlan 直接给出 Worthwhile=false。
package aiservice

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
)

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
