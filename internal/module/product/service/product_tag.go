package productservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
)

const (
	defaultTagProducts = 100

	maxTagProducts = 500

	tagProductsPageSize = 50

	maxTagProductsPageSize = 200
)

func (s *Service) CreateTag(ctx context.Context, req *productdto.CreateTagReq) (res *productdto.TagResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, productenums.Err(productenums.ErrTagNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	kind, ruleType, ruleParams, err := normalizeTagShapeOnCreate(req.Kind, req.RuleType, req.RuleParams)
	if err != nil {
		return nil, err
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = deriveSlug(req.Name)
	}
	if slug == "" {
		slug = "t-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	if taken, serr := s.m.TagSlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, productenums.Err(productenums.ErrTagSlugTaken)
	}
	now := time.Now().UTC()
	e := &productmodel.ProductTagEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Slug: slug,
		Kind: kind, RuleType: ruleType, RuleParams: ruleParams,
		Sort: req.Sort, Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.productWriteScopeTx(ctx, tx, projectID); serr != nil {
			return serr
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		var hitIDs []string
		if e.Kind == productenums.TagKindRule {
			var err error
			hitIDs, err = scoped.ruleHitProductIDs(ctx, projectID, e.RuleType, e.RuleParams)
			if err != nil {
				return err
			}
		}
		if cerr := s.m.CreateTagTx(ctx, tx, e); cerr != nil {
			return cerr
		}

		if xerr := s.enqueueInvalidationTx(ctx, tx, projectID,
			invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: e.ID}); xerr != nil {
			return xerr
		}
		if e.Kind != productenums.TagKindRule {
			return nil
		}
		return s.recalcTagTx(ctx, tx, e, hitIDs, time.Now().UTC())
	}); err != nil {
		return nil, err
	}
	return s.tagDetail(ctx, e)
}

func (s *Service) UpdateTag(ctx context.Context, req *productdto.UpdateTagReq) (res *productdto.TagResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetTag(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapTagNotFound(err)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
			return err
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		var err error
		e, err = scoped.m.GetTag(ctx, req.ID, projectID)
		if err != nil {
			return mapTagNotFound(err)
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return productenums.Err(productenums.ErrTagNameRequired)
			}
			e.Name = name
		}
		if req.Slug != nil {
			slug := normalizeSlug(*req.Slug)
			if slug == "" {
				return productenums.Err(productenums.ErrInvalidParam)
			}
			if taken, serr := scoped.m.TagSlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
				return serr
			} else if taken {
				return productenums.Err(productenums.ErrTagSlugTaken)
			}
			e.Slug = slug
		}
		if req.Sort != nil {
			e.Sort = *req.Sort
		}
		if req.Kind != nil || req.RuleType != nil || req.RuleParams != nil {
			if err = applyTagShapeUpdate(e, req); err != nil {
				return err
			}
		}
		e.UpdatedAt = time.Now().UTC()
		var hitIDs []string
		if e.Kind == productenums.TagKindRule {
			hitIDs, err = scoped.ruleHitProductIDs(ctx, projectID, e.RuleType, e.RuleParams)
			if err != nil {
				return err
			}
		}
		if uerr := s.m.UpdateTagTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		refIDs, rerr := s.m.ProductIDsByTagTx(ctx, tx, e.ProjectID, e.ID, maxRenameFanoutProducts+1)
		if rerr != nil {
			return rerr
		}
		if xerr := s.enqueueEntityRenameFanout(ctx, tx, e.ProjectID, productcontract.EntityTypeTag, e.ID, refIDs); xerr != nil {
			return xerr
		}

		if e.Kind != productenums.TagKindRule {
			return nil
		}
		return s.recalcTagTx(ctx, tx, e, hitIDs, time.Now().UTC())
	}); err != nil {
		return nil, err
	}
	return s.tagDetail(ctx, e)
}

