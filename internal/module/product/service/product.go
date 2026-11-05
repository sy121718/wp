package productservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryenums "go_wp/internal/module/inventory/enums"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	skuSourceCustom    = "custom"
	skuSourceWarehouse = "warehouse"
)

func normalizeSKUSource(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", skuSourceCustom:
		return skuSourceCustom, nil
	case skuSourceWarehouse:
		return skuSourceWarehouse, nil
	default:
		return "", inventoryenums.Err(inventoryenums.ErrSKUSourceInvalid)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if code := strings.TrimSpace(v); code != "" {
			return code
		}
	}
	return ""
}

func (s *Service) Create(ctx context.Context, req *productdto.CreateReq) (res *productdto.ProductResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, productenums.Err(productenums.ErrNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = deriveSlug(req.Name)
	}
	now := time.Now().UTC()
	if slug == "" {
		slug = "p-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	}
	if taken, serr := s.m.SlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, productenums.Err(productenums.ErrSlugTaken)
	}

	attributeIDs, err := s.resolveAttributeIDs(ctx, projectID, req.AttributeIDs)
	if err != nil {
		return nil, err
	}

	categoryIDs, primaryCategoryID, err := s.applyCategoryRefs(ctx, projectID, req.CategoryIDs, nil, &req.PrimaryCategoryID, nil)
	if err != nil {
		return nil, err
	}
	brandID, err := s.resolveBrandID(ctx, projectID, req.BrandID)
	if err != nil {
		return nil, err
	}

	tagIDs, err := s.resolveTagIDs(ctx, projectID, req.TagIDs)
	if err != nil {
		return nil, err
	}

	productID := uuid.NewString()
	relatedIDs, err := s.resolveRelatedIDs(ctx, projectID, productID, req.RelatedIDs)
	if err != nil {
		return nil, err
	}

	if req.TrackQuantity && req.Quantity == nil {
		zero := 0
		req.Quantity = &zero
	}
	if req.Quantity != nil && *req.Quantity < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}

	skuSource, serr := normalizeSKUSource(req.SKUSource)
	if serr != nil {
		return nil, serr
	}

	customSKU := strings.TrimSpace(req.SKUCode)
	metadata := orJSON(req.Metadata, "{}")
	e := &productmodel.ProductEntity{
		ID: productID, ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Subtitle: req.Subtitle,
		Description: orJSON(req.Description, "{}"), Slug: slug,
		Status: productenums.StatusDraft,
		Unit:   req.Unit, Weight: req.Weight,
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Images: orJSONList(mediaURLs(req.Images)), ImageAlts: orJSONList(req.ImageAlts),
		AttributeIDs:      orJSONList(attributeIDs),
		CategoryIDs:       orJSONList(categoryIDs),
		PrimaryCategoryID: primaryCategoryID,
		BrandID:           brandID,
		TagIDs:            orIDList(tagIDs), RelatedIDs: orIDList(relatedIDs),

		DefaultImage: mediaURL(req.DefaultImage),
		DefaultPrice: req.DefaultPrice,
		Metadata:     metadata,
		CreatedAt:    now, UpdatedAt: now,
	}

	refs, werr := s.resolveWarehouseTargets(ctx, projectID, req.WarehouseIDs, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	claim := prefixWarehouse(refs)

	skuInput := customSKU
	externalSKU := ""
	if skuSource == skuSourceWarehouse {

		picked, perr := s.pickWarehouseSKU(ctx, projectID, claim, req.WarehouseSKU)
		if perr != nil {
			return nil, perr
		}
		skuInput = stripWarehousePrefix(picked.SKUCode, picked.WarehouseCode)

		externalSKU = firstNonEmpty(req.ExternalSKU, picked.ExternalSKU, skuInput)
	} else {

		externalSKU = strings.TrimSpace(req.ExternalSKU)
	}

	if e.SKUCode, werr = buildProductContainerSKU(containerSKUInput{
		Slug: slug, Custom: skuInput, WarehouseCode: refCode(claim),
	}); werr != nil {
		return nil, werr
	}
	if s.invSvc == nil {
		if externalSKU != "" {
			return nil, inventoryenums.Err(inventoryenums.ErrStockWarehouseNeeded)
		}
	} else if externalSKU, werr = s.invSvc.NormalizeExternalSKU(externalSKU); werr != nil {
		return nil, werr
	}

	if werr = s.ensureContainerSKUFree(ctx, projectID, e.SKUCode, ""); werr != nil {
		return nil, werr
	}

	v, verr := s.newVariantFromDefaults(ctx, e, nil, refCode(claim), nil)
	if verr != nil {
		return nil, verr
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		if _, xerr := scoped.validateCategoryIDs(ctx, projectID, decodeStrings(e.CategoryIDs)); xerr != nil {
			return xerr
		}
		if xerr := s.m.CreateWithVariantsTx(ctx, tx, e, []*productmodel.VariantEntity{v}); xerr != nil {
			return mapContainerSKUConflict(xerr)
		}

		if xerr := s.createStockRowsTx(ctx, tx, refs, v, v.SKUCode, externalSKU, req.Quantity); xerr != nil {
			return xerr
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, e.ID); xerr != nil {
			return xerr
		}

		if err := s.recordChangesTx(ctx, tx,
			productChangeInput(e, masterdataenums.ActionCreate, masterdataenums.OriginProduct, req.OperatorID,
				nil, productChangeSnapshot(e)),
			variantChangeInput(projectID, v, masterdataenums.ActionCreate, masterdataenums.OriginVariant, req.OperatorID,
				nil, variantChangeSnapshot(v, claim)),
		); err != nil {
			return err
		}
		_, err := s.recalcAutoTagsTx(ctx, tx, projectID)
		return err
	}); err != nil {
		return nil, err
	}
	return s.toResp(ctx, e)
}

