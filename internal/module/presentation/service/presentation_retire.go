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

			s.NotifyPendingReceipt()
			return txErr
		}

		s.abortPublishReceipt(ctx, intent.ReceiptID, "退役事务回滚")
		return txErr
	}
	return s.completeDeactivationIntent(ctx, instance, intent)
}
