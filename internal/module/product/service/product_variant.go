package productservice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"hash/fnv"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

const maxSKUProbe = 500

const (
	VariantSKUSuffix = "_V"

	skuSeparator = "_"
)

func (s *Service) CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ProductID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	p, err := s.m.Get(ctx, req.ProductID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}

	ref, err := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	v, verr := s.newVariantFromDefaults(ctx, p, req, refCode(ref), nil)
	if verr != nil {
		return nil, verr
	}
	if taken, serr := s.m.SKUExists(ctx, p.ID, v.SKUCode, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, productenums.Err(productenums.ErrSkuTaken)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, p.ProjectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateVariantTx(ctx, tx, v); cerr != nil {
			return cerr
		}

		if serr := s.ensureVariantStockTx(ctx, tx, ref, v, v.SKUCode, req.Quantity); serr != nil {
			return serr
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, p.ProjectID, p.ID); xerr != nil {
			return xerr
		}

		if err := s.recordChangesTx(ctx, tx, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariant, req.OperatorID, nil, variantChangeSnapshot(v, ref))); err != nil {
			return err
		}
		_, err := s.recalcAutoTagsTx(ctx, tx, p.ProjectID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return toVariantResp(v), nil
}

func (s *Service) UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	v, err := s.m.GetVariant(ctx, req.ID)
	if err != nil {
		return nil, mapNotFound(err)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if req.SKUCode != nil {
		code := strings.TrimSpace(*req.SKUCode)
		if code == "" {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.SKUExists(ctx, v.ProductID, code, v.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, productenums.Err(productenums.ErrSkuTaken)
		}
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}

		if xerr := s.m.LockPricingTagsTx(ctx, tx, projectID, productenums.TagKindRule); xerr != nil {
			return xerr
		}
		if _, xerr := s.m.GetForUpdateTx(ctx, tx, v.ProductID, projectID); xerr != nil {
			return mapNotFound(xerr)
		}
		locked, lerr := s.m.GetVariantForUpdateTx(ctx, tx, v.ID, v.ProductID)
		if lerr != nil {
			return mapNotFound(lerr)
		}
		v = locked
		before := variantChangeSnapshot(v, nil)
		if req.SKUCode != nil {
			v.SKUCode = strings.TrimSpace(*req.SKUCode)
		}
		if req.Barcode != nil {
			v.Barcode = *req.Barcode
		}
		if req.Price != nil {
			v.Price = *req.Price
		}
		if req.ComparePrice != nil {
			v.ComparePrice = req.ComparePrice
		}
		if req.CostPrice != nil {
			v.CostPrice = req.CostPrice
		}
		if req.Image != nil {
			v.Image = mediaURL(*req.Image)
		}
		if req.OptionValues != nil {
			v.OptionValues = req.OptionValues
		}
		if req.Enabled != nil {
			v.Enabled = *req.Enabled
		}
		if req.Sort != nil {
			v.Sort = *req.Sort
		}
		v.UpdatedAt = time.Now().UTC()
		if uerr := s.m.UpdateVariantTx(ctx, tx, v); uerr != nil {
			return uerr
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, v.ProductID); xerr != nil {
			return xerr
		}

		if xerr := s.recordChangesTx(ctx, tx, variantChangeInput(projectID, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginVariant, req.OperatorID, before, variantChangeSnapshot(v, nil))); xerr != nil {
			return xerr
		}
		_, xerr := s.recalcAutoTagsTx(ctx, tx, projectID)
		return xerr
	})
	if err != nil {
		return nil, err
	}

	return toVariantResp(v), nil
}