func applyTagShapeUpdate(e *productmodel.ProductTagEntity, req *productdto.UpdateTagReq) (err error) {
	kind := e.Kind
	if req.Kind != nil {
		kind = strings.TrimSpace(*req.Kind)
	}
	switch kind {
	case productenums.TagKindManual:

		e.Kind, e.RuleType, e.RuleParams, e.RecalcAt = productenums.TagKindManual, "", json.RawMessage("{}"), nil
		return nil
	case productenums.TagKindRule:
		ruleType := e.RuleType
		if req.RuleType != nil {
			ruleType = strings.TrimSpace(*req.RuleType)
		}
		params := e.RuleParams
		if req.RuleParams != nil {
			params = req.RuleParams
		}
		norm, nerr := normalizeTagRuleParams(ruleType, params)
		if nerr != nil {
			return nerr
		}
		e.Kind, e.RuleType, e.RuleParams = productenums.TagKindRule, ruleType, norm
		return nil
	default:
		return productenums.Err(productenums.ErrTagKindInvalid)
	}
}

func normalizeTagShapeOnCreate(kind, ruleType string, params json.RawMessage) (outKind, outRuleType string, outParams json.RawMessage, err error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {

		kind = productenums.TagKindManual
	}
	switch kind {
	case productenums.TagKindManual:
		if strings.TrimSpace(ruleType) != "" || hasRuleParams(params) {
			return "", "", nil, productenums.Err(productenums.ErrTagRuleNotAllowed)
		}
		return kind, "", json.RawMessage("{}"), nil
	case productenums.TagKindRule:
		norm, nerr := normalizeTagRuleParams(ruleType, params)
		if nerr != nil {
			return "", "", nil, nerr
		}
		return kind, strings.TrimSpace(ruleType), norm, nil
	default:
		return "", "", nil, productenums.Err(productenums.ErrTagKindInvalid)
	}
}

func hasRuleParams(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "{}" && s != "null"
}

func (s *Service) GetTag(ctx context.Context, req *productdto.GetTagReq) (res *productdto.TagResp, err error) {
	if req == nil || req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetTag(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapTagNotFound(err)
	}
	return s.tagDetail(ctx, e)
}

func (s *Service) ListTags(ctx context.Context, req *productdto.ListTagReq) (list []*productdto.TagResp, err error) {
	rawProjectID, kind, keyword := tagListFilter(req)

	projectID, err := s.resolveProjectID(ctx, rawProjectID)
	if err != nil {
		return nil, err
	}

	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	limit, offset := optionalPaging(inPage, inSize)
	rows, err := s.m.ListTags(ctx, projectID, kind, keyword, limit, offset)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	counts, cerr := s.m.CountProductsByTagIDs(ctx, projectID, ids)
	if cerr != nil {
		return nil, cerr
	}
	list = make([]*productdto.TagResp, 0, len(rows))
	for _, r := range rows {
		resp := toTagResp(r)

		resp.ProductCount = int(counts[r.ID])
		list = append(list, resp)
	}
	return list, nil
}

func (s *Service) CountTags(ctx context.Context, req *productdto.ListTagReq) (n int64, err error) {
	rawProjectID, kind, keyword := tagListFilter(req)
	projectID, err := s.resolveProjectID(ctx, rawProjectID)
	if err != nil {
		return 0, err
	}
	return s.m.CountTags(ctx, projectID, kind, keyword)
}

func tagListFilter(req *productdto.ListTagReq) (rawProjectID, kind, keyword string) {
	if req == nil {
		return "", "", ""
	}
	return req.ProjectID, strings.TrimSpace(req.Kind), strings.TrimSpace(req.Keyword)
}

