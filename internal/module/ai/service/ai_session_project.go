// ai_session_project.go — 投影：把事件日志算成「当前上下文」。
//
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
package aiservice

import (
	"context"
	"sort"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
)

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
