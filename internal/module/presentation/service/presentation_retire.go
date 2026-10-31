package presentationservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	presentationenums "go_wp/internal/module/presentation/enums"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
)

// RetireInstance 显式撤销当前发布并软删，保留全部不可变历史。
// 文件停用沿用现有Delete的幂等外部步骤；数据库状态与跨模块路由清理同一事务。
func (s *Service) RetireInstance(ctx context.Context, projectID, instanceID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(instanceID) == "" {
		return presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	instance, err := s.m.GetInstance(ctx, projectID, instanceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return presentationenums.Err(presentationenums.ErrNotFound)
	}
	if err != nil {
		return err
	}
	if instance.DeletedAt != nil {
		return nil
	}
	if err := s.ensureActionTargetUnused(ctx, projectID, instanceID); err != nil {
		return err
	}
	if s.routes == nil || s.publication == nil {
		return fmt.Errorf("实例退役发布端口未装配")
	}

	// 实例条带锁：与发布 / 改 URL / 回滚 / 删除共用同一把，挡住"退役与激活在同一实例上
	// 交错"。**路径必须在锁内读**：锁外先读、锁内再删，读与删之间另一次发布可以把这条
	// 路径换成别人的产物，我们随即把它下线（删的是别人的线上链接）。
	lock := s.lockInstance(instance.EntityType, instance.EntityID)
	lock.Lock()
	defer lock.Unlock()

	paths, err := s.m.ListActivePathsForInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	extra, lerr := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{ProjectID: projectID, PresentationID: instanceID})
	if lerr != nil {
		return lerr
	}
	allPaths := dedupePaths(append(paths, extra...))

	// 下线意图先持久登记（pending），再走"DB 先提交、FS 后下线"。理由与 Delete 同：
	// 反过来的话第 k 条撤链失败就留下半截，而撤链不可回滚；登记缺失则崩溃后没有任何
	// 依据告诉恢复例程该撤哪些路径。
	intent, ierr := s.registerDeactivationIntent(ctx, instance, allPaths)
	if ierr != nil {
		return ierr
	}
	var txErr error
	var commitUncertain bool
	if _, commitUncertain, txErr = s.m.InTransaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		// 稳定锁 → 行锁 → 路径集合复验，与恢复侧同一顺序、同一把键。
		// 与恢复侧同一顺序与同一把键（稳定顾问锁无条件先取 → 行锁）。
		if lerr := s.lockPresentationEntityOrAdvisory(ctx, tx, projectID, instanceID); lerr != nil {
			return lerr
		}
		if err := s.ensureDeactivationPathsUnchanged(ctx, instance, intent.Paths); err != nil {
			return err
		}
		if err := s.routes.MarkDeactivationCommittedTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: instanceID},
			projectID, intent.ReceiptID, intent.PathArtifacts); err != nil {
			return fmt.Errorf("登记退役已提交事实失败: %w", err)
		}
		if err := s.m.RetireInstanceTx(ctx, tx, projectID, instanceID, time.Now().UTC()); err != nil {
			return err
		}
		return s.routes.DeleteRoutesByPresentationTx(ctx, tx, &pubcontract.DeleteRoutesByPresentationReq{ProjectID: projectID, PresentationID: instanceID})
	}); txErr != nil {
		if commitUncertain {
			// 提交结果不明：退役**可能已经生效**（行已软删、businessCommitted 已写）。
			// Abort 会把一条可能已生效的意图终态化，撤链不再重放 —— 保留 pending，
			// 由收敛在稳定锁下重读事实后判定。
			s.NotifyPendingReceipt()
			return txErr
		}
		// 明确回滚 → 退役没发生，businessCommitted 也没写进去。这里只结案这次明确的
		// 回滚（链接从未动过）；收敛侧看到 false 会保留 pending，不会被诱去删链接。
		s.abortPublishReceipt(ctx, intent.ReceiptID, "退役事务回滚")
		return txErr
	}
	return s.completeDeactivationIntent(ctx, instance, intent)
}
