package productservice

import (
	"context"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/facing"
	"gorm.io/gorm"
)

func (s *Service) resolvePricingTargetsTx(ctx context.Context, tx *gorm.DB, pr *pricingRequest) ([]*pricingTarget, error) {
	projectID, err := s.resolveProjectID(ctx, pr.projectID)
	if err != nil {
		return nil, err
	}
	pr.projectID = projectID
	if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
		return nil, err
	}
	if err := s.m.LockPricingTagsTx(ctx, tx, projectID, productenums.TagKindRule); err != nil {
		return nil, err
	}
	m := productmodel.NewModel(tx)
	targets, err := resolvePricingTargets(ctx, m, pr)
	if err != nil {
		return nil, err
	}
	productIDs := make([]string, 0, len(targets))
	names := make(map[string]string, len(targets))
	for _, target := range targets {
		if _, ok := names[target.product.ID]; !ok {
			productIDs = append(productIDs, target.product.ID)
		}
		names[target.product.ID] = target.product.Name
	}
	locked, err := m.LockProductsTx(ctx, tx, productIDs, projectID)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(locked))
	for _, id := range locked {
		present[id] = true
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range productIDs {
		if !present[id] {
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}

	original := targets
	targets, err = resolvePricingTargets(ctx, m, pr)
	if err != nil {
		fact, ok := facing.FactOf(err)
		if ok && (fact.Key == productenums.ErrPricingTargetNotFound || fact.Key == productenums.ErrPricingFilterEmpty) {
			return nil, bulkRejected(pricingTargetChanges(original, nil))
		}
		return nil, err
	}
	if changed := pricingTargetChanges(original, targets); len(changed) > 0 {
		return nil, bulkRejected(changed)
	}
	variantIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		variantIDs = append(variantIDs, target.variant.ID)
	}
	variants, err := m.LockPricingVariantsTx(ctx, tx, variantIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*productmodel.VariantEntity, len(variants))
	for _, variant := range variants {
		byID[variant.ID] = variant
	}
	for _, target := range targets {
		current := byID[target.variant.ID]
		if current == nil {
			blocked = append(blocked, bulkBlocked(target.variant.ID, target.product.Name+" / "+target.variant.SKUCode, productenums.ErrPricingTargetNotFound))
		} else {
			target.variant = current
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	return targets, nil
}

func pricingTargetChanges(before, after []*pricingTarget) []facing.BlockedItem {
	oldIDs := make(map[string]bool, len(before))
	newIDs := make(map[string]bool, len(after))
	for _, target := range before {
		oldIDs[target.variant.ID] = true
	}
	for _, target := range after {
		newIDs[target.variant.ID] = true
	}
	var changed []facing.BlockedItem
	for _, target := range before {
		if !newIDs[target.variant.ID] {
			changed = append(changed, bulkBlocked(target.variant.ID, target.product.Name+" / "+target.variant.SKUCode, productenums.ErrPricingTargetChanged))
		}
	}
	for _, target := range after {
		if !oldIDs[target.variant.ID] {
			changed = append(changed, bulkBlocked(target.variant.ID, target.product.Name+" / "+target.variant.SKUCode, productenums.ErrPricingTargetChanged))
		}
	}
	return changed
}
