// page_publish_converge.go —— 未结案发布回执的定时收敛（不再依赖重启）。
//
// 问题：发布 / 改 URL / 回滚失败后会在 publication_receipts 留下 pending 回执，
// 而在这之前**只有进程启动时**跑一次恢复（RecoverPendingPublications）——
// 长时间不重启就一直 pending，线上与库长期不一致，而且没有任何可见性。
//
// 做法：判定与补齐逻辑一行都不在这个文件里（在 page_publish_recover.go 的 recoverOne），
// 这里只负责「谁在什么时候驱动它」。三个驱动源共用同一段重放实现：
//
//	启动首跑  RecoverPendingPublications —— 不限批，把上次进程留下的残留一次收干净
//	定时兜底  ConvergePendingReceipts   —— 时间驱动，分批领取（多实例安全）
//	写路径    NotifyPendingReceipt      —— 事务提交后的进程内快通道，正常路径毫秒级
//
// 为什么必须有定时兜底：启动恢复只在装配时跑一次，进程活得越久，pending 残留积得越多；
// 没有定时驱动，「线上已生效、数据库没跟上」会一直维持到下一次重启。
package pageservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// pendingReceiptConvergeBatch 单批领取上限（条）。
	//
	// 批次存在的意义与保留期清理同一条：不制造长事务，也不让一次收敛把启动链拖住。
	// 一条重放要读文件系统符号链接 + 若干次 DB 往返，20 条把单批耗时压在秒级以内。
	pendingReceiptConvergeBatch = 20
	// pendingReceiptConvergeMaxBatches 单次收敛的批次上限（20 × 50 = 1000 条/次）。
	//
	// 正常待办是个位数，这个上限只用来兜住「积压了成千上万条」的异常场面：
	// 超出的部分交给下一轮，而不是让一个 goroutine 长时间占着数据库连接跑下去。
	pendingReceiptConvergeMaxBatches = 50
	// pendingReceiptStartupMaxBatches 启动首跑的批次上限；<= 0 表示不限批。
	//
	// 启动首跑沿用改动前的语义（一次把上次进程的残留全部收干净），由调用方给的超时封顶。
	pendingReceiptStartupMaxBatches = 0
	// pendingReceiptConvergeInterval 定时兜底的间隔。
	//
	// 取值理由：正常路径由写路径的快通道以毫秒级驱动，定时器只兜底两种场面 ——
	// 快通道信号丢了（容量 1 的通道在收敛进行中只能留住一次信号），
	// 以及残留是**别的实例**留下的（本进程收不到它的写信号）。
	// 一分钟足以把不一致窗口压在一分钟内；再短只增加空转次数，再长则兜底失去意义。
	// 空转成本本身被迁移 267 的部分索引压到「一次零行的索引扫描」，与表的历史规模无关。
	pendingReceiptConvergeInterval = time.Minute
	// pendingReceiptConvergeTimeout 定时 / 快通道单次收敛的超时。
	pendingReceiptConvergeTimeout = 2 * time.Minute
	// pendingReceiptStartupTimeout 启动首跑的超时（与改动前的启动恢复一致，5 分钟）。
	pendingReceiptStartupTimeout = 5 * time.Minute
)

// pendingReceiptConvergeResult 一次收敛的结果（供日志与返回值使用）。
type pendingReceiptConvergeResult struct {
	claimed    int // 本轮领取到的回执条数
	completed  int // 补齐数据库状态后结案
	rolledBack int // 判定为未生效、结案为已回滚
	failed     int // 仍失败，保留 pending
	batches    int // 实际领取的批次数
}

// converged 已离开 pending 的条数（补完成 + 标回滚）。
//
// 「仍失败」不算收敛：它还在 pending，下一轮继续重放。把失败的 pending 直接标死，
// 等于拿「看起来收敛了」换掉「状态未知」，比不收敛更糟。
func (r pendingReceiptConvergeResult) converged() int { return r.completed + r.rolledBack }

