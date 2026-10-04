package feature

// ai_session_test.go — AI 会话层的投影 / 折叠 / 计量 / 并发序号。
//
// 表结构来自生产迁移（support.NewMigratedPGTestDB → 512_ai_session.sql + 生产种子），
// 不手抄 CREATE TABLE。
//
// 这一层钉住四件事（任务书 B4）：
//   - **投影正确性**：surface_op=append 全进；surface_op=replace 的 [from,to] 区间被它自己盖掉；
//     compact_start / compact_end 只是记账标记，不进投影；
//   - **折叠可逆可审计**：折叠只改「当前上下文」，原始事件一条不少地留在 ai_event；
//   - **折叠后计量是重算值**：context_tokens 等于折叠后投影的 token 和（绝对值重算，不是自减），
//     且 compact_count +1；
//   - **并发追加不撞号**：next_seq 由事务内 UPDATE … RETURNING 分配，并发下序号唯一。
//
// 另含 EnsureSession 的并发同 key 归因（命中唯一约束后重查取回，不产生第二条会话）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/public/test/support"
)

// newAISessionService 建一个跑过生产迁移与种子的库 + 会话服务。
func newAISessionService(t *testing.T) (*aiservice.SessionService, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)
	return aiservice.NewSessionService(aimodel.NewSessionModel(db)), db
}

// appendEvent 追加一条事件，失败即终止。
func appendEvent(t *testing.T, svc *aiservice.SessionService, req aidto.AppendEventReq) *aidto.AppendEventResult {
	t.Helper()
	res, err := svc.AppendEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("追加事件失败：%v", err)
	}
	return res
}

// TestAISessionAppendProjectsEveryVisibleEvent：全是 append 时投影 = 全部事件，序号从 1 连续。
func TestAISessionAppendProjectsEveryVisibleEvent(t *testing.T) {
	svc, _ := newAISessionService(t)
	ctx := context.Background()

	var sid int64
	for i := 1; i <= 3; i++ {
		r := appendEvent(t, svc, aidto.AppendEventReq{
			SessionKey: "proj-1", Kind: string(aienums.EventKindUser),
			Content: fmt.Sprintf("第 %d 条", i), UserID: 1,
		})
		sid = r.Session.ID
		if r.Seq != int64(i) {
			t.Fatalf("第 %d 次追加的序号应为 %d，实际 %d", i, i, r.Seq)
		}
	}

	detail, err := svc.GetSession(ctx, sid)
	if err != nil {
		t.Fatalf("取会话失败：%v", err)
	}
	if detail.VisibleCount != 3 || len(detail.Items) != 3 {
		t.Fatalf("投影应有 3 条可见事件，实际 VisibleCount=%d Items=%d", detail.VisibleCount, len(detail.Items))
	}
	for i, it := range detail.Items {
		if it.Seq != int64(i+1) {
			t.Fatalf("第 %d 项序号应为 %d，实际 %d", i, i+1, it.Seq)
		}
		if it.Folded {
			t.Fatalf("未折叠时不应出现折叠标记（seq=%d）", it.Seq)
		}
	}
	if detail.ContextTokens != detail.VisibleTokens {
		t.Fatalf("会话计量 %d 应等于投影 token 和 %d", detail.ContextTokens, detail.VisibleTokens)
	}
}

// longSegment 造一段足够长的历史，使「剪掉 keep 之后的那截」达到折叠门槛。
func longSegment(t *testing.T, svc *aiservice.SessionService, key string, count int) int64 {
	t.Helper()
	body := strings.Repeat("这是一段用来撑长度的历史内容。", 250) // ≈4000 字符 ≈1334 token/条
	var sid int64
	for i := 1; i <= count; i++ {
		r := appendEvent(t, svc, aidto.AppendEventReq{
			SessionKey: key, Kind: string(aienums.EventKindUser),
			Content: fmt.Sprintf("%s#%d", body, i), UserID: 1,
		})
		sid = r.Session.ID
	}
	return sid
}

