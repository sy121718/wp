// ai_tool_call_log.go — 工具调用流水与工具结果剪枝。
//
// 两件事放在一起是因为它们同源：**模型看到的工具结果**与**审计记下的那一条**
// 出自同一次调用，剪枝结果与审计摘要必须一起算，否则会出现「审计说 320 字、
// 模型实际看到 4000 字」这类对不上的账。
//
// 写入纪律完全沿用 ai_call_log（537）：协程异步写、脱离请求 ctx、独立超时、panic 不外溢。
// 唯一区别是工具调用发生在**一轮对话的中间**：如果同步写，用户等的是「工具耗时 + 两次落库」，
// 而工具本来就可能慢，所以这里的不阻塞更重要。
package aiservice

import (
	"context"
	"strconv"
	"strings"
	"time"

	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

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