// ConvergePendingReceipts 收敛未结案的发布回执（定时器与写路径快通道的入口）。
//
// 与启动恢复共用同一段重放实现（convergePendingReceipts → recoverOne）：
// 两种入口各写一套判定迟早分叉，而分叉的表现是「定时收敛说已收敛、重启恢复又改一遍，
// 两边结果不同」。
//
// 幂等：重放的每一步都是 upsert / 内容寻址 / 按归属重复激活，结案又只改 pending 行，
// 因此重复收敛（多实例、快通道与定时器同时触发）不会写出新状态。
//
// 返回的 error 汇总「领取失败」与「单条仍失败」；已收敛的条数照常返回 ——
// 部分失败不该把整轮的结果丢掉（调用方据此记日志，失败的行保留 pending 等下轮）。
func (s *Service) ConvergePendingReceipts(ctx context.Context) (converged int, err error) {
	res, claimErr, itemErrs := s.convergePendingReceipts(ctx, pendingReceiptConvergeMaxBatches)
	return res.converged(), errors.Join(append([]error{claimErr}, itemErrs...)...)
}

// RecoverPendingPublications 启动 / 运维全量恢复：把未结案的发布回执一次收干净。
//
// 与定时收敛共用同一段实现（convergePendingReceipts），区别只在批次上限：
// 这里不限批（由调用方给的超时封顶），定时 / 快通道按批（不制造长事务）。
// 返回值保持原有形状（补完成 / 标回滚），供既有调用方与用例断言。
//
// 只把「领取失败」回传给调用方；单条重放失败按「保留 pending、下一轮再试」处理，
// 不让一条收不了的旧回执把整个启动恢复判成失败。
func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	res, claimErr, _ := s.convergePendingReceipts(ctx, pendingReceiptStartupMaxBatches)
	return res.completed, res.rolledBack, claimErr
}

// convergePendingReceipts 收敛主循环：分批领取 → 逐条重放既有恢复例程。
//
// maxBatches <= 0 表示不限批（启动首跑；由 ctx 超时封顶）。
// 三个返回值：结果统计、领取错误（数据库层，整轮中止）、单条重放错误（保留 pending）。
func (s *Service) convergePendingReceipts(ctx context.Context, maxBatches int) (res pendingReceiptConvergeResult, claimErr error, itemErrs []error) {
	// 与 RecoverPendingPublications 改动前的守卫一致：没有契约、没有访问面存储时不做任何事
	// （这台实例没有回执设施，不是「没有待办」）。
	if s == nil || s.routes == nil || s.publication == nil {
		return res, nil, nil
	}
	start := time.Now()
	for batch := 0; maxBatches <= 0 || batch < maxBatches; batch++ {
		if cerr := ctx.Err(); cerr != nil {
			itemErrs = append(itemErrs, cerr)
			break
		}
		items, cerr := s.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
			SourceType: pubcontract.ReceiptSourcePage,
			Actions:    recoverableReceiptActions,
			Limit:      pendingReceiptConvergeBatch,
		})
		if cerr != nil {
			claimErr = cerr
			logger.Scene("publication").Error(cerr, "领取未结案发布回执失败（本轮收敛中止）")
			break
		}
		if len(items) == 0 {
			break
		}
		res.claimed += len(items)
		res.batches++
		handled, failed := 0, 0
		for _, item := range items {
			// 领取条件已经筛过一遍，这里是第二道（防止领取口径将来被放宽后静默扩大重放范围：
			// 拿发布形态的判据去「补齐」一条路由回执，会把路由变更纠正成发布状态）。
			if item.SourceType != pubcontract.ReceiptSourcePage || !recoverableReceiptAction(item.Action) {
				continue
			}
			handled++
			done, rerr := s.recoverOne(ctx, item)
			if rerr != nil {
				failed++
				logger.Scene("publication").With("receiptId", item.ID).With("action", item.Action).
					With("path", item.Path).Error(rerr, "发布回执收敛失败（保留 pending，下一轮重试）")
				itemErrs = append(itemErrs, fmt.Errorf("回执 %s(%s): %w", item.ID, item.Action, rerr))
				continue
			}
			if done {
				res.completed++
			} else {
				res.rolledBack++
			}
		}
		res.failed += failed
		// 本批出现仍失败的行就收手：那些行还在 pending（且多半正卡在最旧的一批上），
		// 继续领下一批只会把它们再重放一遍 —— 交给下一轮，别在这里空转。
		if failed > 0 {
			break
		}
		// 本批没有一行属于本收敛例程（领取口径与判定口径不一致）：再领还是同一批，收手。
		if handled == 0 {
			break
		}
	}
	s.lastConvergeAt.Store(time.Now().Unix())
	s.logPendingReceiptConverge(res, time.Since(start))
	return res, claimErr, itemErrs
}

