package feature

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"go_wp/internal/mcp"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/public/test/support"
)

func newAISessionService(t *testing.T) (*aiservice.SessionService, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)
	return aiservice.NewSessionService(aimodel.NewSessionModel(db)), db
}

func appendEvent(t *testing.T, svc *aiservice.SessionService, req aidto.AppendEventReq) *aidto.AppendEventResult {
	t.Helper()
	if req.UserID <= 0 {

		req.UserID = 1
	}
	res, err := svc.AppendEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("追加事件失败：%v", err)
	}
	return res
}

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

func longSegment(t *testing.T, svc *aiservice.SessionService, key string, count int) int64 {
	t.Helper()
	body := strings.Repeat("这是一段用来撑长度的历史内容。", 250)
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

func TestSessionIsolatedByOwner(t *testing.T) {
	svc, db := newAISessionService(t)
	ctx := context.Background()

	const (
		alice int64 = 1001
		bob   int64 = 1002
		key         = "admin-fab:1001"
	)

	sess, err := svc.EnsureSession(ctx, key, "openai", "gpt-x", "Alice 的会话", alice)
	if err != nil {
		t.Fatalf("Alice 建会话失败: %v", err)
	}
	if sess.CreateBy != alice {
		t.Fatalf("会话归属应为 %d，实际 %d", alice, sess.CreateBy)
	}

	if _, err := svc.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID: sess.ID, Kind: string(aienums.EventKindUser),
		Content: "ALICE-SECRET-QUESTION",
		UserID:  alice,
	}); err != nil {
		t.Fatalf("写 Alice 的用户事件失败: %v", err)
	}

	if _, err := svc.EnsureSession(ctx, key, "openai", "gpt-x", "Bob 想续写", bob); err == nil {
		t.Fatal("Bob 拿 Alice 的 key 建/续会话竟然成功了 —— 会话归属没有校验")
	}

	turns, err := svc.RecentDialogue(ctx, key, 10, bob)
	if err != nil {
		t.Fatalf("读历史不该报错（归属不符按无会话处理），实际: %v", err)
	}
	for _, turn := range turns {
		if strings.Contains(turn.Text, "ALICE-SECRET-QUESTION") {
			t.Fatalf("Bob 读到了 Alice 的对话历史：%q", turn.Text)
		}
	}
	if len(turns) != 0 {
		t.Fatalf("Bob 不该看到任何轮次，实际 %d 条", len(turns))
	}

	own, err := svc.RecentDialogue(ctx, key, 10, alice)
	if err != nil {
		t.Fatalf("Alice 读自己的历史失败: %v", err)
	}
	if len(own) == 0 {
		t.Fatal("Alice 读不到自己的对话历史")
	}

	var count int64
	if err := db.Model(&aimodel.AISessionEntity{}).Where("session_key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("统计会话数失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同一把 key 应只有 1 条会话，实际 %d", count)
	}
}

func TestToolIdempotencyIsolatedByActor(t *testing.T) {
	_, db := newAISessionService(t)
	store := aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))

	const (
		alice int64 = 2001
		bob   int64 = 2002
		tool        = "inventory.adjust"
		key         = "retry-1"
	)
	ctxFor := func(uid int64) context.Context { return mcp.WithUserID(context.Background(), uid) }

	if err := store.Save(ctxFor(alice), tool, key, mcp.Result{Text: "ALICE-RESULT"}); err != nil {
		t.Fatalf("Alice 落幂等记录失败: %v", err)
	}

	if res, ok, err := store.Lookup(ctxFor(bob), tool, key); err != nil {
		t.Fatalf("Bob 查幂等记录出错: %v", err)
	} else if ok {
		t.Fatalf("Bob 命中了 Alice 的幂等记录（result=%q）—— 跨用户回放，且他的写不会真的执行", res.Text)
	}

	if res, ok, err := store.Lookup(ctxFor(alice), tool, key); err != nil {
		t.Fatalf("Alice 查幂等记录出错: %v", err)
	} else if !ok || res.Text != "ALICE-RESULT" {
		t.Fatalf("Alice 应命中自己的记录，实际 ok=%v result=%q", ok, res.Text)
	}

	if err := store.Save(ctxFor(bob), tool, key, mcp.Result{Text: "BOB-RESULT"}); err != nil {
		t.Fatalf("Bob 用同一 key 落记录失败（唯一约束未按主体分作用域）: %v", err)
	}
	if res, ok, _ := store.Lookup(ctxFor(bob), tool, key); !ok || res.Text != "BOB-RESULT" {
		t.Fatalf("Bob 应读到自己那条，实际 ok=%v result=%q", ok, res.Text)
	}

	var count int64
	if err := db.Table("ai_tool_idempotency").
		Where("tool_name = ? AND idem_key = ?", tool, key).Count(&count).Error; err != nil {
		t.Fatalf("统计幂等记录失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("同一 (tool,key) 在两个 actor 下应有 2 条记录，实际 %d", count)
	}
}
