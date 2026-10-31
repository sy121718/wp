package productservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/rls"
)

func normalizeBulkIDs(ids []string) ([]string, error) {
	return bulkids.Normalize(ids)
}

func bulkIDsErr(err error) error {
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		return productenums.ErrIDsTooMany(tooMany)
	}
	return productenums.Err(productenums.ErrInvalidParam)
}

func (s *Service) BulkDeleteProducts(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkResultResp{}

	if len(ids) == 0 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	named, err := s.m.ListProductsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(named))
	for _, e := range named {
		names[e.ID] = e.Name
	}
	deleted := 0
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		locked, lerr := s.m.LockProductsTx(ctx, tx, ids, projectID)
		if lerr != nil {
			return lerr
		}

		if len(locked) != len(ids) {
			lockedSet := make(map[string]bool, len(locked))
			for _, id := range locked {
				lockedSet[id] = true
			}
			blocked := make([]facing.BlockedItem, 0, len(ids)-len(locked))
			for _, id := range ids {
				if !lockedSet[id] {
					blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
				}
			}
			return bulkRejected(blocked)
		}
		lockedSet := make(map[string]bool, len(locked))
		for _, id := range locked {
			lockedSet[id] = true
		}

		lockedProducts := make([]*productmodel.ProductEntity, 0, len(locked))
		for _, id := range locked {
			e, perr := s.m.GetForUpdateTx(ctx, tx, id, projectID)
			if perr != nil {
				return perr
			}
			lockedProducts = append(lockedProducts, e)
		}
		lockedVariants := make([]*productmodel.VariantEntity, 0, len(locked))
		for _, id := range locked {
			rows, verr := s.m.ListVariantsForUpdateTx(ctx, tx, id)
			if verr != nil {
				return verr
			}
			lockedVariants = append(lockedVariants, rows...)
		}

		inputs := make([]*masterdatacontract.ChangeInput, 0, len(lockedProducts)+len(lockedVariants))
		for _, e := range lockedProducts {
			if !lockedSet[e.ID] {
				continue
			}
			inputs = append(inputs, productChangeInput(e, masterdataenums.ActionDelete,
				masterdataenums.OriginProduct, "", productChangeSnapshot(e), nil))
		}
		for _, v := range lockedVariants {
			if !lockedSet[v.ProductID] {
				continue
			}
			inputs = append(inputs, variantChangeInput(projectID, v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariant, "", variantChangeSnapshot(v, nil), nil))
		}

		if cerr := s.cleanRelatedRefsTx(ctx, tx, projectID, locked, ""); cerr != nil {
			return cerr
		}
		n, derr := s.m.DeleteProductsTx(ctx, tx, locked)
		if derr != nil {
			return derr
		}
		if int(n) != len(locked) {
			return fmt.Errorf("product: 批量删除商品的行数对不上：锁定 %d，删除 %d", len(locked), n)
		}
		deleted = len(locked)

		targets := make([]invalidationTarget, 0, len(locked))
		for _, id := range locked {
			targets = append(targets, productInvalidationTarget(id))
		}
		if xerr := s.enqueueInvalidationTx(ctx, tx, projectID, targets...); xerr != nil {
			return xerr
		}
		return s.recordChangesTx(ctx, tx, inputs...)
	}); err != nil {
		return nil, err
	}

	res.Done = deleted
	return res, nil
}

func bulkRejected(items []facing.BlockedItem) error {
	return &productdto.BulkRejectedError{
		Err:   productenums.Err(productenums.ErrBulkRejected),
		Items: items,
	}
}

func bulkBlocked(id, name, reasonKey string) facing.BlockedItem {
	return facing.BlockedItem{
		ID: id, Name: strings.TrimSpace(name), ReasonKey: reasonKey,

		Fallback: productenums.FallbackFor(reasonKey),
	}
}

func bulkBlockedNames(ids []string, named map[string]string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = named[id]
	}
	return out
}