func (s *Service) DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return gerr
	}
	v, gerr := s.m.GetVariantScoped(ctx, req.ID, projectID)
	if gerr != nil {
		return mapNotFound(gerr)
	}

	guardScope := projectID
	if reason, detail, rerr := s.variantDeleteBlockReason(ctx, guardScope, v); rerr != nil {
		return rerr
	} else if reason != "" {

		if reason == productenums.VariantSkipHasStock {
			return productenums.Err(productenums.ErrVariantHasStock)
		}
		if detail != "" {
			return fmt.Errorf("%w: %s", errors.New(reason), detail)
		}
		return errors.New(reason)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		if _, xerr := s.m.GetForUpdateTx(ctx, tx, v.ProductID, projectID); xerr != nil {
			return mapNotFound(xerr)
		}
		locked, xerr := s.m.GetVariantForUpdateTx(ctx, tx, v.ID, v.ProductID)
		if xerr != nil {
			return mapNotFound(xerr)
		}
		v = locked
		before := variantChangeSnapshot(v, nil)
		if derr := s.m.DeleteVariantTx(ctx, tx, req.ID); derr != nil {
			return derr
		}

		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, v.ProductID); xerr != nil {
			return xerr
		}
		if err := s.recordChangesTx(ctx, tx, variantChangeInput(projectID, v, masterdataenums.ActionDelete,
			masterdataenums.OriginVariant, req.OperatorID, before, nil)); err != nil {
			return err
		}
		_, err := s.recalcAutoTagsTx(ctx, tx, projectID)
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) newVariantFromDefaults(ctx context.Context, p *productmodel.ProductEntity, req *productdto.CreateVariantReq, warehouseCode string, taken map[string]bool) (v *productmodel.VariantEntity, err error) {
	now := time.Now().UTC()
	v = &productmodel.VariantEntity{
		ID: uuid.NewString(), ProductID: p.ID,
		Price:        defaultFloat(p.DefaultPrice, 0),
		ComparePrice: p.DefaultComparePrice,
		CostPrice:    p.DefaultCostPrice,
		Image:        p.DefaultImage,
		OptionValues: json.RawMessage("{}"),
		Enabled:      true,
		Metadata:     json.RawMessage("{}"),

		WarehouseSKUs: json.RawMessage("[]"),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req != nil {
		if req.Price != nil {
			v.Price = *req.Price
		}
		if req.ComparePrice != nil {
			v.ComparePrice = req.ComparePrice
		}
		if req.CostPrice != nil {
			v.CostPrice = req.CostPrice
		}
		if req.Image != "" {
			v.Image = mediaURL(req.Image)
		}
		if len(req.OptionValues) > 0 {
			v.OptionValues = req.OptionValues
		}
		if req.Enabled != nil {
			v.Enabled = *req.Enabled
		}
		v.Sort = req.Sort
		v.Barcode = req.Barcode
	}

	if custom := strings.TrimSpace(skuOrEmpty(req)); custom != "" {
		v.SKUCode = attachWarehousePrefix(custom, warehouseCode)
		return v, nil
	}

	base := strings.TrimSpace(p.SKUCode)
	if base == "" {

		if base, err = buildProductContainerSKU(containerSKUInput{
			Slug: p.Slug, WarehouseCode: warehouseCode,
		}); err != nil {
			return nil, err
		}
	}

	if v.SKUCode, err = s.uniqueSKUCode(ctx, p.ID, base, taken); err != nil {
		return nil, err
	}
	return v, nil
}

func skuOrEmpty(req *productdto.CreateVariantReq) string {
	if req == nil {
		return ""
	}
	return req.SKUCode
}

func (s *Service) uniqueSKUCode(ctx context.Context, productID, base string, taken map[string]bool) (string, error) {
	for i := 0; i < maxSKUProbe; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%s%03d", base, skuSeparator, i+1)
		}
		if taken[candidate] {
			continue
		}
		exists, qerr := s.m.SKUExists(ctx, productID, candidate, "")
		if qerr != nil {
			return "", qerr
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", productenums.Err(productenums.ErrSkuTaken)
}

type containerSKUInput struct {
	Slug string

	Custom string

	WarehouseCode string
}

func buildProductContainerSKU(in containerSKUInput) (string, error) {
	return buildContainerSKU(in.Slug, in.Custom, in.WarehouseCode)
}

func buildContainerSKU(slug, custom, warehouseCode string) (string, error) {
	code := strings.TrimSpace(custom)
	if code == "" {
		code = productCodeSegment(slug)
		if code == "" {
			return "", productenums.Err(productenums.ErrSkuContainerMissing)
		}
	}
	return attachWarehousePrefix(code, warehouseCode), nil
}

func attachWarehousePrefix(code, warehouseCode string) string {
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return code
	}
	if strings.HasPrefix(strings.ToUpper(code), prefix+skuSeparator) {
		return code
	}
	return prefix + skuSeparator + code
}

func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}

	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

func StripWarehousePrefix(code, warehouseCode string) string {
	return stripWarehousePrefix(code, warehouseCode)
}

func productCodeSegment(slug string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(slug) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Service) resolveWarehouseRef(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	if s.invSvc == nil {
		return nil, nil
	}
	return s.invSvc.ResolveWarehouse(ctx, projectID, warehouseID)
}

func (s *Service) ensureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef,
	v *productmodel.VariantEntity, skuCode string, quantity *int) (err error) {
	if s.invSvc == nil || ref == nil {
		return nil
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		_, xerr := s.attachWarehouseStockTxBySKU(ctx, tx, ref, v, skuCode, quantity)
		return xerr
	})
}

func (s *Service) attachWarehouseStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef,
	v *productmodel.VariantEntity, bareCode, externalSKU string, quantity *int) (stockID string, err error) {
	if s.invSvc == nil || ref == nil {
		return "", nil
	}

	stockID, err = s.invSvc.WarehouseStockIDBySKU(ctx, ref.ProjectID, ref.ID, bareCode)
	if err != nil {
		return "", err
	}
	if stockID == "" {
		stockID, err = s.invSvc.EnsureVariantStockWithExternalTx(ctx, tx, ref, v.ProductID, v.ID, bareCode, externalSKU, quantity)
		if err != nil || stockID == "" {
			return stockID, err
		}
	}
	refs := productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs)
	refs = productmodel.UpsertWarehouseSKURef(refs, productmodel.WarehouseSKURef{
		StockID: stockID, WarehouseID: ref.ID, WarehouseCode: ref.Code, SKUCode: bareCode,
	})
	raw := productmodel.EncodeWarehouseSKUs(refs)
	if xerr := s.m.UpdateVariantWarehouseSKUsTx(ctx, tx, v.ID, raw, time.Now().UTC()); xerr != nil {
		return "", xerr
	}

	v.WarehouseSKUs = raw
	return stockID, nil
}