// logPendingReceiptConverge 记一条结构化收敛日志（收敛条数 / 失败条数 / 耗时）。
//
// 空转（一条都没领到）走 Debug：定时器每分钟一次，用 Info 记「什么都没发生」只会把
// 真正有用的那几条淹掉。有失败一律 Warn —— 失败的 pending 是「线上与库还不一致」，
// 必须在日志里看得见。
func (s *Service) logPendingReceiptConverge(res pendingReceiptConvergeResult, cost time.Duration) {
	entry := logger.Scene("publication").
		With("claimed", res.claimed).
		With("converged", res.converged()).
		With("completed", res.completed).
		With("rolledBack", res.rolledBack).
		With("failed", res.failed).
		With("batches", res.batches).
		With("costMs", cost.Milliseconds())
	switch {
	case res.failed > 0:
		entry.Warn("发布回执收敛完成（有仍未成功的行，保留 pending 等待下一轮）")
	case res.claimed > 0:
		entry.Info("发布回执收敛完成")
	default:
		entry.Debug("发布回执收敛空转（没有未结案的回执）")
	}
}

// NotifyPendingReceipt 写路径的进程内快通道信号（事务提交后调用）。
//
// 容量 1、非阻塞发送：并发写入合并成一次收敛（多推几次不携带额外信息），
// 通道满时直接丢弃 —— 定时器兜底，丢信号只会让收敛晚一个间隔，不会让回执永久 pending。
// 未启动调度（用例、或未装配页面路由的进程）时这个信号没有接收方，同样无害。
func (s *Service) NotifyPendingReceipt() {
	if s == nil || s.convergeWake == nil {
		return
	}
	select {
	case s.convergeWake <- struct{}{}:
	default:
	}
}

// PendingReceiptStatus 只读观测：当前未收敛的 pending 回执数 + 本进程最近一次收敛时刻。
//
// 口径与本收敛例程的领取条件完全一致（page + 三种访问面切换动作）：数的是
// 「本实例会去收、且还没收掉的行」，健康检查据此判断「收敛是不是跟不上了」。
// 路由变更回执（activate / redirect）由各自的事务内结案处理、不归这里，
// presentation 的回执由 presentation 自己的恢复例程负责，也都不计入。
func (s *Service) PendingReceiptStatus(ctx context.Context) (pending int64, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	return pending, lastConvergeAt, nil
}

// PendingReceiptBacklog 只读观测：待收敛积压的完整形状 —— 条数 + 最老一条已等待多久
// + 本进程最近一次收敛时刻。
//
// 与 PendingReceiptStatus 的分工：那个只给「条数 + 收敛时刻」（三元组形状由既有调用方钉住），
// 而条数看不出积压是不是在增长 —— 收掉 3 条又来 3 条，数字纹丝不动。
// **最老一条的年龄**才是「收敛跟不跟得上」的判据：它只会在真的收不动时持续变大。
//
// 口径与收敛例程严格一致（page + 三种访问面切换动作），数出来的就是「本实例会去收、
// 且还没收掉的行」；路由变更回执与 presentation 的回执不归这里（各自的处理方负责）。
//
// 两个查询不共用事务：观测允许读到中间态（收敛正在跑时条数可能少一条、年龄刚被清零），
// 这不影响「积压是否在增长」的判断。
func (s *Service) PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	if pending <= 0 {
		return 0, 0, lastConvergeAt, nil
	}
	return pending, s.oldestPendingReceiptAge(ctx), lastConvergeAt, nil
}

