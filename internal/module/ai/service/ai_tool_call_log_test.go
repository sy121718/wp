// ai_tool_call_log_test.go — 工具调用审计与结果剪枝。
//
// 两个关注点合在一个文件里：**模型看到的文本**（剪枝）与**审计记下的那一行**同源，
// 分开写会漏掉「剪枝后长度与审计长度对不上」这类只有两边一起看才成立的断言。
package aiservice_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
)

// stubToolCallLogWriter 假审计写入端口：把落库的行留在内存里供断言。
type stubToolCallLogWriter struct {
	mu   sync.Mutex
	rows []*aimodel.AIToolCallLogEntity
}

func (w *stubToolCallLogWriter) Insert(_ context.Context, e *aimodel.AIToolCallLogEntity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rows = append(w.rows, e)
	return nil
}

// rowsSnapshot 取当前已落库的行（副本，避免与写入协程竞争）。
func (w *stubToolCallLogWriter) rowsSnapshot() []*aimodel.AIToolCallLogEntity {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*aimodel.AIToolCallLogEntity, len(w.rows))
	copy(out, w.rows)
	return out
}

// waitToolCallRows 等审计行数到位。
//
// 审计是**协程异步写**（与调用流水同一纪律：不阻塞用户等回复），
// 所以断言前必须等，而不是当场读 —— 当场读会变成一条随机失败的用例。
func waitToolCallRows(t *testing.T, w *stubToolCallLogWriter, want int) []*aimodel.AIToolCallLogEntity {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rows := w.rowsSnapshot(); len(rows) >= want {
			return rows
		}
		time.Sleep(5 * time.Millisecond)
	}
	rows := w.rowsSnapshot()
	t.Fatalf("等审计行数 %d 超时，实际 %d 行", want, len(rows))
	return nil
}

// toolLogArgs 造一条带工具调用的会话，并挂上审计记录器。
func toolLogArgs(t *testing.T, text string, status aienums.ToolCallStatus) (*aiservice.SessionService, *stubToolCallLogWriter) {
	t.Helper()
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	writer := &stubToolCallLogWriter{}
	sess.SetToolCallLogWriter(writer)
	sess.SetToolProvider(&stubToolProvider{
		specs:  []aidto.ToolSpec{ordersSummarySpec()},
		text:   text,
		status: status,
	})
	sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "orders_summary", `{"projectId":"p1"}`),
		`{"object":"response","status":"completed","output_text":"好的"}`,
	)
	return sess, writer
}

// TestRunToolWritesAuditRow 一次成功的工具调用要留一行审计，且字段与「模型看到的」一致。
func TestRunToolWritesAuditRow(t *testing.T) {
	const reply = "区间内 120 单"
	sess, writer := toolLogArgs(t, reply, aienums.ToolCallStatusOK)

	res := sendWithTools(t, sess, "tool-log-key-1")
	if len(res.ToolEvents) != 2 {
		t.Fatalf("工具事件应为 2 条（调用 + 结果），实际 %d", len(res.ToolEvents))
	}

	rows := waitToolCallRows(t, writer, 1)
	row := rows[0]
	if row.ToolName != "orders_summary" {
		t.Errorf("工具名应为 orders_summary，实际 %q", row.ToolName)
	}
	if row.UserID != testUserID {
		t.Errorf("账号应为 %d，实际 %d", testUserID, row.UserID)
	}
	if row.SessionID <= 0 {
		t.Errorf("会话 id 应已落库（>0），实际 %d", row.SessionID)
	}
	if row.Status != string(aienums.ToolCallStatusOK) {
		t.Errorf("状态应为 ok，实际 %q", row.Status)
	}
	if row.ErrorKey != "" {
		t.Errorf("成功时不该有 error_key，实际 %q", row.ErrorKey)
	}
	if row.Truncated {
		t.Error("短结果不该被标记为已剪枝")
	}
	// 摘要里要能看到模型给的参数（审计要回答「它按什么条件查的」）。
	if !strings.Contains(row.ArgumentsSummary, "p1") {
		t.Errorf("参数摘要应含 p1，实际 %q", row.ArgumentsSummary)
	}
	// 结果长度按 rune 计（中文一个字算一个）：这是「结果多大」的口径。
	if row.ResultLen != int64(len([]rune(reply))) {
		t.Errorf("结果长度应为 %d，实际 %d", len([]rune(reply)), row.ResultLen)
	}
	if row.ResultSummary != reply {
		t.Errorf("短结果应原样进摘要，实际 %q", row.ResultSummary)
	}
}

