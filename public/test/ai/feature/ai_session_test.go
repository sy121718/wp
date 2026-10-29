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

	"go_wp/internal/mcp"
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
	if req.UserID <= 0 {
		// 会话层的第一关卡要求有身份（ErrUserRequired）；用例默认以一个已登录账号发起，
		// 需要覆盖「无身份」的用例自己显式传 0 并断言哨兵。
		req.UserID = 1
	}
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

// TestSessionIsolatedByOwner 同一把 session_key 在**不同用户**手里不能互相读到。
//
// 守的缺陷形态（SEC-05）：ai_session.session_key 是**全局唯一**，而 EnsureSession 与
// RecentDialogue 此前只按 key 查、不比对 create_by —— 悬浮球那条固定 key
// （admin-fab）于是成了所有人共用的一条会话：A 问过的问题，B 打开面板就看到全部历史。
// 代码注释写着「按 userID 隔离」，实现里没有；这类"注释承诺了、代码没做"的缺陷
// 只有断真实数据才抓得到。
//
// 两层防线各一条判据：
//  1. EnsureSession：别人拿着同一把 key 续写，必须拿不到那条会话；
//  2. RecentDialogue：即便 key 被猜到，也读不出别人的对话历史。
func TestSessionIsolatedByOwner(t *testing.T) {
	svc, db := newAISessionService(t)
	ctx := context.Background()

	const (
		alice int64 = 1001
		bob   int64 = 1002
		key         = "admin-fab:1001" // Alice 派生出来的 key（Bob 不该能用）
	)

	// Alice 建会话并落一轮对话。
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

	// ① Bob 用同一把 key 续写：必须被拒（归属不符按「不存在」处理）。
	if _, err := svc.EnsureSession(ctx, key, "openai", "gpt-x", "Bob 想续写", bob); err == nil {
		t.Fatal("Bob 拿 Alice 的 key 建/续会话竟然成功了 —— 会话归属没有校验")
	}

	// ② Bob 读历史：必须是空（不是 Alice 的内容，也不报错泄露"这把 key 有主"）。
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

	// ③ Alice 自己读得到（证明上面不是"谁都读不到"）。
	own, err := svc.RecentDialogue(ctx, key, 10, alice)
	if err != nil {
		t.Fatalf("Alice 读自己的历史失败: %v", err)
	}
	if len(own) == 0 {
		t.Fatal("Alice 读不到自己的对话历史")
	}

	// ④ 会话总数仍是 1：Bob 那次失败没有偷偷建出第二条。
	var count int64
	if err := db.Model(&aimodel.AISessionEntity{}).Where("session_key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("统计会话数失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同一把 key 应只有 1 条会话，实际 %d", count)
	}
}

// TestToolIdempotencyIsolatedByActor 幂等键必须按调用主体隔离（SEC-05）。
//
// 缺陷形态：幂等台账的唯一约束原先是 (tool_name, idem_key)，**不含主体**。
// idempotencyKey 由客户端生成（MCP 客户端 / 模型自拟的 uuid、retry-1 之类），
// 撞键完全可能。于是后一个用户用同一个 key 时：
//
//	· 拿到前一个人的写结果（跨用户回放，result_text 可能含业务数据）；
//	· 且**不真正执行写入** —— 他以为成功了，实际什么都没写。
//
// 判据：同一 (tool, key) 在两个 actor 下各自独立执行、各自落一条记录。
func TestToolIdempotencyIsolatedByActor(t *testing.T) {
	_, db := newAISessionService(t)
	store := aiservice.NewToolIdempotencyStore(aimodel.NewToolIdempotencyModel(db))

	const (
		alice int64 = 2001
		bob   int64 = 2002
		tool        = "inventory.adjust"
		key         = "retry-1" // 客户端固定用它，撞键很现实
	)
	ctxFor := func(uid int64) context.Context { return mcp.WithUserID(context.Background(), uid) }

	// Alice 执行并落结果。
	if err := store.Save(ctxFor(alice), tool, key, mcp.Result{Text: "ALICE-RESULT"}); err != nil {
		t.Fatalf("Alice 落幂等记录失败: %v", err)
	}

	// ① Bob 用**同一个 key**：不能命中 Alice 的记录。
	if res, ok, err := store.Lookup(ctxFor(bob), tool, key); err != nil {
		t.Fatalf("Bob 查幂等记录出错: %v", err)
	} else if ok {
		t.Fatalf("Bob 命中了 Alice 的幂等记录（result=%q）—— 跨用户回放，且他的写不会真的执行", res.Text)
	}

	// ② Alice 自己仍然命中（证明隔离不是"谁都查不到"）。
	if res, ok, err := store.Lookup(ctxFor(alice), tool, key); err != nil {
		t.Fatalf("Alice 查幂等记录出错: %v", err)
	} else if !ok || res.Text != "ALICE-RESULT" {
		t.Fatalf("Alice 应命中自己的记录，实际 ok=%v result=%q", ok, res.Text)
	}

	// ③ Bob 自己写一条同 key：必须成功（唯一约束按主体分作用域）。
	if err := store.Save(ctxFor(bob), tool, key, mcp.Result{Text: "BOB-RESULT"}); err != nil {
		t.Fatalf("Bob 用同一 key 落记录失败（唯一约束未按主体分作用域）: %v", err)
	}
	if res, ok, _ := store.Lookup(ctxFor(bob), tool, key); !ok || res.Text != "BOB-RESULT" {
		t.Fatalf("Bob 应读到自己那条，实际 ok=%v result=%q", ok, res.Text)
	}

	// ④ 落库确实是两条（不是被 ON CONFLICT 吃掉一条）。
	var count int64
	if err := db.Table("ai_tool_idempotency").
		Where("tool_name = ? AND idem_key = ?", tool, key).Count(&count).Error; err != nil {
		t.Fatalf("统计幂等记录失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("同一 (tool,key) 在两个 actor 下应有 2 条记录，实际 %d", count)
	}
}