// Update 按显式字段更新主体，数值清空与审计在同一事务中完成。
func (s *Service) Update(ctx context.Context, req *productdto.UpdateReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" || (req.ClearDefaultPrice && req.DefaultPrice != nil) || (req.ClearWeight && req.Weight != nil) {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.Get(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return nil, productenums.Err(productenums.ErrNameRequired)
		}
		e.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.SlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, productenums.Err(productenums.ErrSlugTaken)
		}
		e.Slug = slug
	}
	if req.Subtitle != nil {
		e.Subtitle = *req.Subtitle
	}
	if req.Description != nil {
		e.Description = req.Description
	}
	if req.Status != nil {
		newStatus := strings.TrimSpace(*req.Status)

		if !isProductStatus(newStatus) {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		e.Status = newStatus
	}
	if req.Unit != nil {
		e.Unit = *req.Unit
	}
	if req.ClearWeight {
		e.Weight = nil
	} else if req.Weight != nil {
		e.Weight = req.Weight
	}
	if req.SEOTitle != nil {
		e.SEOTitle = *req.SEOTitle
	}
	if req.SEODescription != nil {
		e.SEODescription = *req.SEODescription
	}
	if req.Images != nil {
		e.Images = orJSONList(mediaURLs(req.Images))
	}
	if req.ImageAlts != nil {
		e.ImageAlts = orJSONList(req.ImageAlts)
	}
	if req.AttributeIDs != nil {
		ids, aerr := s.resolveAttributeIDs(ctx, e.ProjectID, req.AttributeIDs)
		if aerr != nil {
			return nil, aerr
		}
		e.AttributeIDs = orJSONList(ids)
	}
	if req.CategoryIDs != nil || req.PrimaryCategoryID != nil {
		ids, primaryID, aerr := s.applyCategoryRefs(ctx, e.ProjectID, req.CategoryIDs,
			decodeStrings(e.CategoryIDs), req.PrimaryCategoryID, e.PrimaryCategoryID)
		if aerr != nil {
			return nil, aerr
		}
		e.CategoryIDs = orJSONList(ids)
		e.PrimaryCategoryID = primaryID
	}
	if req.TagIDs != nil {

		ids, terr := s.resolveTagIDs(ctx, e.ProjectID, req.TagIDs)
		if terr != nil {
			return nil, terr
		}
		e.TagIDs = orIDList(ids)
	}
	if req.RelatedIDs != nil {

		ids, rerr := s.resolveRelatedIDs(ctx, e.ProjectID, e.ID, req.RelatedIDs)
		if rerr != nil {
			return nil, rerr
		}
		e.RelatedIDs = orIDList(ids)
	}
	if req.BrandID != nil {

		brandID, berr := s.resolveBrandID(ctx, e.ProjectID, *req.BrandID)
		if berr != nil {
			return nil, berr
		}
		e.BrandID = brandID
	}
	if req.ClearDefaultPrice {
		e.DefaultPrice = nil
	} else if req.DefaultPrice != nil {
		e.DefaultPrice = req.DefaultPrice
	}
	if req.DefaultImage != nil {
		e.DefaultImage = mediaURL(*req.DefaultImage)
	}
	if req.Metadata != nil {
		e.Metadata = orJSON(req.Metadata, "{}")
	}
	if req.SKUCode != nil {

		custom := strings.TrimSpace(*req.SKUCode)
		if custom == "" {
			return nil, productenums.Err(productenums.ErrContainerSkuInvalid)
		}

		e.SKUCode = custom

		if err = s.ensureContainerSKUFree(ctx, e.ProjectID, e.SKUCode, e.ID); err != nil {
			return nil, err
		}
	}
	e.UpdatedAt = time.Now().UTC()

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, e.ProjectID); serr != nil {
			return serr
		}

		if xerr := s.m.LockPricingTagsTx(ctx, tx, e.ProjectID, productenums.TagKindRule); xerr != nil {
			return xerr
		}
		locked, lerr := s.m.GetForUpdateTx(ctx, tx, req.ID, e.ProjectID)
		if lerr != nil {
			return mapNotFound(lerr)
		}
		before := productChangeSnapshot(locked)
		preserveUntouchedFields(e, locked, req)
		if req.CategoryIDs != nil || req.PrimaryCategoryID != nil {
			scoped := *s
			scoped.m = productmodel.NewModel(tx)
			ids, primary, xerr := scoped.applyCategoryRefs(ctx, e.ProjectID, req.CategoryIDs, decodeStrings(locked.CategoryIDs), req.PrimaryCategoryID, locked.PrimaryCategoryID)
			if xerr != nil {
				return xerr
			}
			e.CategoryIDs, e.PrimaryCategoryID = orJSONList(ids), primary
		}
		e.PublishedAt = locked.PublishedAt
		e.UpdatedAt = time.Now().UTC()

		if req.Status != nil && e.Status == productenums.StatusPublished && locked.Status != productenums.StatusPublished {
			e.PublishedAt = &e.UpdatedAt
		}
		if xerr := s.m.UpdateTx(ctx, tx, e); xerr != nil {
			return mapContainerSKUConflict(xerr)
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, e.ProjectID, e.ID); xerr != nil {
			return xerr
		}

		if xerr := s.recordChangesTx(ctx, tx, productChangeInput(e, masterdataenums.ActionUpdate,
			masterdataenums.OriginProduct, req.OperatorID, before, productChangeSnapshot(e))); xerr != nil {
			return xerr
		}
		_, xerr := s.recalcAutoTagsTx(ctx, tx, e.ProjectID)
		return xerr
	}); err != nil {
		return nil, err
	}

	return s.toResp(ctx, e)
}