func (s *Service) BulkDeleteProductCategories(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkResultResp{}

	if len(ids) == 0 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		rows, err := scoped.m.ListCategoriesByIDs(ctx, ids, projectID)
		if err != nil {
			return err
		}
		childCounts, err := scoped.m.CountCategoryChildrenByIDs(ctx, ids, projectID)
		if err != nil {
			return err
		}
		hits, err := scoped.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
			return scoped.m.ProductRefsByCategoryIDs(ctx, ids, scope)
		})
		if err != nil {
			return err
		}
		present := make(map[string]bool, len(rows))
		names := make(map[string]string, len(rows))
		for _, r := range rows {
			if strings.EqualFold(r.ProjectID, projectID) {
				present[r.ID] = true
				names[r.ID] = r.Name
			}
		}
		blocked := make([]facing.BlockedItem, 0)
		for _, id := range ids {
			switch {
			case !present[id]:
				blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
			case childCounts[id] > 0:
				blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrCategoryHasChildren))
			case hits[id].Referenced():
				blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrCategoryInUse))
			}
		}
		if len(blocked) > 0 {

			return bulkRejected(blocked)
		}
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		n, derr := s.m.DeleteCategoriesTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除分类的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, invalidationTarget{
				EntityType: productcontract.EntityTypeCategory, EntityID: id,
			})
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

func (s *Service) BulkDeleteProductBrands(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkResultResp{}

	if len(ids) == 0 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListBrandsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByBrandIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		present[r.ID] = true
		names[r.ID] = r.Name
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		switch {
		case !present[id]:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case hits[id].Referenced():
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrBrandInUse))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		n, derr := s.m.DeleteBrandsTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除品牌的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, invalidationTarget{
				EntityType: productcontract.EntityTypeBrand, EntityID: id,
			})
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

func (s *Service) BulkDeleteProductAttributes(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkResultResp{}

	if len(ids) == 0 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListAttributesByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByAttributeIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		present[r.ID] = true
		names[r.ID] = r.Name
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		switch {
		case !present[id]:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case hits[id].Referenced():
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrAttrInUse))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		n, derr := s.m.DeleteAttributesTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除属性组的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, attributeInvalidationEntityKey(id))
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

func (s *Service) BulkDeleteProductTags(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkResultResp{}

	if len(ids) == 0 {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListTagsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByTagIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	owner := make(map[string]string, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		owner[r.ID] = r.ProjectID
		names[r.ID] = r.Name
	}
	blocked := make([]facing.BlockedItem, 0)
	groups := map[string][]string{}
	for _, id := range ids {
		pid, ok := owner[id]
		switch {
		case !ok:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case len(hits[id].Outside(pid)) > 0:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrTagCrossProject))
		default:
			groups[pid] = append(groups[pid], id)
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	scopes := make([]string, 0, len(groups))
	for pid := range groups {
		scopes = append(scopes, pid)
	}
	sort.Strings(scopes)
	now := time.Now().UTC()
	if len(scopes) > 0 {
		if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			for _, pid := range scopes {
				if serr := s.productWriteScopeTx(ctx, tx, pid); serr != nil {
					return serr
				}
				gids := groups[pid]
				if _, rerr := s.m.RemoveTagsFromProductsTx(tx, gids, pid, now); rerr != nil {
					return rerr
				}
				targets := make([]invalidationTarget, 0, len(gids))
				for _, id := range gids {
					targets = append(targets, invalidationTarget{
						EntityType: productcontract.EntityTypeTag, EntityID: id,
					})
				}
				if xerr := s.enqueueInvalidationTx(ctx, tx, pid, targets...); xerr != nil {
					return xerr
				}
				n, derr := s.m.DeleteTagsTx(ctx, tx, gids)
				if derr != nil {
					return derr
				}
				if int(n) != len(gids) {
					return fmt.Errorf("product: 批量删除标签的行数对不上：计划 %d，删除 %d", len(gids), n)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	res.Done = len(ids)
	return res, nil
}

func (s *Service) batchRefHits(ctx context.Context, entityIDs []string,
	scan func(projectIDs []string) (map[string]*productmodel.BatchRefHit, error)) (map[string]*productmodel.BatchRefHit, error) {
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return nil, err
	}
	return scan(scope)
}