// TestRunToolAuditMarksTruncated 超长结果要被剪枝，且审计同时记下「剪过」与「原文多大」。
//
// 这是剪枝唯一的可判定证据：只看事件文本会以为工具就返回这么多，
// 只看审计长度又不知道模型看到的是哪一截。
func TestRunToolAuditMarksTruncated(t *testing.T) {
	const full = 5000 // 明显超过剪枝上限（4000），用 rune 计
	sess, writer := toolLogArgs(t, strings.Repeat("单", full), aienums.ToolCallStatusOK)

	res := sendWithTools(t, sess, "tool-log-key-2")
	// 事件里的正文是**剪枝后**的文本（进上下文的就是它）。
	if len(res.ToolEvents) != 2 {
		t.Fatalf("工具事件应为 2 条，实际 %d", len(res.ToolEvents))
	}
	text := res.ToolEvents[1].Content
	if !strings.Contains(text, "已截断") {
		t.Errorf("剪枝标记应回给模型，实际正文结尾 %q", tail(text, 40))
	}
	if got := len([]rune(text)); got >= full {
		t.Errorf("进上下文的文本应短于原文 %d，实际 %d", full, got)
	}

	row := waitToolCallRows(t, writer, 1)[0]
	if !row.Truncated {
		t.Error("超长结果应被标记为已剪枝")
	}
	if row.ResultLen != int64(full) {
		t.Errorf("审计应记**原文**长度 %d，实际 %d", full, row.ResultLen)
	}
	if int64(len([]rune(row.ResultSummary))) > 401 {
		t.Errorf("摘要应被压到 400 字以内，实际 %d", len([]rune(row.ResultSummary)))
	}
}

// TestRunToolAuditRecordsForbidden 越权要单独成类：安全审计靠这个分类答「被拒了多少次」。
func TestRunToolAuditRecordsForbidden(t *testing.T) {
	sess, writer := toolLogArgs(t, "没有权限执行该操作", aienums.ToolCallStatusForbidden)

	sendWithTools(t, sess, "tool-log-key-3")

	row := waitToolCallRows(t, writer, 1)[0]
	if row.Status != string(aienums.ToolCallStatusForbidden) {
		t.Errorf("状态应为 forbidden，实际 %q", row.Status)
	}
	if row.ErrorKey != aienums.ErrToolForbidden {
		t.Errorf("error_key 应为 %q，实际 %q", aienums.ErrToolForbidden, row.ErrorKey)
	}
}

// TestRunToolAuditRecordsArgsError 参数错按 args_error 记，且**不**带面向用户的 error_key：
// 那是模型改参就能重试的事，用户什么都没做错，不该看到「工具执行失败」。
func TestRunToolAuditRecordsArgsError(t *testing.T) {
	sess, writer := toolLogArgs(t, "参数 projectId 不合法", aienums.ToolCallStatusArgsError)

	sendWithTools(t, sess, "tool-log-key-4")

	row := waitToolCallRows(t, writer, 1)[0]
	if row.Status != string(aienums.ToolCallStatusArgsError) {
		t.Errorf("状态应为 args_error，实际 %q", row.Status)
	}
	if row.ErrorKey != "" {
		t.Errorf("参数错的 error_key 应为空（无面向用户文案），实际 %q", row.ErrorKey)
	}
}

// TestRunToolStatusDefaultsToFailed 装配层没给分类时按失败记，绝不记成功。
func TestRunToolStatusDefaultsToFailed(t *testing.T) {
	// status 留空 = 装配层没给分类。
	sess, writer := toolLogArgs(t, "随便一条结果", "")

	sendWithTools(t, sess, "tool-log-key-5")

	row := waitToolCallRows(t, writer, 1)[0]
	if row.Status != string(aienums.ToolCallStatusFailed) {
		t.Errorf("空分类应回落成 failed，实际 %q", row.Status)
	}
	if row.ErrorKey != aienums.ErrToolRunFailed {
		t.Errorf("error_key 应为 %q，实际 %q", aienums.ErrToolRunFailed, row.ErrorKey)
	}
}

// tail 取尾部若干字符（断言消息里给一小段可比对的文本）。
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
