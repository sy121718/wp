package contentservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contentmodel "go_wp/internal/module/content/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
)

type Service struct {
	m *contentmodel.Model

	invalidator contentcontract.DependencyInvalidator

	contentStore i18n.ContentStore
}

func (s *Service) SetDependencyInvalidator(inv contentcontract.DependencyInvalidator) {
	s.invalidator = inv
}

func (s *Service) notifyContentChanged(ctx context.Context, e *contentmodel.Entity) {
	if s == nil || s.invalidator == nil || e == nil {
		return
	}
	for _, k := range []pipeline.DepKey{
		pipeline.DirectContentKey(e.EntityType, e.ID),
		pipeline.ContentCollectionKey(e.EntityType),
	} {
		s.invalidator.Invalidate(ctx, k.Kind, k.Key)
	}
}

func NewService(m *contentmodel.Model) *Service { return &Service{m: m} }

var _ contentcontract.ContentService = (*Service)(nil)

var _ contentcontract.ContentDataSource = (*Service)(nil)

func (s *Service) Create(ctx context.Context, req *contentdto.CreateReq) (res *contentdto.ContentResp, err error) {
	if req == nil || !contentcontract.IsValidType(req.EntityType) || req.Slug == "" {
		return nil, contentenums.Err(contentenums.ErrInvalidParam)
	}

	if _, gerr := s.m.GetBySlug(ctx, req.EntityType, req.Slug); gerr == nil {
		return nil, contentenums.Err(contentenums.ErrSlugTaken)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}

	data, err := validateData(req.EntityType, req.Data)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	now := time.Now().UTC()
	e := &contentmodel.Entity{
		ID: uuid.NewString(), EntityType: req.EntityType, Slug: req.Slug,
		Revision: 1, Data: raw, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}

	s.notifyContentChanged(ctx, e)
	return toResp(e, data), nil
}

func (s *Service) Update(ctx context.Context, req *contentdto.UpdateReq) (res *contentdto.ContentResp, err error) {
	if req == nil || req.ID == "" {
		return nil, contentenums.Err(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contentenums.Err(contentenums.ErrNotFound)
		}
		return nil, err
	}
	data, err := validateData(e.EntityType, req.Data)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	e.Data = raw
	e.Revision++
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.Save(ctx, e); err != nil {
		return nil, err
	}
	s.notifyContentChanged(ctx, e)
	return toResp(e, data), nil
}

func (s *Service) Get(ctx context.Context, req *contentdto.GetReq) (res *contentdto.ContentResp, err error) {
	if req == nil || req.ID == "" {
		return nil, contentenums.Err(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, contentenums.Err(contentenums.ErrNotFound)
		}
		return nil, err
	}
	data := map[string]any{}
	if err = json.Unmarshal(e.Data, &data); err != nil {
		return nil, contentenums.ErrDetail(contentenums.ErrDataInvalid, err.Error())
	}
	return toResp(e, data), nil
}

func (s *Service) List(ctx context.Context, req *contentdto.ListReq) (list []*contentdto.ContentResp, err error) {
	if req == nil {
		req = &contentdto.ListReq{}
	}
	entityType, keyword, err := normalizeListFilter(req)
	if err != nil {
		return nil, err
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	rows, err := s.m.List(ctx, entityType, keyword, req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	out := make([]*contentdto.ContentResp, 0, len(rows))
	for _, r := range rows {
		data := map[string]any{}
		if uerr := json.Unmarshal(r.Data, &data); uerr != nil {

			logger.Scene("content").With("id", r.ID).With("err", uerr).Warn("内容数据解析失败，跳过该行")
			continue
		}
		out = append(out, toResp(r, data))
	}
	return out, nil
}

func (s *Service) Count(ctx context.Context, req *contentdto.ListReq) (n int64, err error) {
	entityType, keyword, err := normalizeListFilter(req)
	if err != nil {
		return 0, err
	}
	return s.m.Count(ctx, entityType, keyword)
}

func normalizeListFilter(req *contentdto.ListReq) (entityType, keyword string, err error) {
	if req == nil {
		return "", "", nil
	}
	if req.EntityType != "" && !contentcontract.IsValidType(req.EntityType) {
		return "", "", contentenums.Err(contentenums.ErrInvalidType)
	}
	return req.EntityType, strings.TrimSpace(req.Keyword), nil
}

func (s *Service) Delete(ctx context.Context, req *contentdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return contentenums.Err(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return contentenums.Err(contentenums.ErrNotFound)
		}
		return err
	}
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return err
	}

	s.notifyContentChanged(ctx, e)
	return nil
}

var normalizableMediaFields = map[string]map[string]bool{
	"article": {"featuredImage": true},
}

func normalizeMediaFields(entityType string, data map[string]any) map[string]any {
	fields := normalizableMediaFields[entityType]
	if len(fields) == 0 || data == nil {
		return data
	}
	for key := range fields {
		v, ok := data[key].(string)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		data[key] = upload.StorageURL(v)
	}
	return data
}

func validateData(entityType string, data map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(data))
	for key, val := range data {
		if !contentcontract.IsValidField(entityType, key) {
			return nil, contentenums.ErrDetail(contentenums.ErrInvalidField, fmt.Sprintf("%q", key))
		}
		out[key] = val
	}
	return normalizeMediaFields(entityType, out), nil
}

func toResp(e *contentmodel.Entity, data map[string]any) *contentdto.ContentResp {
	return &contentdto.ContentResp{
		ID: e.ID, EntityType: e.EntityType, Slug: e.Slug,
		Revision: e.Revision, Data: normalizeMediaFields(e.EntityType, data),
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func (s *Service) SetContentStore(store i18n.ContentStore) { s.contentStore = store }

func (s *Service) translateData(ctx context.Context, lang, entityType string, data map[string]any) map[string]any {
	if len(data) == 0 {
		return data
	}
	fields := contentcontract.TranslatableFields(entityType)
	if len(fields) == 0 {
		return data
	}
	type job struct {
		field       string
		source      string
		contextName string
	}
	jobs := make([]job, 0, len(fields))
	hashes := make([]string, 0, len(fields))
	for _, f := range fields {
		v, ok := data[f]
		if !ok {
			continue
		}
		src, ok := v.(string)
		if !ok || !i18n.ShouldTranslateContent(src) {
			continue
		}
		jobs = append(jobs, job{field: f, source: src, contextName: i18n.ContentContext(entityType, f)})
		hashes = append(hashes, i18n.ContentHash(src))
	}
	if len(jobs) == 0 {
		return data
	}

	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	for _, j := range jobs {
		target := tr.TranslateContent(j.source, j.contextName)

		if target != j.source && contentcontract.IsRichTextField(entityType, j.field) {
			target = core.SanitizeRichHTML(target)
		}
		out[j.field] = target
	}
	return out
}