func (s *Service) ListTagProducts(ctx context.Context, req *productdto.ListTagProductsReq) (res *productdto.TagProductsListResp, err error) {
	if req == nil || req.TagID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tag, gerr := s.m.GetTag(ctx, req.TagID, projectID)
	if gerr != nil {
		return nil, mapTagNotFound(gerr)
	}
	size := req.Size
	if size <= 0 {
		size = tagProductsPageSize
	}
	if size > maxTagProductsPageSize {
		size = maxTagProductsPageSize
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	total, cerr := s.m.CountProductsByTag(ctx, tag.ID, projectID)
	if cerr != nil {
		return nil, cerr
	}

	page = pagination.ClampPage(page, size, total)
	rows, lerr := s.m.ListProductsByTagPage(ctx, tag.ID, projectID, (page-1)*size, size)
	if lerr != nil {
		return nil, lerr
	}
	return &productdto.TagProductsListResp{
		TagID: tag.ID, TagName: tag.Name,
		Total: int(total), Page: page, PageSize: size,
		Items: toTagProductResps(rows),
	}, nil
}

func (s *Service) DeleteTag(ctx context.Context, req *productdto.DeleteTagReq) (err error) {
	if req == nil || req.ID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	tag, gerr := s.m.GetTag(ctx, req.ID, projectID)
	if gerr != nil {
		return mapTagNotFound(gerr)
	}
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByTag(ctx, req.ID, ids)
	})
	if rerr != nil {
		return rerr
	}
	if others := ref.Outside(tag.ProjectID); others.Referenced() {
		return crossProjectRefBlocked(productenums.ErrTagCrossProject, others)
	}
	now := time.Now().UTC()
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {

		if serr := s.productWriteScopeTx(ctx, tx, tag.ProjectID); serr != nil {
			return serr
		}
		if rerr := s.m.RemoveTagFromProductsTx(tx, tag.ID, tag.ProjectID, now); rerr != nil {
			return rerr
		}
		if xerr := s.enqueueInvalidationTx(ctx, tx, tag.ProjectID,
			invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: tag.ID}); xerr != nil {
			return xerr
		}
		return s.m.DeleteTagTx(tx, tag.ID)
	})
}

func (s *Service) ListTagRuleTypes(ctx context.Context) (list []*productdto.TagRuleTypeResp) {
	return tagRuleTypeOptions()
}

