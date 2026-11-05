package feature

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
)

// 本文件锁的是 ai 域四条「事实必须一致」的链路，全部走真实 PG（support.NewMigratedPGTestDB）：
//
//   1. 会话归属：EnsureSession / RecentDialogue / append（id 与 key 两条入口）按创建人隔离；
//   2. 失败轮次：外部调用失败要在库里留可重放的标记，而不是只剩一条孤立的 user 事件；
//   3. 用量聚合：同一屏的会话数 / 事件数 / token 出自同一批行（与整表、与逐条相加都对得上）；
//   4. 趋势桶键：按 UTC 切日，不让本地时区把最后一天挪一位。
//
// 「同一份快照」的并发证明不在这一层：它需要观察一个未提交事务内两次聚合的结果，
// 那种用例放在 model 同包（internal/module/ai/model/ai_session_snapshot_test.go），
// 直接调真实 SessionUsageOf 并用 GORM callback 注入一次并发写入。

func TestAISessionOwnershipIsolatedPerCreator(t *testing.T) {
	_, sess, _, _ := newSessionSendEnv(t)
	ctx := t.Context()

	owner, err := sess.EnsureSession(ctx, "owner-key", "sess-page", "muse", "我的会话", 11)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	// 同一个 key 换个人来续写：必须拒绝，而且用的是「不存在」而不是「没权限」——
	// 对外不能泄漏「这个 key 存在但不是你的」。
	if _, err := sess.EnsureSession(ctx, "owner-key", "sess-page", "muse", "别人的会话", 22); err != aiservice.ErrSessionNotFound {
		t.Fatalf("他人续写同一 key 应为 ErrSessionNotFound，实际 %v", err)
	}

	// 新建的会话不会被别人抢走：换个人建不同 key 是另一条会话。
	other, err := sess.EnsureSession(ctx, "other-key", "sess-page", "muse", "别人的会话", 22)
	if err != nil {
		t.Fatalf("他人建自己的会话应成功：%v", err)
	}
	if other.ID == owner.ID {
		t.Fatalf("两条会话不能是同一行：%d", other.ID)
	}
	if other.CreateBy != 22 || owner.CreateBy != 11 {
		t.Fatalf("创建人落库不对：owner=%d other=%d", owner.CreateBy, other.CreateBy)
	}

	// 同一 key 同一个人再来：续写同一条（不是新建）。
	again, err := sess.EnsureSession(ctx, "owner-key", "sess-page", "muse", "我的会话", 11)
	if err != nil {
		t.Fatalf("同人续写应成功：%v", err)
	}
	if again.ID != owner.ID {
		t.Fatalf("同人续写应命中同一行：got %d want %d", again.ID, owner.ID)
	}
}

