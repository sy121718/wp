package productservice

import (
	"cmp"
	"context"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
)

func (s *Service) CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, productenums.Err(productenums.ErrAttrNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	key := normalizeAttrToken(req.Key)
	if key == "" {
		key = normalizeAttrToken(req.Name)
	}
	if key == "" {
		key = "attr-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, kerr := s.m.AttributeKeyExists(ctx, projectID, key, ""); kerr != nil {
		return nil, kerr
	} else if taken {
		return nil, productenums.Err(productenums.ErrAttrKeyTaken)
	}

	now := time.Now().UTC()
	isVariation := true
	if req.IsVariation != nil {
		isVariation = *req.IsVariation
	}
	values := sanitizeAttributeValues(req.Values)
	e := &productmodel.ProductAttributeEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Key: key, Name: strings.TrimSpace(req.Name),
		IsVariation: isVariation, Sort: req.Sort,
		Values:    encodeAttributeValues(values),
		Metadata:  []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateAttributeTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.enqueueAttributeInvalidation(ctx, tx, projectID, e.ID)
	}); err != nil {
		return nil, err
	}
	return toAttributeResp(e), nil
}

func (s *Service) UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetAttribute(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if req.Key != nil {
		key := normalizeAttrToken(*req.Key)
		if key == "" {
			return nil, productenums.Err(productenums.ErrAttrKeyRequired)
		}
		if taken, kerr := s.m.AttributeKeyExists(ctx, e.ProjectID, key, e.ID); kerr != nil {
			return nil, kerr
		} else if taken {
			return nil, productenums.Err(productenums.ErrAttrKeyTaken)
		}
		e.Key = key
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, productenums.Err(productenums.ErrAttrNameRequired)
		}
		e.Name = name
	}
	if req.IsVariation != nil {
		e.IsVariation = *req.IsVariation
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}

		locked, lerr := s.m.GetAttributeForUpdateTx(ctx, tx, req.ID, e.ProjectID)
		if lerr != nil {
			return mapNotFound(lerr)
		}
		preserveUntouchedAttributeFields(e, locked, req)
		e.UpdatedAt = time.Now().UTC()
		if uerr := s.m.UpdateAttributeTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		return s.enqueueAttributeDefinitionChange(ctx, tx, e.ProjectID, e.ID)
	}); err != nil {
		return nil, err
	}
	return s.toAttributeDetail(ctx, e)
}

func (s *Service) SetAttributeValues(ctx context.Context, req *productdto.SetAttributeValuesReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	values := sanitizeAttributeValues(req.Values)
	var e *productmodel.ProductAttributeEntity
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}

		locked, lerr := s.m.GetAttributeForUpdateTx(ctx, tx, req.ID, projectID)
		if lerr != nil {
			return mapNotFound(lerr)
		}
		e = locked
		e.Values = encodeAttributeValues(values)
		e.UpdatedAt = time.Now().UTC()
		if uerr := s.m.UpdateAttributeTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		return s.enqueueAttributeDefinitionChange(ctx, tx, e.ProjectID, e.ID)
	}); err != nil {
		return nil, err
	}
	return s.toAttributeDetail(ctx, e)
}

func (s *Service) GetAttribute(ctx context.Context, req *productdto.GetAttributeReq) (res *productdto.AttributeResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetAttribute(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return s.toAttributeDetail(ctx, e)
}

func (s *Service) ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) (list []*productdto.AttributeResp, err error) {
	projectID, keyword, variation := attributeFilter(req)
	page, size := attributePageArgs(req)
	rows, err := s.m.ListAttributes(ctx, projectID, keyword, variation, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.AttributeResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toAttributeResp(r))
	}
	return list, nil
}

func (s *Service) CountAttributes(ctx context.Context, req *productdto.ListAttributeReq) (n int64, err error) {
	projectID, keyword, variation := attributeFilter(req)
	return s.m.CountAttributes(ctx, projectID, keyword, variation)
}

func attributeFilter(req *productdto.ListAttributeReq) (projectID, keyword string, variation *bool) {
	if req == nil {
		return "", "", nil
	}

	switch strings.TrimSpace(req.Variation) {
	case "1":
		v := true
		variation = &v
	case "0":
		v := false
		variation = &v
	}
	return req.ProjectID, strings.TrimSpace(req.Keyword), variation
}

func (s *Service) DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	if _, gerr := s.m.GetAttribute(ctx, req.ID, projectID); gerr != nil {
		return mapNotFound(gerr)
	}

	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByAttribute(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if ref.Referenced() {
		return crossProjectRefBlocked(productenums.ErrAttrInUse, ref)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteAttributeTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		return s.enqueueAttributeInvalidation(ctx, tx, projectID, req.ID)
	})
}

func attributeInvalidationEntityKey(attributeID string) invalidationTarget {
	return invalidationTarget{EntityType: productcontract.EntityTypeAttribute, EntityID: attributeID}
}