func (s *Service) Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.Get(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	res, err = s.toResp(ctx, e)
	if err != nil {
		return nil, err
	}
	res.Rating, err = s.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: res.ID, ProjectID: res.ProjectID})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) List(ctx context.Context, req *productdto.ListReq) (list []*productdto.ProductResp, err error) {
	paging := pageArgs(req)
	rows, err := s.m.List(ctx, req.ProjectID, strings.TrimSpace(req.Keyword), req.Status, paging.Size, paging.Offset)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}

	variants, err := s.m.ListVariantPriceFacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}

	attrs, err := s.attributeRespByProduct(ctx, req.ProjectID, rows)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.ProductResp, 0, len(rows))
	for _, r := range rows {
		resp := s.toListResp(r)
		resp.Attributes = attrs[r.ID]
		applyPriceRange(resp, byProduct[r.ID])
		list = append(list, resp)
	}

	s.fillProductStock(ctx, req.ProjectID, list)
	return list, nil
}

func (s *Service) CountProducts(ctx context.Context, req *productdto.ListReq) (n int64, err error) {
	if req == nil {
		return 0, productenums.Err(productenums.ErrInvalidParam)
	}
	return s.m.Count(ctx, req.ProjectID, strings.TrimSpace(req.Keyword), req.Status)
}

func (s *Service) Delete(ctx context.Context, req *productdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return gerr
	}

	ref, gerr := s.m.GetRefFacts(ctx, req.ID, projectID)
	if gerr != nil {
		return mapNotFound(gerr)
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, ref.ProjectID); serr != nil {
			return serr
		}
		e, lerr := s.m.GetForUpdateTx(ctx, tx, req.ID, ref.ProjectID)
		if lerr != nil {

			return mapNotFound(lerr)
		}
		variants, verr := s.m.ListVariantsForUpdateTx(ctx, tx, e.ID)
		if verr != nil {
			return verr
		}
		inputs := make([]*masterdatacontract.ChangeInput, 0, len(variants)+1)
		inputs = append(inputs, productChangeInput(e, masterdataenums.ActionDelete,
			masterdataenums.OriginProduct, req.OperatorID, productChangeSnapshot(e), nil))
		for _, v := range variants {
			inputs = append(inputs, variantChangeInput(e.ProjectID, v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariant, req.OperatorID, variantChangeSnapshot(v, nil), nil))
		}

		if cerr := s.cleanRelatedRefsTx(ctx, tx, e.ProjectID, []string{e.ID}, req.OperatorID); cerr != nil {
			return cerr
		}
		if derr := s.m.DeleteTx(ctx, tx, req.ID); derr != nil {
			return derr
		}

		if derr := s.enqueueProductInvalidationTx(ctx, tx, e.ProjectID, req.ID); derr != nil {
			return derr
		}
		return s.recordChangesTx(ctx, tx, inputs...)
	}); err != nil {
		return err
	}

	return nil
}

func (s *Service) cleanRelatedRefsTx(ctx context.Context, tx *gorm.DB, projectID string, deletedIDs []string, operatorID string) error {
	if len(deletedIDs) == 0 {
		return nil
	}
	refs, err := s.m.ListRelatedReferrersTx(ctx, tx, projectID, deletedIDs)
	if err != nil || len(refs) == 0 {
		return err
	}
	drop := make(map[string]bool, len(deletedIDs))
	for _, id := range deletedIDs {
		drop[id] = true
	}
	for _, ref := range refs {

		if ref == nil || drop[ref.ID] {
			continue
		}
		before := decodeStrings(ref.RelatedIDs)
		after := make([]string, 0, len(before))
		for _, id := range before {
			if !drop[id] {
				after = append(after, id)
			}
		}
		if len(after) == len(before) {
			continue
		}
		if uerr := s.m.UpdateRelatedIDsTx(ctx, tx, projectID, ref.ID, orIDList(after)); uerr != nil {
			return uerr
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, ref.ID); xerr != nil {
			return xerr
		}
		input := productChangeInput(ref, masterdataenums.ActionUpdate, masterdataenums.OriginProduct, operatorID,
			masterdatacontract.NewSnapshot("related_ids", strings.Join(before, ",")),
			masterdatacontract.NewSnapshot("related_ids", strings.Join(after, ",")))
		if rerr := s.recordChangesTx(ctx, tx, input); rerr != nil {
			return rerr
		}
	}
	return nil
}

func (s *Service) pickWarehouseSKU(ctx context.Context, projectID string, ref *productcontract.WarehouseRef, warehouseSKU string) (res *inventorycontract.WarehouseSKUResp, err error) {
	code := strings.TrimSpace(warehouseSKU)
	if code == "" || s.invSvc == nil || ref == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseSKURequired)
	}
	return s.invSvc.GetWarehouseSKU(ctx, &inventorycontract.GetWarehouseSKUReq{
		ProjectID: projectID, WarehouseID: ref.ID, SKUCode: code,
	})
}

func (s *Service) resolveWarehouseTargets(ctx context.Context, projectID string, ids []string, legacyID string) (refs []*productcontract.WarehouseRef, err error) {
	targets := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			targets = append(targets, trimmed)
		}
	}
	if len(targets) == 0 {
		targets = append(targets, strings.TrimSpace(legacyID))
	}
	seen := make(map[string]bool, len(targets))
	for _, id := range targets {
		ref, rerr := s.resolveWarehouseRef(ctx, projectID, id)
		if rerr != nil {
			return nil, rerr
		}
		if ref == nil || ref.ID == "" || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		refs = append(refs, ref)
	}
	return refs, nil
}

func prefixWarehouse(refs []*productcontract.WarehouseRef) *productcontract.WarehouseRef {
	if len(refs) == 0 {
		return nil
	}
	return refs[0]
}

