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
	paths, err := s.m.ListActivePathsForInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if s.routes == nil || s.publication == nil {
		return fmt.Errorf("实例退役发布端口未装配")
	}
	extra, err := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{ProjectID: projectID, PresentationID: instanceID})
	if err != nil {
		return err
	}
	for _, path := range dedupePaths(append(paths, extra...)) {
		if err := s.publication.Deactivate(path); err != nil {
			return err
		}
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		if err := s.m.RetireInstanceTx(ctx, tx, projectID, instanceID, time.Now().UTC()); err != nil {
			return err
		}
		return s.routes.DeleteRoutesByPresentationTx(ctx, tx, &pubcontract.DeleteRoutesByPresentationReq{ProjectID: projectID, PresentationID: instanceID})
	})
}