func (s *Service) enqueueAttributeInvalidation(ctx context.Context, tx *gorm.DB, projectID, attributeID string) error {
	return s.enqueueInvalidationTx(ctx, tx, projectID, attributeInvalidationEntityKey(attributeID))
}

func (s *Service) enqueueAttributeDefinitionChange(ctx context.Context, tx *gorm.DB, projectID, attributeID string) error {
	refIDs, err := s.m.ProductIDsByAttributeTx(ctx, tx, projectID, attributeID, maxRenameFanoutProducts+1)
	if err != nil {
		return err
	}
	return s.enqueueEntityRenameFanout(ctx, tx, projectID, productcontract.EntityTypeAttribute, attributeID, refIDs)
}

func attributePageArgs(req *productdto.ListAttributeReq) (page, size int) {
	page, size = 1, defaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > maxPageSize {
			size = maxPageSize
		}
	}
	return page, size
}

func toAttributeResp(e *productmodel.ProductAttributeEntity) *productdto.AttributeResp {
	values := normalizeValuesFromRaw(e.Values)
	resp := &productdto.AttributeResp{
		ID: e.ID, ProjectID: e.ProjectID, Key: e.Key, Name: e.Name,
		IsVariation: e.IsVariation, Sort: e.Sort,
		Values: values, ValueCount: len(values),
		CreatedAt: utils.NewJSONTime(e.CreatedAt),
		UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
	for _, v := range values {
		if v.Enabled {
			resp.VariationValueCount++
		}
	}
	return resp
}

func (s *Service) toAttributeDetail(ctx context.Context, e *productmodel.ProductAttributeEntity) (*productdto.AttributeResp, error) {
	_ = ctx
	return toAttributeResp(e), nil
}

func (s *Service) resolveAttributeIDs(ctx context.Context, projectID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	dedup := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
	}
	rows, lerr := s.m.ListAttributesByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range dedup {
		row, ok := byID[id]
		if !ok {
			return nil, productenums.Err(productenums.ErrAttrNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, productenums.Err(productenums.ErrAttrProjectMismatch)
		}
		out = append(out, id)
	}
	return out, nil
}

const valueSortBase = 10

func sanitizeAttributeValues(reqs []productdto.AttributeValueReq) []productdto.AttributeValueResp {
	out := make([]productdto.AttributeValueResp, 0, len(reqs))
	seenID := map[string]bool{}
	seenKey := map[string]bool{}
	for i, r := range reqs {
		label := strings.TrimSpace(r.Label)
		if label == "" {
			continue
		}
		id := strings.TrimSpace(r.ID)
		if id == "" || seenID[id] {
			id = uuid.NewString()
		}
		seenID[id] = true

		key := normalizeAttrToken(r.Key)
		if key == "" {
			key = normalizeAttrToken(label)
		}

		if key == "" {
			key = "v" + itoa(i+1)
		}
		if seenKey[key] {
			key = key + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:4]
		}
		seenKey[key] = true

		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		sortV := r.Sort
		if sortV == 0 {
			sortV = (i + 1) * valueSortBase
		}
		out = append(out, productdto.AttributeValueResp{
			ID: id, Key: key, Label: label, Sort: sortV, Enabled: enabled,
		})
	}
	slices.SortStableFunc(out, func(a, b productdto.AttributeValueResp) int { return cmp.Compare(a.Sort, b.Sort) })
	return out
}

func normalizeValuesFromRaw(raw json.RawMessage) []productdto.AttributeValueResp {
	if len(raw) == 0 {
		return []productdto.AttributeValueResp{}
	}
	var objs []productdto.AttributeValueResp
	if err := json.Unmarshal(raw, &objs); err == nil {

		reqs := make([]productdto.AttributeValueReq, 0, len(objs))
		for _, o := range objs {
			enabled := o.Enabled
			reqs = append(reqs, productdto.AttributeValueReq{
				ID: o.ID, Key: o.Key, Label: o.Label, Sort: o.Sort, Enabled: &enabled,
			})
		}
		return sanitizeAttributeValues(reqs)
	}
	var labels []string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return []productdto.AttributeValueResp{}
	}
	reqs := make([]productdto.AttributeValueReq, 0, len(labels))
	for _, l := range labels {
		reqs = append(reqs, productdto.AttributeValueReq{Label: l})
	}
	return sanitizeAttributeValues(reqs)
}

func encodeAttributeValues(values []productdto.AttributeValueResp) json.RawMessage {
	if values == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

func normalizeAttrToken(s string) string {
	return deriveSlug(strings.TrimSpace(s))
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func preserveUntouchedAttributeFields(e, locked *productmodel.ProductAttributeEntity, req *productdto.UpdateAttributeReq) {
	if e == nil || locked == nil || req == nil {
		return
	}
	if req.Key == nil {
		e.Key = locked.Key
	}
	if req.Name == nil {
		e.Name = locked.Name
	}
	if req.IsVariation == nil {
		e.IsVariation = locked.IsVariation
	}
	if req.Sort == nil {
		e.Sort = locked.Sort
	}
}
