package adminservice

import (
	"context"
	"fmt"
	"time"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// sideEffectLease 是重放 worker 领取任务的租约时长。
//
// 领了就占这么久：执行方崩溃时，超过租约的行会被下一次领取重新捡起，
// 所以同一个副作用可能被执行两次，执行体必须幂等（重载策略、删会话都幂等）。
const sideEffectLease = 2 * time.Minute

// sideEffectBatch 是一次领取的上限，避免一次事务里锁太多行。
const sideEffectBatch = 50

// sideEffectInterval 是重放循环的间隔。
//
// 任务已经在业务事务里落库，这里只决定「多久补一次」，不影响正确性、只影响重试延迟。
const sideEffectInterval = 30 * time.Second

// revokeSessionPayload 是「吊销某个管理员的会话」需要的业务事实。
type revokeSessionPayload struct {
	UserID uint64 `json:"user_id"`
}

// appendPolicyReloadTx 在业务事务里登记「提交后需要重载权限策略」。
//
// 策略重载作用在进程内的 enforcer 副本上，回滚不了、也没有别的事务可挂：
// 只有写成持久任务，提交后才可能真正做到「失败可重放」。
func (s *Service) appendPolicyReloadTx(ctx context.Context, tx *gorm.DB) (int64, error) {
	return s.st.AppendTx(ctx, tx, adminmodel.SideEffectPolicyReload, "{}")
}

// AppendPolicyReloadForTest 在独立事务里登记一条策略重载任务，返回任务 id。
//
// 只为「提交后立刻执行」这条链路提供可观察的入口：生产路径的任务 id 来自
// 各自业务事务里的 appendPolicyReloadTx，测试要拿到那个 id 才能验证 FlushSideEffect。
func (s *Service) AppendPolicyReloadForTest(ctx context.Context) (int64, error) {
	var id int64
	err := s.st.Transaction(ctx, func(tx *gorm.DB) error {
		got, err := s.appendPolicyReloadTx(ctx, tx)
		id = got
		return err
	})
	return id, err
}

// appendRevokeSessionTx 在业务事务里登记「提交后吊销这些管理员的会话」。
//
// 会话在 Redis 里，PG 事务回滚不了已经删掉的会话，所以同样落成任务。
func (s *Service) appendRevokeSessionTx(ctx context.Context, tx *gorm.DB, userID uint64) (int64, error) {
	payload, err := json.Marshal(revokeSessionPayload{UserID: userID})
	if err != nil {
		return 0, err
	}
	return s.st.AppendTx(ctx, tx, adminmodel.SideEffectRevokeSession, string(payload))
}

// runSideEffect 立刻执行一条任务的副作用，返回是否成功。
//
// 两种任务都幂等：重载策略只是重新读表，删会话对已失效的会话再删一次无副作用。
func (s *Service) runSideEffect(ctx context.Context, task adminmodel.SideEffectTaskEntity) error {
	switch task.Kind {
	case adminmodel.SideEffectPolicyReload:
		return casbin.ReloadPolicy()
	case adminmodel.SideEffectRevokeSession:
		var payload revokeSessionPayload
		if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
			return fmt.Errorf("副作用任务 %d 的 payload 解析失败: %w", task.ID, err)
		}
		if payload.UserID == 0 {
			return fmt.Errorf("副作用任务 %d 缺少 user_id", task.ID)
		}
		return auth.RevokeUserSession(ctx, payload.UserID)
	default:
		return fmt.Errorf("未知的副作用种类 %q（任务 %d）", task.Kind, task.ID)
	}
}

// StartSideEffectWorker 启动副作用重放循环。
//
// 立刻先跑一轮，把上次进程退出时没来得及执行的任务补上。
// ctx 取消即返回：装配点传的是进程级可取消 ctx（见 internal/routers 的 worker 接线），
// 进程收尾时先 cancel 再 Shutdown，循环不会在关库之后还去领任务。
// 返回的 stop 取消循环并等待在途一轮结束：调用方必须在关闭数据库之前调用它。
func (s *Service) StartSideEffectWorker(ctx context.Context, interval time.Duration) func() {
	if s == nil || s.st == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = sideEffectInterval
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runSideEffectLoop(ctx, interval)
	}()
	return func() { <-done }
}

