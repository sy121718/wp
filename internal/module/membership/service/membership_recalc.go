// membership_recalc.go — 会员归属的日结重算与它的调度器（BIZ-3）。
//
// 数据来源的形状是本文件最要紧的设计点：membership 模块**读不到 users 表，也不该读 orders 表**
// （表隔离 + 消费额口径属于订单域）。所以「谁是本工程的会员候选」这个问题由一个**注入的批量只读端口**
// （membershipcontract.PurchaseSource，订单侧实现）回答，本模块只做「消费额 ≥ 门槛」的分档。
//
// 端口未注入时的行为是**显式不可用**，不是「扫到 0 个人」：
// 后者看起来像「大家都没消费」，而这两个状态的处置方式完全相反（一个是等订单侧接线，
// 一个是去查订单状态口径）。判据就是 RecalcResult.Scanned 那个计数。
//
// 调度器形态照 page_retention / analytics_rollup / order_expire 的既有样式：
// 进程内 goroutine + ticker（先跑一次再等间隔）、**必须带 utils.IsTestProcess() 守卫** ——
// 首跑会动真实库，测试的行为必须由用例自己触发。
package membershipservice

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// recalcInterval 日结重算的运行间隔（每天一次：消费额是慢变量，
	// 更密的频率只会把同一批人反复写一遍）。
	recalcInterval = 24 * time.Hour
	// recalcBatchSize 单个事务里处理的候选人数上限。
	//
	// 分批的意义是不制造长事务与锁表：一个工程几万个会员在一个事务里逐个 upsert，
	// 会把归属表锁住几分钟，而这段时间后台的「会员归属」页全部阻塞。
	recalcBatchSize = 500
	// recalcTimeout 单次重算的整体超时（工程多 / 候选多时不能无限跑）。
	recalcTimeout = 30 * time.Minute
)

// RecalcProject 立即重算某工程的会员归属（后台「立即重算」与排障用）。
//
// 步骤：
//  1. 向消费额端口要素材（谁有消费、各消费多少）——端口未注入即显式失败；
//  2. 取本工程等级清单，定出默认等级（缺失即打回给人，带 project_id）；
//  3. 按批 upsert，每批一个作用域事务。手工锁定的行由 UpsertAutoTx 的 SQL 自己跳过，
//     这里只统计跳过的条数（Skipped）——不靠调用方记得判断 source。
func (s *Service) RecalcProject(ctx context.Context, req *membershipdto.RecalcProjectReq) (res *membershipdto.RecalcResult, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	return s.recalcProject(ctx, req.ProjectID)
}

// recalcProject 单个工程的重算实现（RecalcAllProjects 也走它）。
func (s *Service) recalcProject(ctx context.Context, projectID string) (res *membershipdto.RecalcResult, err error) {
	if s.purchases == nil {
		// 显式不可用：说清是「订单侧的批量聚合端口还没接」，
		// 而不是给一个 Scanned = 0 的结果让人以为没人消费过。
		return nil, errors.New(membershipenums.ErrRecalcUnavailable)
	}

	totals, terr := s.purchases.SpentTotalsByUser(ctx, projectID)
	if terr != nil {
		return nil, terr
	}

	// 等级清单与默认等级读一次（同一作用域事务里取，避免读到「等级刚被删一半」的快照）。
	var tiers []*membershipmodel.TierEntity
	if lerr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		tiers, ierr = s.model.ListTiersTx(ctx, tx, projectID)
		return ierr
	}); lerr != nil {
		return nil, lerr
	}
	defaultTier := pickDefaultTier(tiers)
	if defaultTier == nil {
		// 没有默认等级 → 无法给「还没达到任何档」的人兜底。打回给人（带 project_id），
		// 不静默跳过整个工程：跳过的表现是「重算跑了但谁都没变」，
		// 而运营在页面上看不到任何原因。
		return nil, errors.New(membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, projectID))
	}

	result := &membershipdto.RecalcResult{Scanned: len(totals)}

	// 候选分批：每批一个事务（判据见 recalcBatchSize 的注释）。
	batch := make([]recalcCandidate, 0, recalcBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		changed, skipped, ferr := s.applyRecalcBatch(ctx, projectID, batch)
		if ferr != nil {
			return ferr
		}
		result.Changed += changed
		result.Skipped += skipped
		batch = batch[:0]
		return nil
	}

	for userID, spent := range totals {
		tier := resolveSpendTier(tiers, defaultTier, spent)
		batch = append(batch, recalcCandidate{userID: userID, tierID: tier.ID})
		if len(batch) >= recalcBatchSize {
			if ferr := flush(); ferr != nil {
				return nil, ferr
			}
		}
	}
	if ferr := flush(); ferr != nil {
		return nil, ferr
	}
	return result, nil
}

// recalcCandidate 一个待写入的归属结论（用户 + 他应属的等级）。
type recalcCandidate struct {
	userID uint64
	tierID int64
}