// TestAISessionFoldHidesRangeAndKeepsRawLog：折叠后投影只剩「摘要块 + 尾部」，
// 但事件日志里 20+3 条一条不少（可逆可审计）。
func TestAISessionFoldHidesRangeAndKeepsRawLog(t *testing.T) {
	svc, _ := newAISessionService(t)
	ctx := context.Background()

	const total = 24
	const keep = 12
	sid := longSegment(t, svc, "fold-1", total)

	before, err := svc.GetSession(ctx, sid)
	if err != nil {
		t.Fatalf("折叠前取会话失败：%v", err)
	}

	plan, err := svc.FoldPlan(ctx, aidto.FoldPlanReq{SessionID: sid, KeepRecent: keep})
	if err != nil {
		t.Fatalf("折叠预览失败：%v", err)
	}
	if !plan.Worthwhile {
		t.Fatalf("历史足够长时应判为值得折叠，实际 reason=%s（segmentTokens=%d）", plan.Reason, plan.SegmentTokens)
	}
	if plan.FromSeq != 1 || plan.ToSeq != int64(total-keep) {
		t.Fatalf("建议区间应为 1..%d，实际 %d..%d", total-keep, plan.FromSeq, plan.ToSeq)
	}

	res, err := svc.Fold(ctx, aidto.FoldReq{
		SessionID: sid, FromSeq: plan.FromSeq, ToSeq: plan.ToSeq,
		Summary: "前十二条的摘要", SummaryTokens: 200, UserID: 1,
	})
	if err != nil {
		t.Fatalf("折叠失败：%v", err)
	}
	if res.StartSeq == 0 || res.SummarySeq <= res.StartSeq || res.EndSeq <= res.SummarySeq {
		t.Fatalf("三条折叠事件的序号应递增，实际 start=%d summary=%d end=%d", res.StartSeq, res.SummarySeq, res.EndSeq)
	}

	detail, err := svc.GetSession(ctx, sid)
	if err != nil {
		t.Fatalf("折叠后取会话失败：%v", err)
	}
	// 投影 = 1 个摘要块（覆盖 1..12）+ 尾部 12 条。
	if len(detail.Items) != keep+1 {
		t.Fatalf("投影应为 %d 项（1 摘要 + %d 尾），实际 %d", keep+1, keep, len(detail.Items))
	}
	head := detail.Items[0]
	if !head.Folded || head.FoldedFrom != 1 || head.FoldedTo != int64(total-keep) {
		t.Fatalf("首项应是覆盖 1..%d 的折叠块，实际 Folded=%v %d..%d", total-keep, head.Folded, head.FoldedFrom, head.FoldedTo)
	}
	if head.Content != "前十二条的摘要" {
		t.Fatalf("折叠块内容应是摘要，实际 %q", head.Content)
	}
	for _, it := range detail.Items[1:] {
		if it.Seq <= int64(total-keep) {
			t.Fatalf("被折区间内的事件不应出现在投影里（seq=%d）", it.Seq)
		}
		if it.Folded {
			t.Fatalf("尾部事件不应带折叠标记（seq=%d）", it.Seq)
		}
	}

	// 可审计：原始日志 = 24 条业务事件 + 3 条折叠记账事件，全在。
	events, evTotal, err := svc.ListEvents(ctx, sid, 1, 200)
	if err != nil {
		t.Fatalf("取事件日志失败：%v", err)
	}
	if evTotal != total+3 {
		t.Fatalf("事件日志应为 %d 条，实际 %d", total+3, evTotal)
	}
	kinds := map[string]int{}
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	for _, want := range []string{
		string(aienums.EventKindCompactStart),
		string(aienums.EventKindCompactSummary),
		string(aienums.EventKindCompactEnd),
	} {
		if kinds[want] != 1 {
			t.Fatalf("事件日志应含 1 条 %s，实际 %d", want, kinds[want])
		}
	}
	if kinds[string(aienums.EventKindUser)] != total {
		t.Fatalf("原始用户事件应保留 %d 条，实际 %d", total, kinds[string(aienums.EventKindUser)])
	}

	// 折叠后计量必须变小，且等于投影 token 和（重算值）。
	if detail.ContextTokens >= before.ContextTokens {
		t.Fatalf("折叠后计量应下降：前 %d 后 %d", before.ContextTokens, detail.ContextTokens)
	}
	if detail.ContextTokens != detail.VisibleTokens {
		t.Fatalf("会话计量 %d 应等于折叠后投影 token 和 %d", detail.ContextTokens, detail.VisibleTokens)
	}
	if detail.CompactCount != 1 {
		t.Fatalf("压缩次数应为 1，实际 %d", detail.CompactCount)
	}
}