func (s *Service) createStockRowsTx(ctx context.Context, tx *gorm.DB, refs []*productcontract.WarehouseRef,
	v *productmodel.VariantEntity, productSKU, externalSKU string, quantity *int) (err error) {
	if s.invSvc == nil {
		return nil
	}

	bare := stripWarehousePrefix(productSKU, refCode(prefixWarehouse(refs)))
	for i, ref := range refs {
		if ref == nil {
			continue
		}

		ext := ""
		if i == 0 {
			ext = strings.TrimSpace(externalSKU)

			if ext != "" {
				if xerr := s.invSvc.CheckExternalSKUProductScope(ctx, ref.ProjectID, ref.ID, ext, v.ProductID); xerr != nil {
					return xerr
				}
			}
		}

		if _, xerr := s.attachWarehouseStockTx(ctx, tx, ref, v, bare, ext, quantity); xerr != nil {
			return xerr
		}
	}
	return nil
}

func (s *Service) ensureContainerSKUFree(ctx context.Context, projectID, skuCode, excludeID string) (err error) {
	taken, qerr := s.m.SKUCodeExists(ctx, projectID, skuCode, excludeID)
	if qerr != nil {
		return qerr
	}
	if taken {
		return productenums.Err(productenums.ErrContainerSKUTaken)
	}
	return nil
}

func mapContainerSKUConflict(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return productenums.Err(productenums.ErrContainerSKUTaken)
	}
	msg := err.Error()
	if strings.Contains(msg, "23505") || strings.Contains(msg, "uq_products_project_sku_code") {
		return productenums.Err(productenums.ErrContainerSKUTaken)
	}
	return err
}

func (s *Service) resolveRelatedIDs(ctx context.Context, projectID, selfID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	seen := make(map[string]bool, len(ids))
	dedup := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
	}
	if len(dedup) == 0 {
		return out, nil
	}

	rows, lerr := s.m.ListIDFactsByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductEntity, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		byID[r.ID] = r
	}
	var selfRefs, missing, crossProject, unresolved []string
	for _, id := range dedup {
		if selfID != "" && id == selfID {
			selfRefs = append(selfRefs, id)
			continue
		}
		row, ok := byID[id]
		if !ok {

			unresolved = append(unresolved, id)
			continue
		}
		if projectID != "" && row.ProjectID != projectID {

			crossProject = append(crossProject, id+"@"+row.ProjectID)
			continue
		}
		out = append(out, id)
	}

	if len(unresolved) > 0 {
		owners, oerr := s.lookupRelatedOwners(ctx, unresolved)
		if oerr != nil {
			return nil, oerr
		}
		for _, id := range unresolved {
			if owner, ok := owners[id]; ok {
				crossProject = append(crossProject, id+"@"+owner)
				continue
			}
			missing = append(missing, id)
		}
	}
	if len(selfRefs)+len(missing)+len(crossProject) == 0 {
		return out, nil
	}

	parts := make([]string, 0, 3)
	if len(selfRefs) > 0 {
		parts = append(parts, i18n.ErrorDetail(productenums.DetailRelatedSelfReference,
			"n", strconv.Itoa(len(selfRefs)), "ids", strings.Join(selfRefs, ", ")))
	}
	if len(missing) > 0 {
		parts = append(parts, i18n.ErrorDetail(productenums.DetailRelatedMissing,
			"n", strconv.Itoa(len(missing)), "ids", strings.Join(missing, ", ")))
	}
	if len(crossProject) > 0 {
		parts = append(parts, i18n.ErrorDetail(productenums.DetailRelatedCrossProject,
			"n", strconv.Itoa(len(crossProject)), "ids", strings.Join(crossProject, "；")))
	}
	return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrRelatedInvalid), i18n.JoinErrorDetails(parts...))
}

func (s *Service) lookupRelatedOwners(ctx context.Context, ids []string) (owners map[string]string, err error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	scope, serr := s.crossProjectRefScope(ctx)
	if serr != nil {
		return nil, serr
	}
	return s.m.LookupIDOwners(ctx, scope, ids)
}