// TestAISessionAppendRejectsOtherCreatorOnBothEntries 锁住 append 的两条入口都不能跨人。
//
// 两条入口的绕过方式不一样，所以必须分开测：
//
//	· 按 id —— 猜到 id 就能追加；
//	· 按 key —— 更危险，FAB 的 key 形如 admin-fab:<uid>、会话 key 又是「客户端给什么
//	  就存什么」，几乎可以猜。
//
// 拒绝之后要证明**什么都没写**：事件数、seq 游标、update_by 都不变 ——
// 只看返回错误码不够，事务里已经写了一部分再回滚与「根本没进去」是两件事。
func TestAISessionAppendRejectsOtherCreatorOnBothEntries(t *testing.T) {
	_, sess, _, db := newSessionSendEnv(t)
	ctx := t.Context()

	const ownerID, intruderID int64 = 11, 22
	head, err := sess.EnsureSession(ctx, "admin-fab:11", "sess-page", "muse", "我的会话", ownerID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	if _, err := sess.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID: head.ID, Kind: string(aienums.EventKindUser), Content: "第一句",
		UserID: ownerID, ProviderKey: "sess-page", ModelID: "muse",
	}); err != nil {
		t.Fatalf("本人追加应成功：%v", err)
	}

	snapshot := func() (events int64, nextSeq int64, updateBy int64) {
		t.Helper()
		var row struct {
			NextSeq  int64 `gorm:"column:next_seq"`
			UpdateBy int64 `gorm:"column:update_by"`
		}
		if err := db.Table("ai_session").Where("id = ?", head.ID).Take(&row).Error; err != nil {
			t.Fatalf("读会话失败：%v", err)
		}
		if err := db.Table("ai_event").Where("session_id = ?", head.ID).Count(&events).Error; err != nil {
			t.Fatalf("数事件失败：%v", err)
		}
		return events, row.NextSeq, row.UpdateBy
	}
	eventsBefore, seqBefore, updateByBefore := snapshot()

	// ① 按 id 追加。
	_, err = sess.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID: head.ID, Kind: string(aienums.EventKindUser), Content: "冒名",
		UserID: intruderID, ProviderKey: "sess-page", ModelID: "muse",
	})
	if err != aiservice.ErrSessionNotFound {
		t.Fatalf("他人按 id 追加应为 ErrSessionNotFound（不泄漏存在性），实际 %v", err)
	}

	// ② 按 key 追加 —— 这一条才是可猜的绕过口。
	_, err = sess.AppendEvent(ctx, aidto.AppendEventReq{
		SessionKey: "admin-fab:11", Kind: string(aienums.EventKindUser), Content: "冒名",
		UserID: intruderID, ProviderKey: "sess-page", ModelID: "muse",
	})
	if err != aiservice.ErrSessionNotFound {
		t.Fatalf("他人按 key 追加应为 ErrSessionNotFound，实际 %v", err)
	}

	eventsAfter, seqAfter, updateByAfter := snapshot()
	if eventsAfter != eventsBefore {
		t.Fatalf("被拒绝的追加不能落事件：%d -> %d", eventsBefore, eventsAfter)
	}
	if seqAfter != seqBefore {
		t.Fatalf("被拒绝的追加不能推进 seq 游标：%d -> %d", seqBefore, seqAfter)
	}
	if updateByAfter != updateByBefore {
		t.Fatalf("被拒绝的追加不能改 update_by：%d -> %d", updateByBefore, updateByAfter)
	}

	// 本人仍然能继续写：拒绝只挡住别人。
	if _, err := sess.AppendEvent(ctx, aidto.AppendEventReq{
		SessionKey: "admin-fab:11", Kind: string(aienums.EventKindUser), Content: "第二句",
		UserID: ownerID, ProviderKey: "sess-page", ModelID: "muse",
	}); err != nil {
		t.Fatalf("本人按 key 追加应成功：%v", err)
	}
	if eventsFinal, _, _ := snapshot(); eventsFinal != eventsAfter+1 {
		t.Fatalf("本人追加应落一条事件：%d -> %d", eventsAfter, eventsFinal)
	}
}

func TestAIRecentDialogueIsPerCreator(t *testing.T) {
	_, sess, _, _ := newSessionSendEnv(t)
	ctx := t.Context()

	head, err := sess.EnsureSession(ctx, "ctx-key", "sess-page", "muse", "对话", 11)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	if _, err := sess.AppendEvent(ctx, aidto.AppendEventReq{
		SessionID: head.ID, Kind: string(aienums.EventKindUser), Content: "只有创建人能看到", UserID: 11,
	}); err != nil {
		t.Fatalf("写事件失败：%v", err)
	}

	mine, err := sess.RecentDialogue(ctx, "ctx-key", 10, 11)
	if err != nil {
		t.Fatalf("创建人读对话失败：%v", err)
	}
	if len(mine) == 0 {
		t.Fatal("创建人应该读到自己的对话")
	}

	// 别人读同一个 key：拿不到任何一轮（不是报错 —— 不确认存在性）。
	others, err := sess.RecentDialogue(ctx, "ctx-key", 10, 22)
	if err != nil {
		t.Fatalf("他人读对话不该报错：%v", err)
	}
	if len(others) != 0 {
		t.Fatalf("他人不该读到别人的对话，实际 %d 轮", len(others))
	}
}