func (s *Service) attachWarehouseStockTxBySKU(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef,
	v *productmodel.VariantEntity, skuCode string, quantity *int) (stockID string, err error) {
	if s.invSvc == nil || ref == nil {
		return "", nil
	}
	return s.attachWarehouseStockTx(ctx, tx, ref, v, stripWarehousePrefix(skuCode, ref.Code), "", quantity)
}

func (s *Service) ensureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef,
	v *productmodel.VariantEntity, skuCode string, quantity *int) (err error) {
	_, err = s.attachWarehouseStockTxBySKU(ctx, tx, ref, v, skuCode, quantity)
	return err
}

func refCode(ref *productcontract.WarehouseRef) string {
	if ref == nil {
		return ""
	}
	return ref.Code
}

func defaultFloat(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

const (
	MaxVariationDimensions = 4

	MaxVariantCombinations = 200
)

type optionPair struct {
	Key   string
	Value string
}

type variationDimension struct {
	group  *productmodel.ProductAttributeEntity
	values []productdto.AttributeValueResp
}

func (s *Service) GenerateVariants(ctx context.Context, req *productdto.GenerateVariantsReq) (res *productdto.GenerateVariantsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	dims, derr := buildVariationDimensions(p, attrs, req.Selections)
	if derr != nil {
		return nil, derr
	}
	total := combinationCount(dims)
	if total > MaxVariantCombinations {
		return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationCountLimit),
			i18n.ErrorDetail(productenums.DetailVariationCountExceed,
				"n", strconv.Itoa(total), "max", strconv.Itoa(MaxVariantCombinations)))
	}

	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	now := time.Now().UTC()
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}

	taken := map[string]bool{}
	skus := map[string]bool{}
	var carrier *productmodel.VariantEntity
	for _, v := range existing {
		if v.SKUCode != "" {
			skus[v.SKUCode] = true
		}
		pairs := decodeOptionPairs(v.OptionValues)
		if len(pairs) == 0 {

			if carrier == nil || v.CreatedAt.Before(carrier.CreatedAt) {
				carrier = v
			}
			continue
		}
		taken[optionKey(pairs)] = true
	}

	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}

	groupOrder := attributeGroupOrder(p, attrs)

	res = &productdto.GenerateVariantsResp{
		ProductID: p.ID, Total: total, Variants: []*productdto.VariantResp{},
	}
	var updated, created []*productmodel.VariantEntity

	var carrierBefore masterdatacontract.FieldSnapshot
	if carrier != nil {
		carrierBefore = variantChangeSnapshot(carrier, nil)
	}
	for i, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if taken[key] {

			res.Skipped++
			continue
		}
		taken[key] = true
		raw := encodeOptionPairs(pairs)
		if carrier != nil {
			carrier.OptionValues = raw
			carrier.UpdatedAt = now
			updated = append(updated, carrier)

			carrier = nil
			res.Adopted++
			continue
		}

		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw, Sort: i,
		}, refCode(ref), skus)
		if verr != nil {
			return nil, verr
		}

		v.SKUCode = uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), skus)
		skus[v.SKUCode] = true
		created = append(created, v)
	}

	changeInputs := make([]*masterdatacontract.ChangeInput, 0, len(created)+len(updated))
	for _, v := range updated {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, carrierBefore, variantChangeSnapshot(v, nil)))
	}
	for _, v := range created {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}

		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, v, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}
		if err := s.recordChangesTx(ctx, tx, changeInputs...); err != nil {
			return err
		}
		_, err := s.recalcAutoTagsTx(ctx, tx, p.ProjectID)
		return err
	}); err != nil {
		return nil, err
	}
	res.Created = len(created)
	after, rerr := s.m.ListVariants(ctx, p.ID)
	if rerr != nil {
		return nil, rerr
	}
	for _, v := range after {
		res.Variants = append(res.Variants, toVariantResp(v))
	}

	if err := s.fillVariantListStocks(ctx, p.ProjectID, res.Variants); err != nil {
		return nil, err
	}
	return res, nil
}

