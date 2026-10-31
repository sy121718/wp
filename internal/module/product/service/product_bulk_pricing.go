package productservice

import (
	"context"
	"net/http"
	"sort"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
)

func (s *Service) BulkApplyPricing(ctx context.Context, req *productdto.BulkPricingReq) (res *productdto.BulkPricingResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkPricingResultResp{}

	if len(ids) == 0 {
		return nil, facing.WithStatus(
			facing.New(productenums.BulkPricingNoneSelected, "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"),
			http.StatusBadRequest)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	existing, err := s.m.ListNameFactsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(existing))
	names := make(map[string]string, len(existing))
	for _, p := range existing {
		present[p.ID] = true
		names[p.ID] = p.Name
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		if !present[id] {
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}

	plans := make([]*pricingPlan, 0, len(ids))
	unchanged := 0
	sort.Strings(ids)
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
			return err
		}
		if err := s.m.LockPricingTagsTx(ctx, tx, projectID, productenums.TagKindRule); err != nil {
			return err
		}
		locked, err := s.m.LockProductsTx(ctx, tx, ids, projectID)
		if err != nil {
			return err
		}
		present := make(map[string]bool, len(locked))
		for _, id := range locked {
			present[id] = true
		}
		for _, id := range ids {
			if !present[id] {
				blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
			}
		}
		if len(blocked) > 0 {
			return bulkRejected(blocked)
		}
		for _, id := range ids {
			rule := req.Rule
			rule.ProjectID = projectID
			rule.Scope = productenums.PricingScopeProduct
			rule.TargetID = id
			plan, perr := s.computePricingPlan(ctx, tx, &productdto.PricingApplyReq{
				PricingRuleReq: rule, Note: req.Note, OperatorID: req.OperatorID,
			})
			switch {
			case perr == nil:
				plans = append(plans, plan)
				res.ChangedVariants += plan.res.ChangedCount
			case isPricingNothingChanged(perr):

				unchanged++
			case pricingUnpriceable(perr):

				blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
			default:
				return perr
			}
		}
		if len(blocked) > 0 {
			return bulkRejected(blocked)
		}
		if err := s.writePricingPlansTx(ctx, tx, plans); err != nil {
			return err
		}
		if len(plans) > 0 {
			_, err := s.recalcAutoTagsTx(ctx, tx, projectID)
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	res.Done = len(ids)
	res.UnchangedProducts = unchanged
	return res, nil
}

func pricingUnpriceable(err error) bool {
	fact, ok := facing.FactOf(err)
	return ok && fact.Business && fact.Key == productenums.ErrPricingTargetNotFound
}