func TestAISessionFailedRoundIsPersistedAndReplayable(t *testing.T) {
	ph, sess, svc, _ := newSessionSendEnv(t)
	ctx := t.Context()

	// 上游 500：这一轮注定失败。
	sessionSendUpstreamJSON(t, svc, http.StatusInternalServerError, `{"error":"boom"}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(ctx, "fail-key", "sess-page", "muse", "会失败的会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+itoa(head.ID)+"&providerKey=sess-page&model=muse&input=会失败")
	assertAIFail(t, rec, http.StatusInternalServerError, "")

	// 库里要留一条「这轮失败了」的事实，而不是只有孤立的 user 事件。
	rounds, err := sess.FailedRounds(ctx, head.ID, 20)
	if err != nil {
		t.Fatalf("读失败轮次出错：%v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("应留 1 条失败标记，实际 %d 条：%+v", len(rounds), rounds)
	}
	if rounds[0].SessionID != head.ID || rounds[0].Seq <= 0 {
		t.Fatalf("失败标记定位不对：%+v", rounds[0])
	}
	// 标记里只能是**稳定的错误 key**：那是会经 GET /api/ai/session/events 出接口的事实，
	// 上游原文（URL、回显片段）只该进日志。这里断言它有内容、是 key 形状、且不含上游原文。
	if rounds[0].Reason == "" {
		t.Fatalf("失败标记应带错误 key，不能是空：%+v", rounds[0])
	}
	if !strings.Contains(rounds[0].Reason, "ai.err.") && rounds[0].Reason != aienums.ErrInternal {
		t.Fatalf("落库的应是稳定错误 key，实际 %q", rounds[0].Reason)
	}
	for _, leak := range []string{"boom", "500", "http"} {
		if strings.Contains(rounds[0].Reason, leak) {
			t.Fatalf("失败标记泄漏了上游原文（%q）：%q", leak, rounds[0].Reason)
		}
	}

	// 成功的那一轮不会出现在失败清单里。
	sessionSendUpstream(t, svc, "这次成功了")
	ok, err := sess.EnsureSession(ctx, "fail-key-2", "sess-page", "muse", "会成功的会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	rec = serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+itoa(ok.ID)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIOK(t, rec, "消息已发送，模型已回复")

	apart, err := sess.FailedRounds(ctx, ok.ID, 20)
	if err != nil {
		t.Fatalf("读失败轮次出错：%v", err)
	}
	if len(apart) != 0 {
		t.Fatalf("成功的会话不该有失败标记，实际 %d 条", len(apart))
	}
}

// TestAISessionEventMetaNeverLeaksRawReplyError 钉住「Events 出口不带原文」。
//
// replyError 是我们自己的诊断字段，而 meta 会原样随 GET /api/ai/session/events 出去。
// 新写入只放错误 key；**历史行**里存过 cause.Error() 原文（上游 URL、回显片段），
// 所以读侧投影也必须归一 —— 否则存量数据继续泄漏。这里直接往库里塞一条带原文的
// meta 行（模拟历史记录），再从两个出口读：Events 与 FailedRounds。
func TestAISessionEventMetaNeverLeaksRawReplyError(t *testing.T) {
	ph, sess, _, db := newSessionSendEnv(t)
	ctx := t.Context()

	const sentinel = "https://api.example.test/v1/chat?key=SECRET-SENTINEL-9f3a upstream said boom"
	head, err := sess.EnsureSession(ctx, "leak-key", "sess-page", "muse", "含原文的会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	// 模拟历史行：meta.replyError 是原文，同时带上真实业务内容（tool / reasoning / userText）。
	meta := aimodel.JSONMap{
		"replyState": "failed",
		"replyError": sentinel,
		"reasoning":  "先想一下",
		"userText":   "用户原文",
		"tool":       map[string]any{"name": "querytool", "status": "failed"},
	}
	row := aimodel.AIEventEntity{SessionID: head.ID, Seq: 1, Kind: "user", Content: "你好", Meta: meta}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("写历史事件失败：%v", err)
	}

	// 出口一：GET /api/ai/session/events —— 整份 meta 会进 JSON。
	rec := httptest.NewRecorder()
	serveSessionEvents(t, ph).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/ai/session/events?id="+strconv.FormatInt(head.ID, 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("读事件应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"SECRET-SENTINEL", "upstream said boom", "api.example.test"} {
		if strings.Contains(body, leak) {
			t.Fatalf("事件接口泄漏了 meta.replyError 原文（%q）：%s", leak, body)
		}
	}
	// 业务内容不能跟着一起被删：tool / reasoning / userText 必须还在。
	for _, want := range []string{"先想一下", "用户原文", "querytool"} {
		if !strings.Contains(body, want) {
			t.Errorf("meta 的业务内容 %q 被误删了：%s", want, body)
		}
	}
	if !strings.Contains(body, aienums.ErrInternal) {
		t.Errorf("replyError 应归一成稳定 key（%q），实际响应里没有：%s", aienums.ErrInternal, body)
	}

	// 出口二：FailedRounds —— 同一套归一，不能有第二个泄漏口。
	rounds, err := sess.FailedRounds(ctx, head.ID, 20)
	if err != nil {
		t.Fatalf("读失败轮次出错：%v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("应读到 1 条失败轮次，实际 %d 条", len(rounds))
	}
	if rounds[0].Reason != aienums.ErrInternal {
		t.Fatalf("原文应归一成 %q，实际 %q", aienums.ErrInternal, rounds[0].Reason)
	}
}

// TestAISessionEventMetaStripsReplyErrorTail 钉住 `key|tail` 形态的历史行。
//
// facing.IsBusinessKey 的正则允许 `(\|.*)?`，所以 `ai.err.internal|SECRET-TAIL` 整串
// 会被判成「合法业务 key」—— 直接原样返回就等于把 tail 段（历史行里可能是上游原文）
// 继续从 Events / FailedRounds 透出去。读侧必须先按第一个 `|` 切，只交 key 段。
func TestAISessionEventMetaStripsReplyErrorTail(t *testing.T) {
	ph, sess, _, db := newSessionSendEnv(t)
	ctx := t.Context()

	const tail = "SECRET-TAIL-4b71 upstream said boom"
	head, err := sess.EnsureSession(ctx, "tail-key", "sess-page", "muse", "带 tail 的会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	// 历史行形态：key 前缀合法，后面跟 `|` 与原文。
	meta := aimodel.JSONMap{
		"replyState": "failed",
		"replyError": aienums.ErrInternal + "|" + tail,
	}
	row := aimodel.AIEventEntity{SessionID: head.ID, Seq: 1, Kind: "user", Content: "你好", Meta: meta}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("写历史事件失败：%v", err)
	}

	rec := httptest.NewRecorder()
	serveSessionEvents(t, ph).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/ai/session/events?id="+strconv.FormatInt(head.ID, 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("读事件应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"SECRET-TAIL", "upstream said boom"} {
		if strings.Contains(body, leak) {
			t.Fatalf("Events 透出了 replyError 的 tail（%q）：%s", leak, body)
		}
	}
	if !strings.Contains(body, aienums.ErrInternal) {
		t.Errorf("应只交 key 段 %q，实际响应里没有：%s", aienums.ErrInternal, body)
	}

	// FailedRounds 是第二个出口，同一套规则必须成立。
	rounds, err := sess.FailedRounds(ctx, head.ID, 20)
	if err != nil {
		t.Fatalf("读失败轮次出错：%v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("应读到 1 条失败轮次，实际 %d 条", len(rounds))
	}
	if rounds[0].Reason != aienums.ErrInternal {
		t.Fatalf("tail 应被切掉，只留 key %q，实际 %q", aienums.ErrInternal, rounds[0].Reason)
	}
	for _, leak := range []string{"SECRET-TAIL", "upstream said boom", "|"} {
		if strings.Contains(rounds[0].Reason, leak) {
			t.Fatalf("失败轮次事实透出了 %q：%q", leak, rounds[0].Reason)
		}
	}
}

// serveSessionEvents 把 SessionHandle.Events 挂到与生产同形状的路由上。
// 单独一个入口，是因为本用例要读的是 JSON 出口，不是页面。
func serveSessionEvents(t *testing.T, ph *aihttp.SessionHandle) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", sessionSendUserID)
		c.Next()
	})
	engine.GET("/api/ai/session/events", ph.Events)
	return engine
}

func TestAISessionUsageComesFromOneSnapshot(t *testing.T) {
	_, sess, svc, db := newSessionSendEnv(t)
	ctx := t.Context()

	sessionSendUpstream(t, svc, "模型回复")
	newSessionSendProvider(t, svc)

	// 三条会话、各自一轮：合计口径要能与逐条相加对上，说明四段聚合看的是同一批行。
	var ids []int64
	for i := 0; i < 3; i++ {
		head, err := sess.EnsureSession(ctx, "snap-key-"+itoa(int64(i)), "sess-page", "muse", "会话", sessionSendUserID)
		if err != nil {
			t.Fatalf("建会话失败：%v", err)
		}
		ids = append(ids, head.ID)
		if _, err := sess.SendMessage(ctx, aidto.SendMessageReq{
			SessionID: head.ID, ProviderKey: "sess-page", Model: "muse", Input: "你好",
			UserID: sessionSendUserID,
		}); err != nil {
			t.Fatalf("发消息失败：%v", err)
		}
	}

	usage, err := sess.SessionUsageOf(ctx, aidto.SessionQuery{Status: -1})
	if err != nil {
		t.Fatalf("读用量失败：%v", err)
	}
	if usage.Sessions < 3 {
		t.Fatalf("会话数至少 3，实际 %d", usage.Sessions)
	}
	if usage.Events == 0 || usage.Tokens == 0 {
		t.Fatalf("同一屏的事件数与 token 不该为 0：%+v", usage)
	}

	// 事件数与 token 都要和「直接数表」对上：只断言「不为 0」证明不了四段来自同一批行。
	var whole struct {
		Events int64 `gorm:"column:events"`
		Tokens int64 `gorm:"column:tokens"`
	}
	if err := db.Table("ai_event").
		Select("COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Take(&whole).Error; err != nil {
		t.Fatalf("数事件失败：%v", err)
	}
	if usage.Events != whole.Events {
		t.Fatalf("事件数应与表内行数相等：usage=%d, 表=%d", usage.Events, whole.Events)
	}
	if usage.Tokens != whole.Tokens {
		t.Fatalf("token 总量应与表内合计相等：usage=%d, 表=%d", usage.Tokens, whole.Tokens)
	}

	// 逐条相加也必须等于总量，且事件数与 token 两个量都要对上。
	var sumEvents int64
	var sumTokens int64
	for _, id := range ids {
		detail, err := sess.GetSession(ctx, id)
		if err != nil {
			t.Fatalf("读会话 %d 失败：%v", id, err)
		}
		sumEvents += int64(detail.VisibleCount)
		sumTokens += detail.VisibleTokens
	}
	if sumEvents == 0 {
		t.Fatal("三条会话合计事件数不该是 0")
	}
	if sumEvents != whole.Events {
		t.Fatalf("逐条事件数相加应等于表内行数：相加=%d, 表=%d", sumEvents, whole.Events)
	}
	if sumTokens != usage.Tokens {
		t.Fatalf("token 总量与逐条相加不一致：total=%d, 相加=%d（四段聚合读到的不是同一份快照？）",
			usage.Tokens, sumTokens)
	}
	// 压缩成本的分母就是 token 总量：两者必须出自同一份快照，否则百分比会飘。
	if usage.CompactCostPct < 0 {
		t.Fatalf("压缩成本比例不能为负：%+v", usage)
	}

	// 读用时不该留下任何写入（ReadOnly 事务）。
	var before, after int64
	if err := db.Table("ai_session").Count(&before).Error; err != nil {
		t.Fatalf("数会话失败：%v", err)
	}
	if _, err := sess.SessionUsageOf(ctx, aidto.SessionQuery{Status: -1}); err != nil {
		t.Fatalf("二次读用量失败：%v", err)
	}
	if err := db.Table("ai_session").Count(&after).Error; err != nil {
		t.Fatalf("数会话失败：%v", err)
	}
	if before != after {
		t.Fatalf("读用量不该改变行数：%d -> %d", before, after)
	}
}

func TestAISessionTokenTrendBucketsByUTC(t *testing.T) {
	_, sess, svc, db := newSessionSendEnv(t)
	ctx := t.Context()

	sessionSendUpstream(t, svc, "模型回复")
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(ctx, "trend-key", "sess-page", "muse", "趋势", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	if _, err := sess.SendMessage(ctx, aidto.SendMessageReq{
		SessionID: head.ID, ProviderKey: "sess-page", Model: "muse", Input: "你好",
		UserID: sessionSendUserID,
	}); err != nil {
		t.Fatalf("发消息失败：%v", err)
	}

	// 关键反例：把进程时区换到 UTC+8。
	// 纯 UTC 环境里「用本地时区」和「用 UTC」结果相同，旧实现也能过 ——
	// 只有进程时区非 UTC 时，桶键错误才会暴露（UTC 00:30 在东八区是当天 08:30，
	// 若用本地日切，这一笔反而会落到 03-10；但若用本地日切又要按 UTC 显示，
	// 起点会整体偏移一天）。这里显式设定并还原，避免污染同包其它用例。
	origLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = origLocal })

	// 把落库时刻钉在 UTC 的 00:30 —— 只要桶键用本地时区，这一笔就会落到前一天。
	utcDay := time.Date(2026, 3, 10, 0, 30, 0, 0, time.UTC)
	if err := db.Exec("UPDATE ai_event SET create_time = ?", utcDay).Error; err != nil {
		t.Fatalf("改事件时间失败：%v", err)
	}
	if err := db.Exec("UPDATE ai_session SET create_time = ? WHERE id = ?", utcDay, head.ID).Error; err != nil {
		t.Fatalf("改会话时间失败：%v", err)
	}

	// 取 5 天窗口：03-08 .. 03-12。桶键若用本地时区，这一笔会落到 03-09 或 03-11，
	// 于是「峰值出现在哪一天」就能把落桶错误暴露出来。
	trend, err := sess.SessionTrend(ctx, aidto.SessionQuery{Status: -1, From: "2026-03-08", To: "2026-03-12"}, 5)
	if err != nil {
		t.Fatalf("读趋势失败：%v", err)
	}
	if trend.To != "2026-03-12" || trend.From != "2026-03-08" {
		t.Fatalf("窗口应为 03-08..03-12（UTC 日），实际 %s..%s", trend.From, trend.To)
	}
	if trend.Peak <= 0 {
		t.Fatalf("应有一笔 token 构成峰值，实际 peak=%d", trend.Peak)
	}
	if len(trend.Series) == 0 {
		t.Fatal("趋势不该为空")
	}

	// 峰值那一笔必须落在窗口里的第 3 天（索引 2 = 03-10）。
	// 服务端只交逐日 token 数组（Values），坐标由 ai-trend.js 现算 ——
	// 落错桶时这个索引会变成 1 或 3，数组里一眼就看得出。
	series := trend.Series[0]
	if series.Total != trend.Peak {
		t.Fatalf("单系列时该系列总量应等于峰值：total=%d peak=%d", series.Total, trend.Peak)
	}
	if len(series.Values) != 5 {
		t.Fatalf("5 天窗口应有 5 个桶，实际 %d：%v", len(series.Values), series.Values)
	}
	peakIdx := -1
	for i, v := range series.Values {
		if v > 0 {
			if peakIdx >= 0 {
				t.Fatalf("窗口里只该有一天有 token，实际多天：%v", series.Values)
			}
			peakIdx = i
		}
	}
	if peakIdx != 2 {
		t.Fatalf("这笔 token 应落在第 3 个桶（2026-03-10，UTC），实际索引 %d：%v", peakIdx, series.Values)
	}
}