// oldestPendingReceiptAge 本模块口径里最老一条 pending 回执已等待的时长；读不到时返回 0。
//
// 走 ListPendingReceipts（按 create_time ASC 的**只读**列举）：契约里唯一一条不加锁的列举能力。
// 不能用 ClaimPendingReceipts —— 它用 FOR UPDATE SKIP LOCKED **认领**行，健康检查 / 列表页
// 走那条路等于让观测动作把待收敛的行锁住再放回去，直接干扰收敛（多实例下还会把行从
// 别的实例的批次里抢走）。
//
// 代价：只在条数不为 0 时调用，且这一次列举会把全部 pending 行拉回来。正常积压是个位数；
// 真积压成千上万条时，多这一趟读取相比「积压完全不可见」仍是划算的。
// 时间戳解析失败按「读不到」处理 —— 年龄是附加信息，不该连条数一起丢掉。
func (s *Service) oldestPendingReceiptAge(ctx context.Context) time.Duration {
	items, err := s.routes.ListPendingReceipts(ctx)
	if err != nil {
		logger.Scene("publication").With("err", err).
			Warn("读取最老未结案回执失败（积压条数仍可用，年龄暂缺）")
		return 0
	}
	for _, item := range items {
		// 列表按 create_time ASC：第一条命中本模块口径的就是最老的一条。
		if item.SourceType != pubcontract.ReceiptSourcePage || !recoverableReceiptAction(item.Action) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, item.CreatedAt)
		if perr != nil || ts.IsZero() {
			return 0
		}
		if age := time.Since(ts); age > 0 {
			return age
		}
		return 0
	}
	return 0
}

// StartPendingReceiptConvergenceScheduler 启动发布回执收敛调度（进程内 goroutine + ticker）。
//
// 形状与 page_retention / order_expire / build_worker 的既有调度一致：先跑一次，再等间隔。
// 两个信号源由 select 合并：
//   - ticker：兜底（快通道信号丢失，或残留是别的实例留下的）；
//   - convergeWake：写路径在事务提交后推的进程内快通道，正常路径毫秒级收敛。
//
// 启动首跑直接做全量恢复（不限批 + 5 分钟预算）—— 它取代了原先装配处的裸启动恢复。
// 不能留着那个入口：同一段实现若有两个「启动时跑一次」的驱动源，启动瞬间会有两个
// goroutine 并发重放同一批回执（重放虽幂等，仍会白白多跑一遍、多占一次连接）。
func StartPendingReceiptConvergenceScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startPendingReceiptConvergenceScheduler(svc, pendingReceiptConvergeInterval)
}

// StartPendingReceiptConvergenceSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入一个远大于用例时长的间隔，就能把「收敛确实由快通道驱动、
// 而不是定时器顺手做掉的」证成（见 public/test/page/unit/page_receipt_converge_test.go）。
func StartPendingReceiptConvergenceSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPendingReceiptConvergenceScheduler(svc, interval)
}

func startPendingReceiptConvergenceScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pendingReceiptConvergeInterval
	}
	go func() {
		// 启动首跑：全量收一次上次进程留下的残留。
		startupCtx, cancelStartup := context.WithTimeout(context.Background(), pendingReceiptStartupTimeout)
		_, _, _ = svc.RecoverPendingPublications(startupCtx)
		cancelStartup()

		converge := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pendingReceiptConvergeTimeout)
			defer cancel()
			_, _ = svc.ConvergePendingReceipts(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				converge()
			case <-svc.convergeWake:
				converge()
			}
		}
	}()
}