func (s *Service) RecalcTags(ctx context.Context, req *productdto.RecalcTagsReq) (res *productdto.RecalcTagsResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	res = &productdto.RecalcTagsResp{Tags: []*productdto.TagResp{}}
	projectID := strings.TrimSpace(req.ProjectID)
	var tags []*productmodel.ProductTagEntity
	if id := strings.TrimSpace(req.TagID); id != "" {

		scopeID, serr := s.resolveProjectID(ctx, projectID)
		if serr != nil {
			return nil, serr
		}
		tag, gerr := s.m.GetTag(ctx, id, scopeID)
		if gerr != nil {
			return nil, mapTagNotFound(gerr)
		}
		tags = []*productmodel.ProductTagEntity{tag}
		if projectID == "" {
			projectID = tag.ProjectID
		}
	} else {
		if projectID == "" {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
		tags, err = s.m.ListTags(ctx, projectID, productenums.TagKindRule, "", 0, 0)
		if err != nil {
			return nil, err
		}
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.productWriteScopeTx(ctx, tx, projectID); err != nil {
			return err
		}
		scoped := *s
		scoped.m = productmodel.NewModel(tx)
		if req.TagID != "" {
			current, err := scoped.m.GetTag(ctx, strings.TrimSpace(req.TagID), projectID)
			if err != nil {
				return mapTagNotFound(err)
			}
			tags = []*productmodel.ProductTagEntity{current}
		} else {
			var err error
			tags, err = scoped.m.ListTags(ctx, projectID, productenums.TagKindRule, "", 0, 0)
			if err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		for _, tag := range tags {
			if tag.Kind != productenums.TagKindRule {
				continue
			}
			ids, err := scoped.ruleHitProductIDs(ctx, projectID, tag.RuleType, tag.RuleParams)
			if err != nil {
				return err
			}
			if err := scoped.recalcTagTx(ctx, tx, tag, ids, now); err != nil {
				return err
			}
			res.Recalculated++
			res.Products += len(ids)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	after, lerr := s.ListTags(ctx, &productdto.ListTagReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	res.Tags = after
	return res, nil
}

func (s *Service) resolveTagIDs(ctx context.Context, projectID string, in []string) (out []string, err error) {
	out = []string{}
	if len(in) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	dedup := make([]string, 0, len(in))
	for _, id := range in {
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
	rows, lerr := s.m.ListTagsByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductTagEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range dedup {
		row, ok := byID[id]
		if !ok {
			return nil, productenums.Err(productenums.ErrTagNotFound)
		}
		if projectID != "" && row.ProjectID != projectID {
			return nil, productenums.Err(productenums.ErrTagProjectMismatch)
		}
		if row.Kind != productenums.TagKindManual {
			return nil, productenums.Err(productenums.ErrTagNotManual)
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) recalcTagTx(ctx context.Context, tx *gorm.DB, tag *productmodel.ProductTagEntity,
	ids []string, now time.Time) (err error) {
	if serr := s.productWriteScopeTx(ctx, tx, tag.ProjectID); serr != nil {
		return serr
	}

	oldIDs, oerr := s.m.ListProductIDsByTagTx(tx, tag.ID, tag.ProjectID)
	if oerr != nil {
		return oerr
	}
	if rerr := s.m.ReplaceTagProductsTx(tx, tag.ID, tag.ProjectID, ids, now); rerr != nil {
		return rerr
	}

	changed := membershipDiff(oldIDs, ids)
	targets := make([]invalidationTarget, 0, len(changed)+1)
	targets = append(targets, invalidationTarget{EntityType: productcontract.EntityTypeTag, EntityID: tag.ID})
	for _, pid := range changed {
		targets = append(targets, productInvalidationTarget(pid))
	}
	if xerr := s.enqueueInvalidationTx(ctx, tx, tag.ProjectID, targets...); xerr != nil {
		return xerr
	}

	tag.RecalcAt = &now
	tag.UpdatedAt = now
	return s.m.UpdateTagTx(ctx, tx, tag)
}

func (s *Service) recalcAutoTagsTx(ctx context.Context, tx *gorm.DB, projectID string) (int, error) {
	scoped := *s
	scoped.m = productmodel.NewModel(tx)
	tags, err := scoped.m.ListTags(ctx, projectID, productenums.TagKindRule, "", 0, 0)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	for _, tag := range tags {
		ids, err := scoped.ruleHitProductIDs(ctx, projectID, tag.RuleType, tag.RuleParams)
		if err != nil {
			return 0, err
		}
		if err := scoped.recalcTagTx(ctx, tx, tag, ids, now); err != nil {
			return 0, err
		}
	}
	return len(tags), nil
}

func (s *Service) ruleHitProductIDs(ctx context.Context, projectID, ruleType string, params json.RawMessage) (ids []string, err error) {
	rule := lookupTagRule(ruleType)
	if rule == nil {
		return nil, ruleTypeErr(ruleType)
	}
	ids, err = rule.Evaluate(ctx, s.m, projectID, params)
	if err != nil {
		return nil, err
	}
	return s.filterProjectProductIDs(ctx, projectID, ids)
}

func (s *Service) filterProjectProductIDs(ctx context.Context, projectID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, lerr := s.m.ListIDFactsByIDs(ctx, ids, projectID)
	if lerr != nil {
		return nil, lerr
	}
	for _, p := range rows {
		if p == nil {
			continue
		}
		if projectID != "" && p.ProjectID != projectID {
			continue
		}
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) tagDetail(ctx context.Context, e *productmodel.ProductTagEntity) (res *productdto.TagResp, err error) {

	rows, lerr := s.m.ListProductsByTag(ctx, e.ID, e.ProjectID, maxTagProducts)
	if lerr != nil {
		return nil, lerr
	}
	total, cerr := s.m.CountProductsByTag(ctx, e.ID, e.ProjectID)
	if cerr != nil {
		return nil, cerr
	}
	resp := toTagResp(e)
	resp.Products = toTagProductResps(rows)
	resp.ProductCount = int(total)
	return resp, nil
}

func toTagResp(e *productmodel.ProductTagEntity) *productdto.TagResp {
	resp := &productdto.TagResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Slug: e.Slug,
		Kind: e.Kind, RuleType: e.RuleType,
		RuleParams: orJSON(e.RuleParams, "{}"),
		RuleValues: tagRuleValues(e.RuleParams),
		Sort:       e.Sort,
		CreatedAt:  utils.NewJSONTime(e.CreatedAt),
		UpdatedAt:  utils.NewJSONTime(e.UpdatedAt),
	}
	if e.RecalcAt != nil {
		if e.RecalcAt != nil {
			resp.RecalcAt = utils.NewJSONTime(*e.RecalcAt)
		}
	}
	return resp
}

func toTagProductResps(rows []*productmodel.ProductEntity) []*productdto.TagProductResp {
	out := make([]*productdto.TagProductResp, 0, len(rows))
	for _, p := range rows {
		if p == nil {
			continue
		}
		out = append(out, &productdto.TagProductResp{
			ID: p.ID, Name: p.Name, Slug: p.Slug, Status: p.Status,
		})
	}
	return out
}

func mapTagNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return productenums.Err(productenums.ErrTagNotFound)
	}
	return err
}

const (
	tagRuleDaysMin  = 1
	tagRuleDaysMax  = 365
	tagRulePriceMax = 1e9
)

type tagRule struct {
	Type string

	ParamKeys []string

	Normalize func(params json.RawMessage) (json.RawMessage, error)

	Evaluate func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error)
}

var tagRules = []*tagRule{
	{
		Type:      productenums.TagRuleNewArrival,
		ParamKeys: []string{"days"},
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m, "days"); err != nil {
				return nil, err
			}
			raw, ok := m["days"]
			if !ok {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRequired, "field", "days"))
			}
			days, derr := ruleInt(raw, "days")
			if derr != nil {
				return nil, derr
			}
			if days < tagRuleDaysMin || days > tagRuleDaysMax {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRange,
					"field", "days", "min", strconv.Itoa(tagRuleDaysMin),
					"max", strconv.Itoa(tagRuleDaysMax), "value", strconv.Itoa(days)))
			}
			return json.RawMessage(fmt.Sprintf("{%q:%d}", "days", days)), nil
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			p, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			days, derr := ruleInt(p["days"], "days")
			if derr != nil {
				return nil, derr
			}
			since := time.Now().UTC().AddDate(0, 0, -days)
			return m.ListProductIDsPublishedSince(ctx, projectID, productenums.StatusPublished, since)
		},
	},
	{
		Type:      productenums.TagRulePriceRange,
		ParamKeys: []string{"minPrice", "maxPrice"},
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m, "minPrice", "maxPrice"); err != nil {
				return nil, err
			}
			if len(m) == 0 {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagPriceNeedsOne))
			}
			var minV, maxV *float64
			if raw, ok := m["minPrice"]; ok {
				v, verr := ruleFloat(raw, "minPrice")
				if verr != nil {
					return nil, verr
				}
				if v < 0 || v > tagRulePriceMax {
					return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRangePlain,
						"field", "minPrice", "min", "0", "max", formatRulePrice(tagRulePriceMax)))
				}
				minV = &v
			}
			if raw, ok := m["maxPrice"]; ok {
				v, verr := ruleFloat(raw, "maxPrice")
				if verr != nil {
					return nil, verr
				}
				if v < 0 || v > tagRulePriceMax {
					return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRangePlain,
						"field", "maxPrice", "min", "0", "max", formatRulePrice(tagRulePriceMax)))
				}
				maxV = &v
			}
			if minV != nil && maxV != nil && *minV > *maxV {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagPriceOrderWrong))
			}

			parts := make([]string, 0, 2)
			if minV != nil {
				parts = append(parts, fmt.Sprintf("%q:%s", "minPrice", formatRulePrice(*minV)))
			}
			if maxV != nil {
				parts = append(parts, fmt.Sprintf("%q:%s", "maxPrice", formatRulePrice(*maxV)))
			}
			return json.RawMessage("{" + strings.Join(parts, ",") + "}"), nil
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			p, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			var minV, maxV *float64
			if raw, ok := p["minPrice"]; ok {
				v, verr := ruleFloat(raw, "minPrice")
				if verr != nil {
					return nil, verr
				}
				minV = &v
			}
			if raw, ok := p["maxPrice"]; ok {
				v, verr := ruleFloat(raw, "maxPrice")
				if verr != nil {
					return nil, verr
				}
				maxV = &v
			}
			return m.ListProductIDsByVariantPrice(ctx, minV, maxV)
		},
	},
	{
		Type: productenums.TagRuleOnSale,

		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m); err != nil {
				return nil, err
			}
			return json.RawMessage("{}"), nil
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			return m.ListProductIDsWithDiscount(ctx)
		},
	},
}

