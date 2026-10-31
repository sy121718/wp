package productservice

import (
	"context"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

const (
	outboxBatchMax = 200

	outboxLease = 5 * time.Minute

	outboxDispatchInterval = 15 * time.Second
)

type invalidationTarget struct {
	EntityType string
	EntityID   string
}

func productInvalidationTarget(productID string) invalidationTarget {
	return invalidationTarget{EntityType: productcontract.EntityTypeProduct, EntityID: productID}
}

func membershipDiff(before, after []string) []string {
	inBefore := make(map[string]bool, len(before))
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" {
			inBefore[id] = true
		}
	}
	inAfter := make(map[string]bool, len(after))
	changed := make([]string, 0, len(after))
	for _, id := range after {
		if id = strings.TrimSpace(id); id != "" {
			inAfter[id] = true
			if !inBefore[id] {
				changed = append(changed, id)
			}
		}
	}
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" && !inAfter[id] {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}

func (s *Service) enqueueInvalidationTx(ctx context.Context, tx *gorm.DB, projectID string, targets ...invalidationTarget) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || tx == nil || len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]productmodel.OutboxEventEntity, 0, len(targets)*2)
	seen := map[[2]string]bool{}
	collectionEmitted := false
	for _, t := range targets {
		t.EntityType = strings.TrimSpace(t.EntityType)
		t.EntityID = strings.TrimSpace(t.EntityID)
		key := [2]string{t.EntityType, t.EntityID}
		if t.EntityType == "" || t.EntityID == "" || seen[key] {
			continue
		}
		seen[key] = true
		rev, err := s.m.NextOutboxRevisionTx(ctx, tx, t.EntityType, t.EntityID)
		if err != nil {
			return err
		}
		direct := pipeline.DirectContentKey(t.EntityType, t.EntityID)
		keys := []pipeline.DepKey{direct}

		if !collectionEmitted {
			collectionEmitted = true
			keys = append(keys, pipeline.ContentCollectionKey(productcontract.EntityTypeProduct))
		}
		for _, k := range keys {
			rows = append(rows, productmodel.OutboxEventEntity{
				ProjectID:      projectID,
				EntityType:     t.EntityType,
				EntityID:       t.EntityID,
				EntityRevision: rev,
				DependencyKind: k.Kind,
				DependencyKey:  k.Key,
				CreateTime:     now,
			})
		}
	}
	return s.m.AppendOutboxTx(ctx, tx, rows)
}

func (s *Service) enqueueProductInvalidationTx(ctx context.Context, tx *gorm.DB, projectID, productID string) error {
	return s.enqueueInvalidationTx(ctx, tx, projectID, productInvalidationTarget(productID))
}

func (s *Service) DispatchOutbox(ctx context.Context, limit int) (int, error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	if limit <= 0 || limit > outboxBatchMax {
		limit = outboxBatchMax
	}
	rows, err := s.m.ClaimPendingOutbox(ctx, outboxLease, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if s.invalidator == nil {

		return 0, nil
	}
	seen := map[[2]string]bool{}
	done := make([]int64, 0, len(rows))
	for _, row := range rows {
		k := [2]string{row.DependencyKind, row.DependencyKey}
		if !seen[k] {
			seen[k] = true

			s.invalidator.Invalidate(ctx, row.DependencyKind, row.DependencyKey)
		}
		done = append(done, row.ID)
	}
	if err := s.m.MarkOutboxProcessed(ctx, done); err != nil {

		return 0, err
	}
	return len(rows), nil
}

func (s *Service) StartOutboxWorker(ctx context.Context, interval time.Duration) {
	if s == nil || s.invalidator == nil {
		return
	}
	if interval <= 0 {
		interval = outboxDispatchInterval
	}
	go s.runOutboxLoop(ctx, interval)
}

func (s *Service) runOutboxLoop(ctx context.Context, interval time.Duration) {
	s.dispatchOutboxBatch(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchOutboxBatch(ctx)
		}
	}
}

func (s *Service) dispatchOutboxBatch(ctx context.Context) {
	n, err := s.DispatchOutbox(ctx, outboxBatchMax)
	if err != nil {
		logger.Scene("product").Error(err, "商品依赖事件消费失败（事件保留待重试）")
		return
	}
	if n > 0 {
		logger.Scene("product").With("count", n).Info("商品依赖事件已扇出")
	}
}

const maxRenameFanoutProducts = 5000

func (s *Service) enqueueEntityRenameFanout(ctx context.Context, tx *gorm.DB, projectID, entityType, entityID string,
	referencedProductIDs []string) error {
	if len(referencedProductIDs) > maxRenameFanoutProducts {
		logger.Scene("product").With("entity_type", entityType).With("entity_id", entityID).
			With("referencing", len(referencedProductIDs)).With("limit", maxRenameFanoutProducts).
			Warn("实体改名牵涉的商品数超过单次扇出上限：按上限逐个发失效事件（超出部分不在本次失效范围内）")
		referencedProductIDs = referencedProductIDs[:maxRenameFanoutProducts]
	}
	targets := make([]invalidationTarget, 0, len(referencedProductIDs)+1)
	targets = append(targets, invalidationTarget{EntityType: entityType, EntityID: entityID})
	for _, pid := range referencedProductIDs {
		targets = append(targets, productInvalidationTarget(pid))
	}
	return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
}