// applyRecalcBatch 在一个作用域事务里写入一批归属结论，返回（写入数, 被手工锁定而跳过的数）。
func (s *Service) applyRecalcBatch(ctx context.Context, projectID string, batch []recalcCandidate) (changed, skipped int, err error) {
	err = s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		for _, item := range batch {
			// UpsertAutoTx 的 SQL 里带 `WHERE membership_assignments.source <> 'manual'`：
			// 命中冲突但守卫不成立时影响 0 行且不报错 —— 那就是「这一行被手工锁定，
			// 本次自动结论作废」。语义由 SQL 承载，这里只数结果。
			ok, uerr := s.model.UpsertAutoTx(ctx, tx, projectID, item.userID, item.tierID)
			if uerr != nil {
				return uerr
			}
			if ok {
				changed++
			} else {
				skipped++
			}
		}
		return nil
	})
	return changed, skipped, err
}

// resolveSpendTier 纯函数：按消费额定档，落空回退默认等级。
//
// 与 selectTierBySpend 的分工：那个只回答「达到哪一档」，本函数回答「达不到任何档怎么办」。
// 两者分开是为了让单测能各自钉住 —— 「达到某档」与「一个档都达不到」是两条不同的判据，
// 而后者被写错时的表现是「所有新访客都掉到某个意想不到的等级」。
func resolveSpendTier(tiers []*membershipmodel.TierEntity, defaultTier *membershipmodel.TierEntity, spent int64) *membershipmodel.TierEntity {
	if tier := selectTierBySpend(tiers, spent); tier != nil {
		return tier
	}
	return defaultTier
}

// RecalcReady 消费额批量端口是否已接入（后台页面据此决定「立即重算」按钮是否可用）。
//
// 暴露成只读探针而不是让 handler 去猜：端口注没注入只有本结构体知道，
// 让页面重新拿一次端口等于维护第二份状态来源 —— 两份迟早不一致。
func (s *Service) RecalcReady() bool {
	if s == nil {
		return false
	}
	return s.purchases != nil
}

// RecalcAllProjects 遍历全部工程重算一遍（日结调度器的入口）。
//
// 一个工程失败不中断其余工程：某个工程缺默认等级时它的重算是无意义的（会返回
// ErrDefaultTierMissing），但那不该让别的工程的会员等级也不再更新。
// 失败逐个记日志，整体返回最后一个错误供调度器留痕。
func (s *Service) RecalcAllProjects(ctx context.Context) (total *membershipdto.RecalcResult, err error) {
	total = &membershipdto.RecalcResult{}
	if s.purchases == nil {
		return total, errors.New(membershipenums.ErrRecalcUnavailable)
	}
	if s.projects == nil {
		return total, errors.New(membershipenums.ErrProjectRequired)
	}
	projects, perr := s.projects.List(ctx)
	if perr != nil {
		return total, perr
	}
	var lastErr error
	for _, p := range projects {
		res, rerr := s.recalcProject(ctx, p.ID)
		if rerr != nil {
			lastErr = rerr
			logger.Scene(errScene).With("projectId", p.ID).Error(rerr, "会员归属重算失败（该工程已跳过）")
			continue
		}
		total.Scanned += res.Scanned
		total.Changed += res.Changed
		total.Skipped += res.Skipped
	}
	return total, lastErr
}

// StartRecalcScheduler 启动会员归属日结（幂等；测试进程或端口未注入时空操作）。
//
// 幂等靠 s.recalcOnce：装配层可能在两处调用（例如将来在订单侧注入后再调一次），
// 重复启动会得到两条 ticker，同一批人每天被写两遍。
func (s *Service) StartRecalcScheduler() {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if s == nil {
		return
	}
	if s.purchases == nil {
		// 端口未注入 → 调度器**禁用**而不是空转：空转会在启动日志之外不留任何痕迹，
		// 而「日结从来没跑过」这件事必须在启动时就说得出来（下面这条 Warn 就是那个痕迹）。
		logger.Scene(errScene).Warn(
			"会员归属日结未启动：消费额批量只读端口（membershipcontract.PurchaseSource）尚未注入，" +
				"归属重算整体不可用（等订单侧接线后调用 SetPurchaseSource + StartRecalcScheduler）")
		return
	}
	s.recalcOnce.Do(func() {
		go func() {
			run := func() {
				ctx, cancel := context.WithTimeout(context.Background(), recalcTimeout)
				defer cancel()
				res, err := s.RecalcAllProjects(ctx)
				if err != nil {
					logger.Scene(errScene).Error(err, "会员归属日结执行失败（部分工程已跳过）")
				}
				logger.Scene(errScene).
					With("scanned", res.Scanned).With("changed", res.Changed).With("skipped", res.Skipped).
					Info("会员归属日结完成")
			}
			run()
			ticker := time.NewTicker(recalcInterval)
			defer ticker.Stop()
			for range ticker.C {
				run()
			}
		}()
	})
}