func buildVariationDimensions(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity, selections []productdto.VariantSelectionReq) (dims []variationDimension, err error) {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	picked := map[string][]string{}
	seenSel := map[string]bool{}
	for _, sel := range selections {
		id := strings.TrimSpace(sel.AttributeID)
		if id == "" {
			continue
		}
		picked[id] = append(picked[id], sel.ValueIDs...)
		if seenSel[id] {
			continue
		}
		seenSel[id] = true
		if a, ok := byID[id]; !ok || !a.IsVariation {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationAttributeInvalid),
				i18n.ErrorDetail(productenums.DetailVariationAttributeUnreferenced, "attribute", id))
		}
	}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		enabled := enabledAttributeValues(a)
		if len(enabled) == 0 {

			continue
		}
		wanted, hit := picked[id]
		if !hit {

			dims = append(dims, variationDimension{group: a, values: enabled})
			continue
		}
		chosen, cerr := pickAttributeValues(a, enabled, wanted)
		if cerr != nil {
			return nil, cerr
		}
		if len(chosen) == 0 {
			continue
		}
		dims = append(dims, variationDimension{group: a, values: chosen})
	}
	if len(dims) == 0 {
		return nil, productenums.Err(productenums.ErrVariationNoDimension)
	}
	if len(dims) > MaxVariationDimensions {
		return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationDimensionLimit),
			i18n.ErrorDetail(productenums.DetailVariationDimensionExceed,
				"n", strconv.Itoa(len(dims)), "max", strconv.Itoa(MaxVariationDimensions)))
	}
	return dims, nil
}

func enabledAttributeValues(a *productmodel.ProductAttributeEntity) (out []productdto.AttributeValueResp) {
	out = []productdto.AttributeValueResp{}
	for _, v := range normalizeValuesFromRaw(a.Values) {
		if v.Enabled {
			out = append(out, v)
		}
	}
	return out
}

func pickAttributeValues(a *productmodel.ProductAttributeEntity, enabled []productdto.AttributeValueResp, wanted []string) (out []productdto.AttributeValueResp, err error) {
	enabledSet := make(map[string]bool, len(enabled))
	for _, v := range enabled {
		enabledSet[v.ID] = true
	}
	want := map[string]bool{}
	for _, id := range wanted {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !enabledSet[id] {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationValueInvalid),
				i18n.ErrorDetail(productenums.DetailVariationValueNotEnabled, "attribute", a.Key, "value", id))
		}
		want[id] = true
	}
	out = []productdto.AttributeValueResp{}
	for _, v := range enabled {
		if want[v.ID] {
			out = append(out, v)
		}
	}
	return out, nil
}

func combinationCount(dims []variationDimension) int {
	total := 1
	for _, d := range dims {
		total *= len(d.values)
	}
	return total
}

func expandCombinations(dims []variationDimension) (out [][]optionPair) {
	if len(dims) == 0 {
		return nil
	}
	idx := make([]int, len(dims))
	for {
		pairs := make([]optionPair, len(dims))
		for i, d := range dims {
			pairs[i] = optionPair{Key: d.group.Key, Value: attributeValueCode(d.values[idx[i]])}
		}
		out = append(out, pairs)
		i := len(dims) - 1
		for i >= 0 {
			idx[i]++
			if idx[i] < len(dims[i].values) {
				break
			}
			idx[i] = 0
			i--
		}
		if i < 0 {
			break
		}
	}
	return out
}

func decodeOptionPairs(raw json.RawMessage) []optionPair {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	out := make([]optionPair, 0, len(m))
	for k, v := range m {
		out = append(out, optionPair{Key: k, Value: v})
	}
	slices.SortFunc(out, func(a, b optionPair) int { return cmp.Compare(a.Key, b.Key) })
	return out
}

func optionKey(pairs []optionPair) string {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	slices.SortFunc(sorted, func(a, b optionPair) int { return cmp.Compare(a.Key, b.Key) })
	var b strings.Builder
	for i, p := range sorted {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	return b.String()
}

func encodeOptionPairs(pairs []optionPair) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.Key)
		v, _ := json.Marshal(p.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

func attributeGroupOrder(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity) map[string]int {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	order := make(map[string]int, len(attrs))
	next := 0
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := order[a.Key]; dup {
			continue
		}
		order[a.Key] = next
		next++
	}

	for _, a := range attrs {
		if _, ok := order[a.Key]; !ok {
			order[a.Key] = next
			next++
		}
	}
	return order
}

func variantSKUCode(container string, optionValues json.RawMessage, groupOrder map[string]int) string {
	pairs := decodeOptionPairs(optionValues)
	if len(pairs) == 0 {
		return container
	}
	pairs = sortedOptionPairs(pairs, groupOrder)
	segments := make([]string, 0, len(pairs))
	for _, p := range pairs {
		segments = append(segments, skuSegment(p.Value))
	}
	return container + skuSeparator + strings.Join(segments, skuSeparator) + VariantSKUSuffix
}

func uniqueVariantSKU(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	body := strings.TrimSuffix(base, VariantSKUSuffix)
	for i := 2; i < maxSKUProbe; i++ {
		candidate := fmt.Sprintf("%s%s%d%s", body, skuSeparator, i, VariantSKUSuffix)
		if !taken[candidate] {
			return candidate
		}
	}
	return base
}

func containerSKUOfVariants(variants []*productmodel.VariantEntity) string {
	var earliest *productmodel.VariantEntity
	for _, v := range variants {
		if strings.TrimSpace(v.SKUCode) == "" {
			continue
		}
		if earliest == nil || v.CreatedAt.Before(earliest.CreatedAt) ||
			(v.CreatedAt.Equal(earliest.CreatedAt) && v.ID < earliest.ID) {
			earliest = v
		}
	}
	if earliest == nil {
		return ""
	}
	return earliest.SKUCode
}