// TestAISessionFoldPlanRefusesShortHistory：可见项不足 keep 时不给折叠建议。
func TestAISessionFoldPlanRefusesShortHistory(t *testing.T) {
	svc, _ := newAISessionService(t)
	ctx := context.Background()

	r := appendEvent(t, svc, aidto.AppendEventReq{
		SessionKey: "fold-short", Kind: string(aienums.EventKindUser), Content: "只有一条", UserID: 1,
	})
	plan, err := svc.FoldPlan(ctx, aidto.FoldPlanReq{SessionID: r.Session.ID})
	if err != nil {
		t.Fatalf("折叠预览失败：%v", err)
	}
	if plan.Worthwhile {
		t.Fatalf("历史过短时不应建议折叠，实际 reason=%s", plan.Reason)
	}
	if plan.Reason != aiservice.FoldReasonNoSegment {
		t.Fatalf("理由应为 %s，实际 %s", aiservice.FoldReasonNoSegment, plan.Reason)
	}
}

// TestAISessionConcurrentAppendKeepsSeqUnique：并发追加的序号必须唯一且连续。
func TestAISessionConcurrentAppendKeepsSeqUnique(t *testing.T) {
	svc, _ := newAISessionService(t)
	ctx := context.Background()

	seed := appendEvent(t, svc, aidto.AppendEventReq{
		SessionKey: "conc-1", Kind: string(aienums.EventKindNote), Content: "起点", UserID: 1,
	})
	sid := seed.Session.ID

	const n = 16
	seqs := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.AppendEvent(ctx, aidto.AppendEventReq{
				SessionID: sid, Kind: string(aienums.EventKindNote),
				Content: fmt.Sprintf("并发 %d", i), UserID: 1,
			})
			if err != nil {
				errs[i] = err
				return
			}
			seqs[i] = res.Seq
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发追加第 %d 个失败：%v", i, err)
		}
		if seen[seqs[i]] {
			t.Fatalf("序号撞号：%d", seqs[i])
		}
		seen[seqs[i]] = true
	}
	// 起点 1 条 + 并发 n 条 → 序号应是 2..n+1。
	for want := int64(2); want <= int64(n+1); want++ {
		if !seen[want] {
			t.Fatalf("缺少序号 %d（已得 %v）", want, seen)
		}
	}
	detail, err := svc.GetSession(ctx, sid)
	if err != nil {
		t.Fatalf("取会话失败：%v", err)
	}
	if detail.EventCount != int64(n+1) {
		t.Fatalf("事件数应为 %d，实际 %d", n+1, detail.EventCount)
	}
}

// TestAISessionConcurrentEnsureSameKeyKeepsSingleSession：同一个会话标识被并发首次写入时，
// 只允许产生一条会话（命中唯一约束后重查取回，不静默拆成两条）。
func TestAISessionConcurrentEnsureSameKeyKeepsSingleSession(t *testing.T) {
	svc, _ := newAISessionService(t)
	ctx := context.Background()

	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.AppendEvent(ctx, aidto.AppendEventReq{
				SessionKey: "race-key", Kind: string(aienums.EventKindNote),
				Content: fmt.Sprintf("并发建会话 %d", i), UserID: 1,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发建会话第 %d 个失败：%v", i, err)
		}
	}

	_, total, err := svc.ListSessions(ctx, "race-key", -1, 1, 50)
	if err != nil {
		t.Fatalf("列会话失败：%v", err)
	}
	if total != 1 {
		t.Fatalf("同一 key 的并发写入应只留一条会话，实际 %d 条", total)
	}
}