func (s *Service) attributeRespByProduct(ctx context.Context, projectID string, products []*productmodel.ProductEntity) (out map[string][]*productdto.AttributeResp, err error) {
	out = map[string][]*productdto.AttributeResp{}
	idSet := map[string]bool{}
	for _, p := range products {
		for _, id := range decodeStrings(p.AttributeIDs) {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	rows, lerr := s.m.ListAttributesByIDs(ctx, ids, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productdto.AttributeResp, len(rows))
	for _, r := range rows {
		byID[r.ID] = toAttributeResp(r)
	}
	for _, p := range products {
		refs := decodeStrings(p.AttributeIDs)
		if len(refs) == 0 {
			continue
		}
		items := make([]*productdto.AttributeResp, 0, len(refs))
		for _, id := range refs {
			if a, ok := byID[id]; ok {
				items = append(items, a)
			}
		}
		if len(items) > 0 {
			out[p.ID] = items
		}
	}
	return out, nil
}

func (s *Service) toResp(ctx context.Context, e *productmodel.ProductEntity) (res *productdto.ProductResp, err error) {
	variants, err := s.m.ListVariants(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	resp := s.toListResp(e)
	resp.ImageAlts = decodeStrings(e.ImageAlts)
	resp.Description = orJSON(e.Description, "{}")
	resp.Subtitle = e.Subtitle
	resp.Unit = e.Unit
	resp.Weight = e.Weight
	resp.SEOTitle = e.SEOTitle
	resp.SEODescription = e.SEODescription
	resp.AttributeIDs = decodeStrings(e.AttributeIDs)
	resp.CategoryIDs = decodeStrings(e.CategoryIDs)
	if e.PrimaryCategoryID != nil {
		resp.PrimaryCategoryID = *e.PrimaryCategoryID
	}
	resp.TagIDs = decodeStrings(e.TagIDs)
	if e.PublishedAt != nil {
		if e.PublishedAt != nil {
			resp.PublishedAt = utils.NewJSONTime(*e.PublishedAt)
		}
	}
	resp.RelatedIDs = decodeStrings(e.RelatedIDs)

	resp.SKUCode = e.SKUCode
	resp.DefaultPrice = e.DefaultPrice
	resp.DefaultImage = mediaURL(e.DefaultImage)
	resp.Metadata = orJSON(e.Metadata, "{}")
	if e.BrandID != nil {
		resp.BrandID = *e.BrandID
	}
	for _, v := range variants {
		resp.Variants = append(resp.Variants, toVariantResp(v))
	}

	if err := s.fillVariantListStocks(ctx, resp.ProjectID, resp.Variants); err != nil {
		return nil, err
	}

	s.fillProductStock(ctx, resp.ProjectID, []*productdto.ProductResp{resp})

	if groups, aerr := s.attributeRespByProduct(ctx, e.ProjectID, []*productmodel.ProductEntity{e}); aerr != nil {
		return nil, aerr
	} else {
		resp.Attributes = groups[e.ID]
	}
	applyPriceRange(resp, variants)

	if rerr := s.fillRelated(ctx, resp, e); rerr != nil {
		return nil, rerr
	}
	return resp, nil
}

func (s *Service) fillProductStock(ctx context.Context, projectID string, resps []*productdto.ProductResp) {
	if s.invSvc == nil || len(resps) == 0 || strings.TrimSpace(projectID) == "" {
		return
	}
	ids := make([]string, 0, len(resps))
	for _, r := range resps {
		if r != nil && r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := s.invSvc.WarehouseStocksByProducts(ctx, projectID, ids)
	if err != nil {

		logger.SceneCtx("product", ctx).With("project", projectID).With("products", len(ids)).
			Error(err, "商品库存投影读取失败（库存列按空值显示；显示口径见 DEC-02）")

		for _, resp := range resps {
			if resp != nil {
				resp.StockUnknown = true
			}
		}
		return
	}
	byProduct := make(map[string][]inventorycontract.ProductWarehouseStock, len(ids))
	for _, row := range rows {
		byProduct[row.ProductID] = append(byProduct[row.ProductID], row)
	}
	for _, resp := range resps {
		if resp == nil {
			continue
		}
		items := byProduct[resp.ID]
		if len(items) == 0 {
			resp.StockState = productenums.StockStateNone
			continue
		}
		state, total := aggregateStockState(items)
		resp.StockState = state
		if state == productenums.StockStateTracked {
			resp.StockTotal = total
		}
		resp.StockWarehouses = warehouseStockRows(items)
	}
}

func aggregateStockState(rows []inventorycontract.ProductWarehouseStock) (state string, total int) {
	if len(rows) == 0 {
		return productenums.StockStateNone, 0
	}
	for _, r := range rows {
		if !r.TrackQuantity {
			return productenums.StockStateInfinite, 0
		}
		total += r.Quantity
	}
	return productenums.StockStateTracked, total
}

func warehouseStockRows(rows []inventorycontract.ProductWarehouseStock) []*productdto.ProductWarehouseStockResp {
	order := make([]string, 0, len(rows))
	grouped := make(map[string][]inventorycontract.ProductWarehouseStock, len(rows))
	for _, r := range rows {
		if _, ok := grouped[r.WarehouseID]; !ok {
			order = append(order, r.WarehouseID)
		}
		grouped[r.WarehouseID] = append(grouped[r.WarehouseID], r)
	}
	out := make([]*productdto.ProductWarehouseStockResp, 0, len(order))
	for _, id := range order {
		items := grouped[id]
		state, total := aggregateStockState(items)
		row := &productdto.ProductWarehouseStockResp{WarehouseID: id, State: state}
		for _, it := range items {
			if row.WarehouseCode == "" {
				row.WarehouseCode, row.WarehouseName = it.WarehouseCode, it.WarehouseName
			}
			if row.SKUCode == "" {
				row.SKUCode = it.SKUCode
			}
			if row.CostPrice == nil {
				row.CostPrice = it.CostPrice
			}
		}
		if state == productenums.StockStateTracked {
			row.TrackQuantity = true
			row.Quantity = total
		}
		out = append(out, row)
	}
	return out
}

func (s *Service) fillRelated(ctx context.Context, resp *productdto.ProductResp, e *productmodel.ProductEntity) (err error) {
	ids := decodeStrings(e.CategoryIDs)
	if len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids, e.ProjectID)
		if cerr != nil {
			return cerr
		}
		byID := make(map[string]*productmodel.ProductCategoryEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range ids {
			if row, ok := byID[id]; ok {
				resp.Categories = append(resp.Categories, toCategoryResp(row))
			}
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {
			resp.Brand = toBrandResp(row)
		}
	}
	tagIDs := decodeStrings(e.TagIDs)
	if len(tagIDs) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, tagIDs, e.ProjectID)
		if terr != nil {
			return terr
		}
		byID := make(map[string]*productmodel.ProductTagEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range tagIDs {
			if row, ok := byID[id]; ok {
				resp.Tags = append(resp.Tags, toTagResp(row))
			}
		}
	}
	return nil
}

func (s *Service) toListResp(e *productmodel.ProductEntity) *productdto.ProductResp {
	resp := &productdto.ProductResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Slug: e.Slug,

		SKUCode: e.SKUCode,

		DefaultPrice: e.DefaultPrice, DefaultImage: e.DefaultImage,
		Status: e.Status, Sort: e.Sort, Images: mediaURLs(decodeStrings(e.Images)),
		ImageAlts: decodeStrings(e.ImageAlts), AttributeIDs: decodeStrings(e.AttributeIDs),
		CategoryIDs: decodeStrings(e.CategoryIDs), TagIDs: decodeStrings(e.TagIDs),
		CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
	if e.PrimaryCategoryID != nil {
		resp.PrimaryCategoryID = *e.PrimaryCategoryID
	}
	if e.BrandID != nil {
		resp.BrandID = *e.BrandID
	}
	if len(resp.Images) == 0 {
		resp.Images = []string{}
	}
	return resp
}

func applyPriceRange(resp *productdto.ProductResp, variants []*productmodel.VariantEntity) {
	resp.VariantCount = len(variants)
	if len(variants) == 0 {
		return
	}

	summary := summarizeVariantPrices(variants, false)
	if !summary.HasAny {
		return
	}
	resp.PriceMin, resp.PriceMax = summary.MinPrice, summary.MaxPrice
}

func toVariantResp(v *productmodel.VariantEntity) *productdto.VariantResp {
	return &productdto.VariantResp{
		ID: v.ID, ProductID: v.ProductID, SKUCode: v.SKUCode, Barcode: v.Barcode,
		Price: v.Price, ComparePrice: v.ComparePrice, CostPrice: v.CostPrice,
		Image: v.Image, OptionValues: orJSON(v.OptionValues, "{}"),
		Enabled: v.Enabled, Sort: v.Sort,
		CreatedAt: utils.NewJSONTime(v.CreatedAt), UpdatedAt: utils.NewJSONTime(v.UpdatedAt),
	}
}

const maxRatingScore = 5

func (s *Service) AddRating(ctx context.Context, req *productdto.AddRatingReq) (res *productdto.RatingResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}

	if req.Score == nil || math.IsNaN(*req.Score) || math.IsInf(*req.Score, 0) || *req.Score < 0 || *req.Score > maxRatingScore {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	product, err := s.m.GetRefFacts(ctx, strings.TrimSpace(req.ProductID), projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}
	row := &productmodel.ProductRatingEntity{
		ID:        uuid.NewString(),
		ProjectID: product.ProjectID,
		ProductID: product.ID,
		Score:     *req.Score,
		Source:    source,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, product.ProjectID); serr != nil {
			return serr
		}
		if _, lerr := s.m.GetForUpdateTx(ctx, tx, product.ID, product.ProjectID); lerr != nil {
			return mapNotFound(lerr)
		}
		if cerr := s.m.CreateRatingTx(ctx, tx, row); cerr != nil {
			return cerr
		}
		if ierr := s.enqueueProductInvalidationTx(ctx, tx, product.ProjectID, product.ID); ierr != nil {
			return ierr
		}
		rows, rerr := s.m.ListRatingsTx(ctx, tx, product.ID, product.ProjectID)
		if rerr != nil {
			return rerr
		}
		res = ratingRespFromRows(product.ProjectID, product.ID, rows)
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) ListRatings(ctx context.Context, req *productdto.ListRatingsReq) (res *productdto.RatingResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	if _, err = s.m.GetRefFacts(ctx, strings.TrimSpace(req.ProductID), projectID); err != nil {
		return nil, mapNotFound(err)
	}
	return s.ratingRespOf(ctx, projectID, strings.TrimSpace(req.ProductID))
}

// DeleteRating 按商品串行修改评分，删除、失效事件和返回事实共用同一事务。
func (s *Service) DeleteRating(ctx context.Context, req *productdto.DeleteRatingReq) (res *productdto.RatingResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rating, err := s.m.GetRating(ctx, strings.TrimSpace(req.ID), projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		if _, lerr := s.m.GetForUpdateTx(ctx, tx, rating.ProductID, projectID); lerr != nil {
			return mapNotFound(lerr)
		}
		deleted, derr := s.m.DeleteRatingTx(ctx, tx, rating.ID, projectID)
		if derr != nil {
			return mapNotFound(derr)
		}
		if ierr := s.enqueueProductInvalidationTx(ctx, tx, projectID, deleted.ProductID); ierr != nil {
			return ierr
		}
		rows, rerr := s.m.ListRatingsTx(ctx, tx, deleted.ProductID, projectID)
		if rerr != nil {
			return rerr
		}
		res = ratingRespFromRows(projectID, deleted.ProductID, rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) ratingRespOf(ctx context.Context, projectID, productID string) (res *productdto.RatingResp, err error) {
	rows, err := s.m.ListRatings(ctx, productID, projectID)
	if err != nil {
		return nil, err
	}
	return ratingRespFromRows(projectID, productID, rows), nil
}

func ratingRespFromRows(projectID, productID string, rows []*productmodel.ProductRatingEntity) *productdto.RatingResp {
	items := make([]productmodel.ProductRatingEntity, 0, len(rows))
	out := make([]productdto.RatingItemResp, 0, len(rows))
	for _, row := range rows {
		items = append(items, *row)
		out = append(out, productdto.RatingItemResp{
			ID: row.ID, ProductID: row.ProductID, Score: row.Score, Source: row.Source,
			CreatedAt: utils.NewJSONTime(row.CreatedAt),
		})
	}
	res := &productdto.RatingResp{ProjectID: projectID, ProductID: productID, Items: out}
	if avg, count, ok := productmodel.RatingSummary(items); ok {
		res.Rating, res.RatingCount, res.HasRating = avg, count, true
	}
	return res
}

var _ productcontract.SearchPort = (*Service)(nil)

func (s *Service) SearchPublishedProducts(ctx context.Context, projectID, keyword string, limit int) (hits []*productcontract.ProductSearchHit, err error) {
	projectID = strings.TrimSpace(projectID)
	keyword = strings.TrimSpace(keyword)
	if projectID == "" || keyword == "" {
		return nil, nil
	}
	rows, err := s.m.SearchPublished(ctx, projectID, keyword, limit)
	if err != nil {
		return nil, err
	}
	hits = make([]*productcontract.ProductSearchHit, 0, len(rows))
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.ID) == "" {
			continue
		}
		hits = append(hits, &productcontract.ProductSearchHit{
			ID: row.ID, Name: row.Name, Subtitle: row.Subtitle,
			Slug: row.Slug, DefaultImage: row.DefaultImage,
		})
	}
	return hits, nil
}

var _ commentcontract.EntityPolicy = (*Service)(nil)

func (s *Service) SetPurchaseChecker(port productcontract.PurchaseChecker) {
	s.purchases = port
}

func (s *Service) AllowComment(ctx context.Context, req *commentcontract.PolicyReq) (denial *commentcontract.PolicyDenial, err error) {
	if req == nil || strings.TrimSpace(req.EntityType) != productcontract.EntityTypeProduct {
		return nil, nil
	}
	if s.purchases == nil {

		return nil, nil
	}
	purchased, cerr := s.purchases.HasPurchasedProduct(ctx, req.ProjectID, req.UserID, req.EntityID)
	if cerr != nil {

		logger.Scene("product").Error(cerr, "商品评论的购买事实判定失败（本次放行，待审核队列把关）")
		return nil, nil
	}
	if purchased {
		return nil, nil
	}
	return &commentcontract.PolicyDenial{
		Message:  productenums.ErrCommentPurchaseRequired,
		Fallback: "购买过该商品才能发表评论",
	}, nil
}

func (s *Service) UpdateVariantCost(ctx context.Context, projectID, variantID string, cost float64, operatorID string) (err error) {
	if strings.TrimSpace(variantID) == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}

	var (
		v        *productmodel.VariantEntity
		resolved string
	)
	if v, err = s.m.GetVariant(ctx, variantID); err != nil {
		return mapNotFound(err)
	}

	if resolved, err = s.variantProjectID(ctx, v, projectID); err != nil {
		return err
	}
	now := time.Now().UTC()

	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, resolved); serr != nil {
			return serr
		}

		if _, lerr := s.m.GetForUpdateTx(ctx, tx, v.ProductID, resolved); lerr != nil {
			return mapNotFound(lerr)
		}
		locked, lerr := s.m.GetVariantForUpdateTx(ctx, tx, variantID, v.ProductID)
		if lerr != nil {
			return mapNotFound(lerr)
		}
		v = locked
		before := variantChangeSnapshot(v, nil)
		after := variantChangeSnapshot(v, nil)
		after["cost_price"] = masterdatacontract.FormatPrice(cost)
		if uerr := s.m.UpdateVariantCostTx(ctx, tx, variantID, cost, now); uerr != nil {
			return uerr
		}
		return s.recordChangesTx(ctx, tx, variantChangeInput(resolved, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginReceipt, operatorID, before, after))
	})
}