func (s *Service) runSideEffectLoop(ctx context.Context, interval time.Duration) {
	s.replaySideEffectBatch(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.replaySideEffectBatch(ctx)
		}
	}
}

func (s *Service) replaySideEffectBatch(ctx context.Context) {
	if _, err := s.ReplaySideEffects(ctx); err != nil {
		logger.Scene("admin").Error(err, "管理员副作用任务重放失败（任务保留待重试）")
	}
}

// FlushSideEffect 尝试把「刚登记的那一条」任务立刻做完。
//
// 提交后立刻执行是为了让正常路径不产生任何延迟：只是「先写一条事实，再执行它」。
// 领取必须按 id 定位（ClaimOneTx），不能批量领取 —— 批量按 id 升序取，
// 队列里只要有一条更早的失败任务，就会先执行别人的、把本次任务推到下一轮，
// 而调用方以为「已经立刻做完了」。
//
// 失败不向上报错 —— 业务已经提交，报失败就是谎报整批回滚；
// 失败原因留在任务行上，由 worker 重放。
func (s *Service) FlushSideEffect(ctx context.Context, id int64) {
	if id <= 0 {
		return
	}
	var task adminmodel.SideEffectTaskEntity
	var ok bool
	err := s.st.Transaction(ctx, func(tx *gorm.DB) error {
		row, claimed, err := s.st.ClaimOneTx(ctx, tx, id, sideEffectLease)
		task, ok = row, claimed
		return err
	})
	if err != nil {
		logger.Scene("admin").With("task_id", id).Error(err, "副作用任务领取失败，留给 worker 重放")
		return
	}
	if !ok {
		// 已完成、或正被别的持有者用未过期租约占着：交给 worker，不在这里重复执行。
		return
	}
	s.applySideEffect(ctx, task)
}

// claimSideEffects 在一个事务里领取一批待执行任务。
func (s *Service) claimSideEffects(ctx context.Context, limit int) ([]adminmodel.SideEffectTaskEntity, error) {
	var claimed []adminmodel.SideEffectTaskEntity
	err := s.st.Transaction(ctx, func(tx *gorm.DB) error {
		rows, err := s.st.ClaimPendingTx(ctx, tx, sideEffectLease, limit)
		if err != nil {
			return err
		}
		claimed = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// applySideEffect 执行一条已领取的任务并落定状态。
func (s *Service) applySideEffect(ctx context.Context, task adminmodel.SideEffectTaskEntity) {
	if err := s.runSideEffect(ctx, task); err != nil {
		logger.Scene("admin").
			With("task_id", task.ID).With("kind", task.Kind).With("attempts", task.Attempts).
			Error(err, "副作用任务执行失败，保留给 worker 重放")
		if markErr := s.st.MarkFailed(ctx, task.ID, task.Attempts, err.Error()); markErr != nil {
			logger.Scene("admin").With("task_id", task.ID).
				Error(markErr, "副作用任务失败状态未能写回")
		}
		return
	}
	if err := s.st.MarkDone(ctx, task.ID, task.Attempts); err != nil {
		logger.Scene("admin").With("task_id", task.ID).
			Error(err, "副作用任务完成状态未能写回，worker 会重新执行（幂等）")
	}
}

// ReplaySideEffects 是 worker 的一轮：领取一批待执行任务逐条重放。
// 返回本轮处理的条数，供 worker 决定要不要继续跑一轮。
func (s *Service) ReplaySideEffects(ctx context.Context) (int, error) {
	tasks, err := s.claimSideEffects(ctx, sideEffectBatch)
	if err != nil {
		return 0, err
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		s.applySideEffect(ctx, task)
	}
	return len(tasks), nil
}