func attributeValueCode(v productdto.AttributeValueResp) string {
	if code := strings.TrimSpace(v.Key); code != "" {
		return code
	}
	return "v" + shortHash(v.ID)
}

func skuSegment(value string) string {
	v := strings.TrimSpace(value)
	if v != "" && isSKUASCII(v) {
		return v
	}
	return "v" + shortHash(v)
}

func isSKUASCII(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

func (s *Service) PreviewVariantCombinations(ctx context.Context, req *productdto.PreviewVariantReq) (res *productdto.PreviewVariantResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	dims, derr := buildVariationDimensions(p, attrs, req.Selections)
	if derr != nil {
		return nil, derr
	}
	total := combinationCount(dims)
	if total > MaxVariantCombinations {
		return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationCountLimit),
			i18n.ErrorDetail(productenums.DetailVariationCountExceed,
				"n", strconv.Itoa(total), "max", strconv.Itoa(MaxVariantCombinations)))
	}
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)

	taken := make(map[string]bool, len(existing))
	seen := map[string]bool{}
	for _, v := range existing {
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			taken[code] = true
		}
		seen[optionKey(decodeOptionPairs(v.OptionValues))] = true
	}
	for _, raw := range req.ExistingOptionValues {
		if len(raw) == 0 {
			continue
		}
		seen[optionKey(decodeOptionPairs(raw))] = true
	}
	res = &productdto.PreviewVariantResp{ProductID: p.ID, Total: total, Rows: []*productdto.VariantPreviewResp{}}
	for _, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if seen[key] {

			res.Skipped++
			continue
		}
		seen[key] = true
		raw := encodeOptionPairs(pairs)
		code := uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), taken)
		taken[code] = true
		res.Rows = append(res.Rows, &productdto.VariantPreviewResp{
			OptionValues: raw, OptionKey: key, SKUCode: code,
		})
	}
	return res, nil
}