func (s *Service) ProductTranslationCandidates(ctx context.Context, projectID, productID string) (list []productcontract.TranslationCandidate, err error) {
	e, gerr := s.m.Get(ctx, productID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	return s.translationCandidatesForProduct(ctx, e)
}

func (s *Service) ProjectTranslationCandidates(ctx context.Context, projectID string) (list []productcontract.TranslationCandidate, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, nil
	}

	rows, lerr := s.m.ListAllColumnsForTranslation(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	seen := map[string]bool{}
	for _, row := range rows {
		cands, cerr := s.translationCandidatesForProduct(ctx, row)
		if cerr != nil {
			return nil, cerr
		}
		list = appendUniqueCandidates(list, seen, cands)
	}

	extra, eerr := s.orphanTranslationCandidates(ctx, projectID, seen)
	if eerr != nil {
		return nil, eerr
	}
	return append(list, extra...), nil
}

func (s *Service) translationCandidatesForProduct(ctx context.Context, e *productmodel.ProductEntity) (list []productcontract.TranslationCandidate, err error) {
	if e == nil {
		return nil, nil
	}
	list = append(list, productTextCandidates(e)...)

	categoryRows, cerr := s.m.ListCategoriesByIDs(ctx, decodeStrings(e.CategoryIDs), e.ProjectID)
	if cerr != nil {
		return nil, cerr
	}
	for _, row := range categoryRows {
		list = append(list, entityTextCandidates(productcontract.EntityTypeCategory, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle})...)
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {
			list = append(list, entityTextCandidates(productcontract.EntityTypeBrand, row.ID, row.Name,
				map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle})...)
		}
	}
	tagRows, terr := s.m.ListTagsByIDs(ctx, decodeStrings(e.TagIDs), e.ProjectID)
	if terr != nil {
		return nil, terr
	}
	for _, row := range tagRows {
		list = append(list, entityTextCandidates(productcontract.EntityTypeTag, row.ID, row.Name,
			map[string]string{"name": row.Name})...)
	}
	attrRows, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	for _, row := range attrRows {
		list = append(list, attributeTextCandidates(row)...)
	}
	return list, nil
}

