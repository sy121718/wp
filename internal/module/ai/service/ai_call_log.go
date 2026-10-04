// ai_call_log.go — 每次上游调用的流水记录（协程异步写）。
//
// 落点是**唯一出站点**（Service.Chat），不是某个调用方：这样无论谁发起对话
// （会话页发消息、对外 JSON 接口、将来的批量任务），流水都记得到，不需要每个调用方各写一遍。
package aiservice

import (
	"context"
	"errors"
	"time"

	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

// callLogTimeout 单条调用流水的写入上限（独立于上游超时：上游 15s 之后还要留出落库时间）。
const callLogTimeout = 5 * time.Second

// CallLogWriter 调用流水的写入端口（由 model 实现，装配期注入）。
//
// 用接口而不是直接持 *aimodel.CallLogModel：出站层对这张表只有「追加一条」这一件事，
// 端口形状把能力收窄到这一件事上（用例也能换一个记录器进来断言写了什么）。
type CallLogWriter interface {
	Insert(ctx context.Context, e *aimodel.AICallLogEntity) error
}

// SetCallLogWriter 注入调用流水写入端口；未注入时 logCallAsync 是空操作（不 panic）。
func (s *Service) SetCallLogWriter(w CallLogWriter) { s.calls = w }

// logCallAsync 异步落一条调用流水。
//
// 三条「为什么必须这么写」（改回同步或改回原 ctx 都会踩）：
//
//  1. **旁路观测不该惩罚正常路径**：写日志的往返（含库抖动时的重连）会直接加在用户等回复的时间上。
//  2. **必须脱离请求 ctx**：HTTP 请求返回后 ctx 立刻被取消，用原 ctx 异步写等于 100% 写不进去 ——
//     这不是偶发竞态而是必然，所以走 context.WithoutCancel 再配自己的超时。
//  3. **panic 与错误都不许外溢**：这是纯观测路径，任何写失败都不能影响业务返回、更不能带走进程。
//
// 已知代价（写在这里免得后人当 bug 查）：进程在协程落库前退出，这条流水会丢。
// 观测数据丢一条不影响业务正确性，这是用「不阻塞用户」换来的，接受。
func (s *Service) logCallAsync(ctx context.Context, e *aimodel.AICallLogEntity) {
	if s == nil || s.calls == nil || e == nil {
		return
	}
	// context.WithoutCancel(nil) 会 panic，先兜住 nil（与 admin 的数据权限快照同口径）。
	if ctx == nil {
		ctx = context.Background()
	}
	base := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Scene("ai").With("session", e.SessionID).With("panic", r).
					Error(nil, "AI 调用流水写入协程发生 panic，已忽略")
			}
		}()
		wctx, cancel := context.WithTimeout(base, callLogTimeout)
		defer cancel()
		if err := s.calls.Insert(wctx, e); err != nil {
			logger.Scene("ai").
				With("session", e.SessionID).
				With("provider", e.ProviderKey).
				With("model", e.ModelID).
				Error(err, "AI 调用流水写入失败（只记日志，不影响本次调用结果）")
		}
	}()
}

// callErrorSentinels 允许落进 ai_call_log.error_key 的哨兵。
//
// 它们的**值本身就是 i18n key**（enums 口径），所以可以直接入库；
// 不在这个集合里的错误一律归口 ErrInternal —— 底层原文（上游报文片段、SQL 片段）
// 只进日志，不进表：流水表会被后台页面读，原文透出去就是信息泄漏。
var callErrorSentinels = []error{
	ErrProviderNotFound,
	ErrInvalidParam,
	ErrProtocolUnsupported,
	ErrBaseURLRequired,
	ErrCipherUnavailable,
	ErrModelsFetchFailed,
	ErrURLMalformed,
	ErrURLSchemeUnsupported,
	ErrURLHostMissing,
	ErrURLUnresolvable,
	ErrURLDenied,
	ErrInternal,
}

// callErrorKey 把一次失败归口成可入库的 key；识别不出（含被包装过的非哨兵错误）回 ErrInternal。
func callErrorKey(err error) string {
	for _, sentinel := range callErrorSentinels {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return aienums.ErrInternal
}