func (s *Service) SaveVariantList(ctx context.Context, req *productdto.SaveVariantListReq) (res *productdto.SaveVariantListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
			return err
		}
		var saveErr error
		res, saveErr = s.saveVariantListTx(ctx, tx, projectID, req)
		if saveErr != nil {
			return saveErr
		}
		_, saveErr = s.recalcAutoTagsTx(ctx, tx, projectID)
		return saveErr
	})
	if err != nil {
		return nil, err
	}
	if err := s.fillVariantListStocks(ctx, projectID, res.Variants); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) saveVariantListTx(ctx context.Context, tx *gorm.DB, projectID string, req *productdto.SaveVariantListReq) (res *productdto.SaveVariantListResp, err error) {
	p, gerr := s.m.GetForUpdateTx(ctx, tx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDsForShareTx(ctx, tx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	existing, lerr := s.m.ListVariantsForUpdateTx(ctx, tx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)
	allowed := variationValueIndex(attrs)

	byID := make(map[string]*productmodel.VariantEntity, len(existing))

	owner := make(map[string]string, len(existing))
	for _, v := range existing {
		byID[v.ID] = v
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			owner[code] = v.ID
		}
	}
	now := time.Now().UTC()
	res = &productdto.SaveVariantListResp{ProductID: p.ID}
	kept := make(map[string]bool, len(req.Rows))
	seenKeys := make(map[string]bool, len(req.Rows))
	var updated, created []*productmodel.VariantEntity
	changes := make([]*masterdatacontract.ChangeInput, 0, len(req.Rows))

	stockRows := make(map[string][]productdto.VariantStockRowResp, len(req.Rows))
	for i, row := range req.Rows {
		if vid := strings.TrimSpace(row.VariantID); vid != "" {
			v := byID[vid]
			if v == nil {

				return nil, variantListRejected(vid, row.SKUCode, productenums.VariantSkipVariantMissing)
			}
			key := optionKey(decodeOptionPairs(v.OptionValues))
			if seenKeys[key] {
				return nil, variantListRejected(v.ID, v.SKUCode, productenums.ErrVariantListDuplicated)
			}
			seenKeys[key] = true
			kept[v.ID] = true

			stockRows[v.ID] = row.Stocks
			code := normalizeVariantSKU(row.SKUCode)
			if code == "" {
				return nil, productenums.Err(productenums.ErrVariantSKUEmpty)
			}
			if code == v.SKUCode {

				continue
			}
			if id, taken := owner[code]; taken && id != v.ID {
				return nil, variantListRejected(v.ID, code, productenums.ErrSkuTaken)
			}
			if old := strings.TrimSpace(v.SKUCode); old != "" {
				delete(owner, old)
			}
			owner[code] = v.ID
			before := variantChangeSnapshot(v, nil)
			v.SKUCode = code
			v.UpdatedAt = now
			updated = append(updated, v)
			changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
				masterdataenums.OriginVariantGenerate, req.OperatorID, before, variantChangeSnapshot(v, nil)))
			res.Updated++
			continue
		}
		pairs, perr := normalizeListOptionPairs(row.OptionValues, allowed, groupOrder)
		if perr != nil {
			return nil, perr
		}
		raw := encodeOptionPairs(pairs)
		key := optionKey(pairs)
		if seenKeys[key] {
			return nil, variantListRejected(fmt.Sprintf("row:%d", i+1), row.SKUCode, productenums.ErrVariantListDuplicated)
		}
		seenKeys[key] = true
		code := normalizeVariantSKU(row.SKUCode)
		if code == "" {

			code = variantSKUCode(container, raw, groupOrder)
		}
		if strings.TrimSpace(code) == "" {
			return nil, productenums.Err(productenums.ErrVariantSKUEmpty)
		}
		if _, taken := owner[code]; taken {
			return nil, variantListRejected(fmt.Sprintf("row:%d", i+1), code, productenums.ErrSkuTaken)
		}
		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw,
		}, refCode(ref), nil)
		if verr != nil {
			return nil, verr
		}
		v.SKUCode = code
		v.Sort = i
		owner[code] = v.ID
		if len(row.Stocks) > 0 {
			stockRows[v.ID] = row.Stocks
		}
		created = append(created, v)
		changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
		res.Created++
	}

	pruneStocks := make(map[string]bool, len(kept))
	for id := range kept {
		pruneStocks[id] = true
	}
	stockPlan, sperr := planVariantStockApply(stockRows, pruneStocks)
	if sperr != nil {
		return nil, sperr
	}

	type pendingDelete struct {
		v      *productmodel.VariantEntity
		before masterdatacontract.FieldSnapshot
	}
	var deletions []pendingDelete
	removedIDs, stockIDs := []string{}, []string{}
	for _, v := range existing {
		if !kept[v.ID] {
			removedIDs = append(removedIDs, v.ID)
			stockIDs = append(stockIDs, stockIDsOfRefs(productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs))...)
		}
	}
	facts, err := s.variantDeletionFactsTx(ctx, tx, p.ProjectID, removedIDs, stockIDs)
	if err != nil {
		return nil, err
	}
	stocked := map[string]bool{}
	for _, fact := range facts.Stocks {
		if fact.Quantity != 0 {
			stocked[fact.StockID] = true
		}
	}
	moved := map[string]bool{}
	for _, id := range facts.MovementVariantIDs {
		moved[id] = true
	}
	blocked := []facing.BlockedItem{}
	for _, v := range existing {
		if kept[v.ID] {
			continue
		}
		reason := ""
		for _, ref := range productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs) {
			if stocked[ref.StockID] {
				reason = productenums.VariantSkipHasStock
				break
			}
		}
		if reason == "" && moved[v.ID] {
			reason = productenums.VariantSkipHasMovement
		}
		if reason != "" {
			blocked = append(blocked, facing.BlockedItem{ID: v.ID, Name: v.SKUCode, ReasonKey: reason, Fallback: productenums.VariantSkipFallback(reason)})
			continue
		}
		deletions = append(deletions, pendingDelete{v: v, before: variantChangeSnapshot(v, nil)})
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}

	if err = func() error {
		if serr := s.productWriteScopeTx(ctx, tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}
		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, v, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}

		for _, step := range stockPlan {
			if xerr := s.applyVariantStockRowsTx(ctx, tx, p.ProjectID, p.ID, step); xerr != nil {
				return xerr
			}
		}
		for _, item := range deletions {
			if xerr := s.m.DeleteVariantTx(ctx, tx, item.v.ID); xerr != nil {
				return xerr
			}
			changes = append(changes, variantChangeInput(p.ProjectID, item.v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariantGenerate, req.OperatorID, item.before, nil))
			res.Deleted++
		}
		if err := s.enqueueProductInvalidationTx(ctx, tx, p.ProjectID, p.ID); err != nil {
			return err
		}
		return s.recordChangesTx(ctx, tx, changes...)
	}(); err != nil {
		return nil, err
	}
	after, rerr := s.m.ListVariantsForUpdateTx(ctx, tx, p.ID)
	if rerr != nil {
		return nil, rerr
	}
	for _, v := range after {
		res.Variants = append(res.Variants, toVariantResp(v))
	}
	return res, nil
}

func variantListRejected(id, sku, reason string) error {
	fallback := productenums.FallbackFor(reason)
	if fallback == "" {
		fallback = productenums.VariantSkipFallback(reason)
	}
	return bulkRejected([]facing.BlockedItem{{ID: id, Name: sku, ReasonKey: reason, Fallback: fallback}})
}

func (s *Service) variantDeletionFactsTx(ctx context.Context, tx *gorm.DB, projectID string, variants, stocks []string) (*inventorycontract.VariantDeletionFacts, error) {
	if len(variants) == 0 {
		return &inventorycontract.VariantDeletionFacts{}, nil
	}
	if s.inv != nil {
		return s.inv.VariantDeletionFactsTx(ctx, tx, projectID, variants, stocks)
	}
	if s.invSvc != nil {
		return s.invSvc.VariantDeletionFactsTx(ctx, tx, projectID, variants, stocks)
	}
	return &inventorycontract.VariantDeletionFacts{}, nil
}