func (s *Service) orphanTranslationCandidates(ctx context.Context, projectID string, seen map[string]bool) (list []productcontract.TranslationCandidate, err error) {
	categories, cerr := s.m.ListCategories(ctx, projectID, "")
	if cerr != nil {
		return nil, cerr
	}
	for _, row := range categories {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeCategory, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle}))
	}

	brands, berr := s.m.ListBrands(ctx, projectID, "", 0, 0)
	if berr != nil {
		return nil, berr
	}
	for _, row := range brands {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeBrand, row.ID, row.Name,
			map[string]string{"name": row.Name, "description": row.Description, "seoTitle": row.SEOTitle}))
	}
	tags, terr := s.m.ListTags(ctx, projectID, "", "", 0, 0)
	if terr != nil {
		return nil, terr
	}
	for _, row := range tags {
		list = appendUniqueCandidates(list, seen, entityTextCandidates(productcontract.EntityTypeTag, row.ID, row.Name,
			map[string]string{"name": row.Name}))
	}
	attrs, aerr := s.m.ListAttributesByProject(ctx, projectID)
	if aerr != nil {
		return nil, aerr
	}
	for _, row := range attrs {
		list = appendUniqueCandidates(list, seen, attributeTextCandidates(row))
	}
	return list, nil
}