func lookupTagRule(ruleType string) *tagRule {
	ruleType = strings.TrimSpace(ruleType)
	for _, r := range tagRules {
		if r.Type == ruleType {
			return r
		}
	}
	return nil
}

func tagRuleTypeOptions() (out []*productdto.TagRuleTypeResp) {
	out = make([]*productdto.TagRuleTypeResp, 0, len(tagRules))
	for _, r := range tagRules {
		out = append(out, &productdto.TagRuleTypeResp{Type: r.Type, ParamKeys: r.ParamKeys})
	}
	return out
}

func normalizeTagRuleParams(ruleType string, params json.RawMessage) (out json.RawMessage, err error) {
	r := lookupTagRule(ruleType)
	if r == nil {
		return nil, ruleTypeErr(ruleType)
	}
	norm, nerr := r.Normalize(params)
	if nerr != nil {
		return nil, nerr
	}
	return norm, nil
}

func tagRuleValues(params json.RawMessage) productdto.RuleValuesResp {
	m, err := decodeRuleParams(params)
	if err != nil {
		return productdto.RuleValuesResp{}
	}
	var out productdto.RuleValuesResp
	if raw, ok := m["days"]; ok {
		if days, derr := ruleInt(raw, "days"); derr == nil {
			out.Days, out.HasDays = days, true
		}
	}
	if raw, ok := m["minPrice"]; ok {
		if v, ferr := ruleFloat(raw, "minPrice"); ferr == nil {
			out.MinPrice, out.HasMinPrice = v, true
		}
	}
	if raw, ok := m["maxPrice"]; ok {
		if v, ferr := ruleFloat(raw, "maxPrice"); ferr == nil {
			out.MaxPrice, out.HasMaxPrice = v, true
		}
	}
	return out
}

