package contentservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentenums "go_wp/internal/module/content/enums"
)

const collectionSourcePrefix = "content:"

var _ core.CollectionPager = (*Service)(nil)

func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	items, _, err = s.resolveCollection(ctx, source, filter, 0, 0)
	return items, err
}

func (s *Service) ResolveCollectionPage(ctx context.Context, source string, q core.CollectionQuery) (core.CollectionPage, error) {
	items, total, err := s.resolveCollection(ctx, source, q.Filter, q.Offset, q.Limit)
	if err != nil {
		return core.CollectionPage{}, err
	}
	return core.CollectionPage{Items: items, Total: total}, nil
}

func (s *Service) resolveCollection(ctx context.Context, source string, filter map[string]string, offset, limit int) (items []map[string]any, total int, err error) {
	entityType, ok := strings.CutPrefix(source, collectionSourcePrefix)
	if !ok || !contentcontract.IsValidType(entityType) {
		return nil, 0, contentenums.ErrDetail(contentenums.ErrInvalidType, fmt.Sprintf("%q（期望 content:{product|article|category}）", source))
	}

	fields := contentcontract.CollectionFieldWhitelist(entityType)
	rows, err := s.m.ListForCollection(ctx, entityType, fields, filter, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out := map[string]any{}
		if len(r.Fields) > 0 {
			if uerr := json.Unmarshal(r.Fields, &out); uerr != nil {
				return nil, 0, contentenums.ErrDetail(contentenums.ErrDataInvalid, uerr.Error())
			}
		}

		if !matchFilter(out, filter) {
			continue
		}

		out["id"], out["slug"], out["revision"] = r.ID, r.Slug, r.Revision

		normalizeMediaFields(entityType, out)
		items = append(items, out)
	}

	if limit > 0 && len(rows) < limit {
		total = offset + len(items)
		return items, total, nil
	}
	n, cerr := s.m.CountForCollection(ctx, entityType, filter)
	if cerr != nil {
		return nil, 0, cerr
	}
	return items, int(n), nil
}

func matchFilter(data map[string]any, filter map[string]string) bool {
	for k, want := range filter {
		v, ok := data[k]
		if !ok || scalarString(v) != want {
			return false
		}
	}
	return true
}

func (s *Service) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	types := contentcontract.EntityTypes()
	out := make([]core.CollectionSchema, 0, len(types))
	for _, t := range types {

		key, fallback := contentenums.EntityTypeLabel(t)
		out = append(out, core.CollectionSchema{
			Source:   collectionSourcePrefix + t,
			Label:    fallback,
			LabelKey: key,

			Fields: contentcontract.CollectionFieldWhitelist(t),
		})
	}
	return out, nil
}

var (
	_ core.CollectionResolver       = (*Service)(nil)
	_ core.CollectionSchemaProvider = (*Service)(nil)
)

func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !contentcontract.IsValidType(entityType) {
		return nil, contentenums.Err(contentenums.ErrInvalidType)
	}
	e, gerr := s.m.Get(ctx, entityID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, contentenums.Err(contentenums.ErrNotFound)
		}
		return nil, gerr
	}
	if e.EntityType != entityType {
		return nil, fmt.Errorf("实体 %s 类型 %s 与请求 %s 不符", entityID, e.EntityType, entityType)
	}
	var data map[string]any
	if err = json.Unmarshal(e.Data, &data); err != nil {
		return nil, contentenums.ErrDetail(contentenums.ErrDataInvalid, err.Error())
	}

	lang := core.BuildLang(ctx)
	data = s.translateData(ctx, lang, entityType, data)
	mergeSEOFields(entityType, data)
	return &entityResolver{entityType: entityType, data: data}, nil
}

func mergeSEOFields(entityType string, data map[string]any) {
	if entityType != "article" || data == nil {
		return
	}
	if v, ok := data["title"]; ok {
		data["seoTitle"] = v
	}
	if v, ok := data["excerpt"]; ok {
		data["seoDescription"] = v
	}
}

type entityResolver struct {
	entityType string
	data       map[string]any
}

func (r *entityResolver) ResolveString(field string) (string, error) {
	parts := strings.SplitN(field, ".", 2)
	if len(parts) != 2 {
		return "", contentenums.ErrDetail(contentenums.ErrInvalidField, fmt.Sprintf("%q（期望 entityType.field）", field))
	}
	entityType, fieldName := parts[0], parts[1]
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !contentcontract.IsValidField(entityType, fieldName) {
		return "", contentenums.ErrDetail(contentenums.ErrInvalidField, fmt.Sprintf("%q", fieldName))
	}
	v, ok := r.data[fieldName]
	if !ok || v == nil {
		return "", nil
	}
	return scalarString(v), nil
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		if len(x) > 0 {
			return scalarString(x[0])
		}
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

var _ core.ContentResolver = (*entityResolver)(nil)

type entityFieldSource struct {
	svc        *Service
	entityType string
}

func (e *entityFieldSource) EntityType() string { return e.entityType }

func (e *entityFieldSource) FieldWhitelist() []string {
	return contentcontract.FieldWhitelist(e.entityType)
}

func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range contentcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

var _ core.EntityFieldSource = (*entityFieldSource)(nil)

var _ contentcontract.SearchPort = (*Service)(nil)

func (s *Service) SearchArticles(ctx context.Context, keyword string, limit int) (hits []*contentcontract.ArticleSearchHit, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	rows, err := s.m.SearchArticles(ctx, contentcontract.EntityTypeArticle, keyword, limit)
	if err != nil {
		return nil, err
	}
	hits = make([]*contentcontract.ArticleSearchHit, 0, len(rows))
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.ID) == "" {
			continue
		}
		title, excerpt := articleSearchText(row.Data)
		hits = append(hits, &contentcontract.ArticleSearchHit{
			ID: row.ID, Slug: row.Slug, Title: title, Excerpt: excerpt,
		})
	}
	return hits, nil
}

func articleSearchText(raw json.RawMessage) (title, excerpt string) {
	if len(raw) == 0 {
		return "", ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	title, _ = fields["title"].(string)
	excerpt, _ = fields["excerpt"].(string)
	return strings.TrimSpace(title), strings.TrimSpace(excerpt)
}