func stockIDsOfRefs(refs []productmodel.WarehouseSKURef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.StockID != "" {
			out = append(out, r.StockID)
		}
	}
	return out
}

func (s *Service) variantDeleteBlockReason(ctx context.Context, projectID string, v *productmodel.VariantEntity) (reason, detail string, err error) {
	if v == nil {
		return "", "", nil
	}
	if s.inv != nil {

		stockIDs := stockIDsOfRefs(productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs))
		if len(stockIDs) > 0 {
			facts, cerr := s.inv.StockFactsByStockIDs(ctx, projectID, stockIDs)
			if cerr != nil {
				return "", "", cerr
			}
			for _, f := range facts {
				if f != nil && f.Quantity != 0 {
					return productenums.VariantSkipHasStock, "", nil
				}
			}
		}
	}

	if s.invSvc != nil {
		moved, merr := s.invSvc.VariantHasStockMovement(ctx, projectID, v.ID)
		if merr != nil {
			return "", "", merr
		}
		if moved {
			return productenums.VariantSkipHasMovement, "", nil
		}
	}
	return "", "", nil
}

func resolveContainerSKU(p *productmodel.ProductEntity, existing []*productmodel.VariantEntity, warehouseCode string) (string, error) {
	if p == nil {
		return "", productenums.Err(productenums.ErrInvalidParam)
	}
	if container := strings.TrimSpace(p.SKUCode); container != "" {
		return container, nil
	}
	if container := containerSKUOfVariants(existing); container != "" {
		return container, nil
	}

	return buildProductContainerSKU(containerSKUInput{
		Slug: p.Slug, WarehouseCode: warehouseCode,
	})
}

func normalizeVariantSKU(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, skuSeparator)
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
		parts[i] = skuSegment(part)
	}
	return strings.Join(parts, skuSeparator)
}

func normalizeListOptionPairs(raw json.RawMessage, allowed map[string]map[string]bool, groupOrder map[string]int) ([]optionPair, error) {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return nil, productenums.Err(productenums.ErrVariantOptionsInvalid)
	}
	for _, p := range pairs {
		values, ok := allowed[p.Key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationAttributeInvalid),
				i18n.ErrorDetail(productenums.DetailVariationAttributeNotInProduct, "attribute", p.Key))
		}
		if !values[p.Value] {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrVariationValueInvalid),
				i18n.ErrorDetail(productenums.DetailVariationValueNotEnabled, "attribute", p.Key, "value", p.Value))
		}
	}
	return sortedOptionPairs(pairs, groupOrder), nil
}

func variationValueIndex(attrs []*productmodel.ProductAttributeEntity) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(attrs))
	for _, a := range attrs {
		if a == nil || !a.IsVariation {
			continue
		}
		set := map[string]bool{}
		for _, v := range enabledAttributeValues(a) {
			set[attributeValueCode(v)] = true
		}
		if len(set) > 0 {
			out[a.Key] = set
		}
	}
	return out
}

func sortedOptionPairs(pairs []optionPair, groupOrder map[string]int) []optionPair {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	fallback := len(groupOrder) + 1
	slices.SortStableFunc(sorted, func(a, b optionPair) int {
		ra, aok := groupOrder[a.Key]
		if !aok {
			ra = fallback
		}
		rb, bok := groupOrder[b.Key]
		if !bok {
			rb = fallback
		}
		return cmp.Or(cmp.Compare(ra, rb), cmp.Compare(a.Key, b.Key))
	})
	return sorted
}

var _ productcontract.VariantSnapshotPort = (*Service)(nil)

func (s *Service) VariantSnapshots(ctx context.Context, variantIDs []string, projectID string) (list []*productcontract.VariantSnapshot, err error) {
	ids := dedupeNonEmpty(variantIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, nil
	}

	productIDs := make([]string, 0, len(variants))
	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		if !seen[v.ProductID] {
			seen[v.ProductID] = true
			productIDs = append(productIDs, v.ProductID)
		}
	}

	products, err := s.m.ListNameFactsByIDs(ctx, productIDs, projectID)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[string]string, len(products))
	projectOf := make(map[string]string, len(products))
	for _, p := range products {
		nameOf[p.ID] = p.Name
		projectOf[p.ID] = p.ProjectID
	}

	refs := make([]inventorycontract.VariantWarehouseCostRef, 0, len(variants))
	for _, v := range variants {
		refs = append(refs, inventorycontract.VariantWarehouseCostRef{VariantID: v.ID})
	}
	costs := s.variantWarehouseCosts(ctx, projectID, refs)

	list = make([]*productcontract.VariantSnapshot, 0, len(variants))
	for _, v := range variants {

		ownerProject := projectOf[v.ProductID]
		if ownerProject == "" {
			continue
		}
		list = append(list, &productcontract.VariantSnapshot{
			VariantID:    v.ID,
			ProductID:    v.ProductID,
			ProjectID:    ownerProject,
			ProductName:  nameOf[v.ProductID],
			VariantLabel: variantOptionLabel(v.OptionValues),
			SKU:          v.SKUCode,
			Price:        yuanToCents(v.Price),
			CostPrice:    costOf(costs, v.ID),
			Enabled:      v.Enabled,
		})
	}
	return list, nil
}