func decodeRuleParams(raw json.RawMessage) (m map[string]json.RawMessage, err error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]json.RawMessage{}, nil
	}
	if err = json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamsNotObject))
	}
	return m, nil
}

func rejectUnknownRuleKeys(m map[string]json.RawMessage, allowed ...string) (err error) {
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	unknown := make([]string, 0, len(m))
	for k := range m {
		if !allow[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagUnknownKeys,
		"keys", strings.Join(unknown, ", ")))
}

func ruleInt(raw json.RawMessage, key string) (v int, err error) {
	f, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return 0, ferr
	}
	if f != float64(int(f)) {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotInteger, "field", key))
	}
	return int(f), nil
}

func ruleFloat(raw json.RawMessage, key string) (v float64, err error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotNumber, "field", key))
	}
	f, ferr := strconv.ParseFloat(s, 64)
	if ferr != nil {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotNumber, "field", key))
	}
	return f, nil
}

func formatRulePrice(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func ruleParamsErr(detail string) error {
	return fmt.Errorf("%w: %s", productenums.Err(productenums.ErrTagRuleParamsInvalid), detail)
}

func ruleTypeErr(ruleType string) error {
	if strings.TrimSpace(ruleType) == "" {
		return productenums.Err(productenums.ErrTagRuleTypeInvalid)
	}
	return fmt.Errorf("%w: %s", productenums.Err(productenums.ErrTagRuleTypeInvalid),
		i18n.ErrorDetail(productenums.DetailTagUnknownRuleType, "type", ruleType))
}