func appendUniqueCandidates(dst []productcontract.TranslationCandidate, seen map[string]bool, add []productcontract.TranslationCandidate) []productcontract.TranslationCandidate {
	for _, c := range add {
		key := i18n.ContentIndexKey(c.SourceHash, c.Context)
		if seen[key] {
			continue
		}
		seen[key] = true
		dst = append(dst, c)
	}
	return dst
}

func productTextCandidates(e *productmodel.ProductEntity) []productcontract.TranslationCandidate {
	values := map[string]string{
		"name":        e.Name,
		"subtitle":    e.Subtitle,
		"description": descriptionHTML(e.Description),
	}
	list := make([]productcontract.TranslationCandidate, 0, 8)
	for _, field := range productcontract.TranslatableFields(productcontract.EntityTypeProduct) {
		if field == "imageAlts" {
			for _, alt := range decodeStrings(e.ImageAlts) {
				if c, ok := makeCandidate(productcontract.EntityTypeProduct, e.ID, e.Name, field, alt); ok {
					list = append(list, c)
				}
			}
			continue
		}
		if c, ok := makeCandidate(productcontract.EntityTypeProduct, e.ID, e.Name, field, values[field]); ok {
			list = append(list, c)
		}
	}
	return list
}

func entityTextCandidates(entityType, id, name string, values map[string]string) []productcontract.TranslationCandidate {
	list := make([]productcontract.TranslationCandidate, 0, len(values))
	for _, field := range productcontract.TranslatableFields(entityType) {
		if c, ok := makeCandidate(entityType, id, name, field, values[field]); ok {
			list = append(list, c)
		}
	}
	return list
}

func attributeTextCandidates(e *productmodel.ProductAttributeEntity) []productcontract.TranslationCandidate {
	if e == nil {
		return nil
	}
	list := make([]productcontract.TranslationCandidate, 0, 4)
	if c, ok := makeCandidate(productcontract.EntityTypeAttribute, e.ID, e.Name, "name", e.Name); ok {
		list = append(list, c)
	}
	for _, v := range normalizeValuesFromRaw(e.Values) {
		if c, ok := makeCandidate(productcontract.EntityTypeAttribute, e.ID, e.Name, "values", v.Label); ok {
			list = append(list, c)
		}
	}
	return list
}

func makeCandidate(entityType, id, name, field, source string) (productcontract.TranslationCandidate, bool) {
	if !productcontract.IsTranslatableField(entityType, field) {
		return productcontract.TranslationCandidate{}, false
	}
	if !i18n.ShouldTranslateContent(source) {
		return productcontract.TranslationCandidate{}, false
	}
	return productcontract.TranslationCandidate{
		EntityType: entityType, EntityID: id, EntityName: name,
		Field:      field,
		Context:    productcontract.FieldContext(entityType, field),
		SourceText: source, SourceHash: i18n.ContentHash(source),
	}, true
}

func preserveUntouchedFields(e, locked *productmodel.ProductEntity, req *productdto.UpdateReq) {
	if e == nil || locked == nil || req == nil {
		return
	}
	if req.Name == nil {
		e.Name = locked.Name
	}
	if req.Subtitle == nil {
		e.Subtitle = locked.Subtitle
	}
	if len(req.Description) == 0 {
		e.Description = locked.Description
	}
	if req.Slug == nil {
		e.Slug = locked.Slug
	}
	if req.SKUCode == nil {
		e.SKUCode = locked.SKUCode
	}
	if req.Status == nil {
		e.Status = locked.Status
	}
	if req.Unit == nil {
		e.Unit = locked.Unit
	}
	if req.Weight == nil && !req.ClearWeight {
		e.Weight = locked.Weight
	}
	if req.SEOTitle == nil {
		e.SEOTitle = locked.SEOTitle
	}
	if req.SEODescription == nil {
		e.SEODescription = locked.SEODescription
	}
	if req.Images == nil {
		e.Images = locked.Images
	}
	if req.ImageAlts == nil {
		e.ImageAlts = locked.ImageAlts
	}
	if req.AttributeIDs == nil {
		e.AttributeIDs = locked.AttributeIDs
	}

	if req.CategoryIDs == nil && req.PrimaryCategoryID == nil {
		e.CategoryIDs = locked.CategoryIDs
		e.PrimaryCategoryID = locked.PrimaryCategoryID
	}
	if req.TagIDs == nil {
		e.TagIDs = locked.TagIDs
	}
	if req.RelatedIDs == nil {
		e.RelatedIDs = locked.RelatedIDs
	}
	if req.BrandID == nil {
		e.BrandID = locked.BrandID
	}
	if req.DefaultPrice == nil && !req.ClearDefaultPrice {
		e.DefaultPrice = locked.DefaultPrice
	}
	if req.DefaultImage == nil {
		e.DefaultImage = locked.DefaultImage
	}
	if len(req.Metadata) == 0 {
		e.Metadata = locked.Metadata
	}
}

func isProductStatus(s string) bool {
	switch s {
	case productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived:
		return true
	}
	return false
}