func variantOptionLabel(raw json.RawMessage) string {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return ""
	}
	vals := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if strings.TrimSpace(p.Value) != "" {
			vals = append(vals, p.Value)
		}
	}
	return strings.Join(vals, " / ")
}

func yuanToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

func costOf(costs map[string]int64, variantID string) *int64 {
	cents, ok := costs[variantID]
	if !ok {
		return nil
	}
	value := cents
	return &value
}

func (s *Service) variantWarehouseCosts(ctx context.Context, projectID string, refs []inventorycontract.VariantWarehouseCostRef) map[string]int64 {
	out := make(map[string]int64, len(refs))
	if len(refs) == 0 {
		return out
	}
	if s.invSvc == nil {

		return out
	}
	costs, err := s.invSvc.ResolveVariantWarehouseCosts(ctx, projectID, refs)
	if err != nil {
		return out
	}
	for _, c := range costs {
		if c.CostPrice == nil {
			continue
		}
		out[c.VariantID] = yuanToCents(*c.CostPrice)
	}
	return out
}

func dedupeNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

var _ productcontract.VariantAvailabilityLookupPort = (*Service)(nil)

const VariantAvailabilityLookupMaxIDs = 100

func (s *Service) VariantAvailabilities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = map[string]int{}
	ids := normalizeVariantIDs(variantIDs)
	if len(ids) == 0 {
		return out, nil
	}
	if s.availability == nil {
		return out, nil
	}

	pid := strings.TrimSpace(projectID)
	if pid == "" || !isUUID(pid) {
		return out, nil
	}

	avail, aerr := s.availability.AvailableQuantities(ctx, pid, ids)
	if aerr != nil {
		return nil, aerr
	}
	for id, n := range avail {
		if n < 0 {
			n = 0
		}
		out[id] = n
	}
	return out, nil
}

func normalizeVariantIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !isUUID(id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= VariantAvailabilityLookupMaxIDs {
			break
		}
	}
	return out
}

func (s *Service) VariantSnapshotsInWarehouse(ctx context.Context, refs []productcontract.VariantWarehouseRef,
	projectID string) (list []*productcontract.VariantSnapshot, err error) {
	variantIDs := make([]string, 0, len(refs))
	warehouseOf := make(map[string]string, len(refs))
	for _, r := range refs {
		v := strings.TrimSpace(r.VariantID)
		if v == "" {
			continue
		}
		variantIDs = append(variantIDs, v)

		warehouseOf[v] = strings.TrimSpace(r.WarehouseID)
	}
	ids := dedupeNonEmpty(variantIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, nil
	}
	productIDs := make([]string, 0, len(variants))
	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		if !seen[v.ProductID] {
			seen[v.ProductID] = true
			productIDs = append(productIDs, v.ProductID)
		}
	}
	products, err := s.m.ListNameFactsByIDs(ctx, productIDs, projectID)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[string]string, len(products))
	projectOf := make(map[string]string, len(products))
	for _, p := range products {
		nameOf[p.ID] = p.Name
		projectOf[p.ID] = p.ProjectID
	}

	costRefs := make([]inventorycontract.VariantWarehouseCostRef, 0, len(variants))
	for _, v := range variants {
		costRefs = append(costRefs, inventorycontract.VariantWarehouseCostRef{
			VariantID: v.ID, WarehouseID: warehouseOf[v.ID],
		})
	}
	costByKey := make(map[string]int64, len(costRefs))
	if s.invSvc != nil {
		if costs, cerr := s.invSvc.ResolveVariantWarehouseCosts(ctx, projectID, costRefs); cerr == nil {
			for _, c := range costs {
				if c.CostPrice == nil {
					continue
				}

				costByKey[c.VariantID+"|"+c.WarehouseID] = yuanToCents(*c.CostPrice)
			}
		}

	}
	list = make([]*productcontract.VariantSnapshot, 0, len(variants))
	for _, v := range variants {
		ownerProject := projectOf[v.ProductID]
		if ownerProject == "" {
			continue
		}
		costKey := v.ID + "|" + warehouseOf[v.ID]
		list = append(list, &productcontract.VariantSnapshot{
			VariantID:    v.ID,
			ProductID:    v.ProductID,
			ProjectID:    ownerProject,
			ProductName:  nameOf[v.ProductID],
			VariantLabel: variantOptionLabel(v.OptionValues),
			SKU:          v.SKUCode,
			Price:        yuanToCents(v.Price),
			CostPrice:    costOf(costByKey, costKey),
			Enabled:      v.Enabled,
		})
	}
	return list, nil
}

var _ productcontract.VariantSnapshotByWarehousePort = (*Service)(nil)
