package productservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/money"
)

const collectionItemLimit = 100

var _ core.CollectionPager = (*Service)(nil)

func (s *Service) resolveCollection(ctx context.Context, source string, filter map[string]string, offset, limit int) (items []map[string]any, total int, err error) {
	if source != productcontract.CollectionSourceProduct {
		return nil, 0, fmt.Errorf("%s: %q", productenums.ErrCollectionSourceInvalid, source)
	}
	f, err := parseCollectionFilter(filter)
	if err != nil {
		return nil, 0, err
	}

	f.ProjectID = core.BuildProjectID(ctx)
	rows, err := s.m.ListForCollection(ctx, f, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items = make([]map[string]any, 0, len(rows))
	if len(rows) == 0 {

		n, cerr := s.m.CountForCollection(ctx, f)
		if cerr != nil {
			return nil, 0, cerr
		}
		return items, int(n), nil
	}
	lang := core.BuildLang(ctx)
	ids := make([]string, 0, len(rows))
	attrIDs := make([]string, 0, len(rows))
	seenAttr := map[string]bool{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		for _, id := range decodeStrings(r.AttributeIDs) {
			if !seenAttr[id] {
				seenAttr[id] = true
				attrIDs = append(attrIDs, id)
			}
		}
	}

	variants, err := s.m.ListVariantDisplayFacts(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	attrs, err := s.m.ListAttributesByIDs(ctx, attrIDs, f.ProjectID)
	if err != nil {
		return nil, 0, err
	}

	categoryIndex, brandIndex, tagIndex, ierr := s.taxonomyIndex(ctx, f.ProjectID, rows)
	if ierr != nil {
		return nil, 0, ierr
	}

	pre := s.precomputeRelatedTexts(ctx, lang, categoryIndex, brandIndex, tagIndex, attrs)
	valueBatches := make([]map[string]string, 0, len(rows))
	for _, r := range rows {

		loc := s.localizeRelatedWithPrecomputed(ctx, lang, r, pre, categoryIndex, brandIndex, tagIndex, attrs)
		valueBatches = append(valueBatches, productFieldValues(r, byProduct[r.ID], attrs, loc))
	}
	s.translateFieldsBatch(ctx, lang, productcontract.EntityTypeProduct, valueBatches)
	var publishedPaths map[string]string
	if s.publishedLocator != nil && f.ProjectID != "" && len(ids) > 0 {
		publishedPaths, err = s.publishedLocator.PublishedEntityPaths(ctx, f.ProjectID, productcontract.EntityTypeProduct, lang, ids)
		if err != nil {
			return nil, 0, err
		}
	}
	for i, r := range rows {
		item := collectionItem(r, valueBatches[i])
		if p := publishedPaths[r.ID]; p != "" {

			item["url"] = seo.AbsoluteSiteURL(p)
		}
		items = append(items, item)
	}

	if limit > 0 && len(rows) < limit {
		return items, offset + len(items), nil
	}
	n, cerr := s.m.CountForCollection(ctx, f)
	if cerr != nil {
		return nil, 0, cerr
	}
	return items, int(n), nil
}

func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	items, _, err = s.resolveCollection(ctx, source, filter, 0, collectionItemLimit)
	return items, err
}

func (s *Service) ResolveCollectionPage(ctx context.Context, source string, q core.CollectionQuery) (core.CollectionPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = collectionItemLimit
	}
	items, total, err := s.resolveCollection(ctx, source, q.Filter, q.Offset, limit)
	if err != nil {
		return core.CollectionPage{}, err
	}
	return core.CollectionPage{Items: items, Total: total}, nil
}

func (s *Service) tagIndex(ctx context.Context, projectID string, rows []*productmodel.ProductEntity) (map[string]*productmodel.ProductTagEntity, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, id := range decodeStrings(row.TagIDs) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	tags := map[string]*productmodel.ProductTagEntity{}
	if len(ids) == 0 {
		return tags, nil
	}
	loaded, err := s.m.ListTagsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	for _, row := range loaded {
		tags[row.ID] = row
	}
	return tags, nil
}

func (s *Service) taxonomyIndex(ctx context.Context, projectID string, rows []*productmodel.ProductEntity) (categories map[string]*productmodel.ProductCategoryEntity, brands map[string]*productmodel.ProductBrandEntity, tags map[string]*productmodel.ProductTagEntity, err error) {
	categoryIDs, brandIDs := []string{}, []string{}
	seenCategory, seenBrand := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		for _, id := range decodeStrings(r.CategoryIDs) {
			if !seenCategory[id] {
				seenCategory[id] = true
				categoryIDs = append(categoryIDs, id)
			}
		}
		if r.BrandID != nil && !seenBrand[*r.BrandID] {
			seenBrand[*r.BrandID] = true
			brandIDs = append(brandIDs, *r.BrandID)
		}
	}
	categories, brands, tags = map[string]*productmodel.ProductCategoryEntity{}, map[string]*productmodel.ProductBrandEntity{}, map[string]*productmodel.ProductTagEntity{}
	if len(categoryIDs) > 0 {
		categoryRows, cerr := s.m.ListCategoriesByIDs(ctx, categoryIDs, projectID)
		if cerr != nil {
			return nil, nil, nil, cerr
		}
		for _, row := range categoryRows {
			categories[row.ID] = row
		}
	}
	if len(brandIDs) > 0 {
		brandRows, berr := s.m.ListBrandsByIDs(ctx, brandIDs, projectID)
		if berr != nil {
			return nil, nil, nil, berr
		}
		for _, row := range brandRows {
			brands[row.ID] = row
		}
	}
	tags, err = s.tagIndex(ctx, projectID, rows)
	return categories, brands, tags, err
}

func (s *Service) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{
		Source:   productcontract.CollectionSourceProduct,
		Label:    productcontract.CollectionLabel,
		LabelKey: productcontract.CollectionLabelKey,
		Fields:   productcontract.FieldWhitelist(productcontract.EntityTypeProduct),
		Filters:  productcontract.CollectionFilters(),
		OrderBy:  productcontract.CollectionOrderBy(),
	}}, nil
}

func parseCollectionFilter(filter map[string]string) (f productmodel.CollectionFilter, err error) {
	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !productcontract.IsCollectionFilterKey(k) {
			return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
		}
		v := strings.TrimSpace(filter[k])
		if v == "" {
			continue
		}

		if attrKey, ok := strings.CutPrefix(k, productcontract.CollectionFilterOptionPrefix); ok {

			values := splitCSV(v)
			for _, value := range values {
				if !optionKeyRe.MatchString(value) {
					return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
				}
			}
			if !optionKeyRe.MatchString(attrKey) || len(values) == 0 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			if f.Options == nil {
				f.Options = map[string][]string{}
			}
			f.Options[attrKey] = values
			continue
		}
		switch k {
		case productcontract.CollectionFilterStatus:
			f.Status = v
		case productcontract.CollectionFilterCategoryID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.CategoryID = v
		case productcontract.CollectionFilterBrandID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.BrandID = v
		case productcontract.CollectionFilterTagID:
			if !isUUID(v) {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.TagID = v
		case productcontract.CollectionFilterCategoryIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.CategoryIDs = ids
		case productcontract.CollectionFilterBrandIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.BrandIDs = ids
		case productcontract.CollectionFilterTagIDs:
			ids, err := parseIDList(k, v)
			if err != nil {
				return f, err
			}
			f.TagIDs = ids
		case productcontract.CollectionFilterCategoryMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.CategoryAll = all
		case productcontract.CollectionFilterBrandMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.BrandAll = all
		case productcontract.CollectionFilterTagMode:
			all, merr := parseMultiMode(k, v)
			if merr != nil {
				return f, merr
			}
			f.TagAll = all
		case productcontract.CollectionFilterMinRating:

			rating, rerr := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if rerr != nil || rating < 0 || rating > 5 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			f.MinRating = &rating
		case productcontract.CollectionFilterMinPrice, productcontract.CollectionFilterMaxPrice:

			amount, perr := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if perr != nil || amount < 0 {
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
			if k == productcontract.CollectionFilterMinPrice {
				f.MinPrice = &amount
			} else {
				f.MaxPrice = &amount
			}
		case productcontract.CollectionFilterOnSale:
			switch v {
			case "true", "1":
				f.OnSale = true
			case "false", "0":
				f.OnSale = false
			default:
				return f, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, k)
			}
		}
	}

	if len(f.CategoryIDs) > 0 {
		f.CategoryID = ""
	}
	if len(f.BrandIDs) > 0 {
		f.BrandID = ""
	}
	if len(f.TagIDs) > 0 {
		f.TagID = ""
	}

	return f, validatePriceRange(f)
}

func parseIDList(key, raw string) ([]string, error) {
	ids := splitCSV(raw)
	for _, id := range ids {
		if !isUUID(id) {
			return nil, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, key)
		}
	}
	return ids, nil
}

func parseMultiMode(key, raw string) (all bool, err error) {
	switch strings.TrimSpace(raw) {
	case productcontract.CollectionTagModeAll:
		return true, nil
	case productcontract.CollectionTagModeAny:
		return false, nil
	default:
		return false, fmt.Errorf("%s: %q", productenums.ErrCollectionFilterInvalid, key)
	}
}

func validatePriceRange(f productmodel.CollectionFilter) error {
	if f.MinPrice != nil && f.MaxPrice != nil && *f.MinPrice > *f.MaxPrice {
		return fmt.Errorf("%s: minPrice 大于 maxPrice", productenums.ErrCollectionFilterInvalid)
	}
	return nil
}

func splitCSV(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		v := strings.TrimSpace(part)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

var optionKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func collectionItem(p *productmodel.ProductEntity, values map[string]string) map[string]any {
	item := make(map[string]any, len(values)+2)
	for k, v := range values {
		item[k] = v
	}
	item["id"] = p.ID
	item["slug"] = p.Slug
	item["images"] = imageURLsAny(p)

	item["createdAt"] = p.CreatedAt.UnixMilli()

	if p.MinPrice != nil {
		item["minPrice"] = *p.MinPrice
	}

	if avg, _, ok := p.RatingSummaryOf(); ok {
		item["ratingValue"] = avg
	}
	return item
}

func imageURLsAny(p *productmodel.ProductEntity) []any {
	urls := imageURLs(p)
	out := make([]any, 0, len(urls))
	for _, u := range urls {
		out = append(out, u)
	}
	return out
}

var (
	_ core.CollectionResolver       = (*Service)(nil)
	_ core.CollectionSchemaProvider = (*Service)(nil)
	_ core.CollectionSourceProvider = (*Service)(nil)
)

var _ core.CollectionFilterOptionsProvider = (*Service)(nil)

func (s *Service) CollectionFilterOptions(ctx context.Context, source, projectID string) (out core.CollectionFilterOptions, err error) {
	if strings.TrimSpace(source) != productcontract.CollectionSourceProduct {
		return out, nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return out, fmt.Errorf("缺少工程 ID，无法给出筛选选项")
	}
	lang := core.BuildLang(ctx)

	categories, cerr := s.m.ListCategories(ctx, projectID, "")
	if cerr != nil {
		return out, cerr
	}
	for _, row := range categories {
		parent := ""
		if row.ParentID != nil {
			parent = *row.ParentID
		}
		out.Categories = append(out.Categories, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.categoryValues(ctx, lang, row), row.Name), ParentID: parent,
		})
	}

	brands, berr := s.m.ListBrands(ctx, projectID, "", 0, 0)
	if berr != nil {
		return out, berr
	}
	for _, row := range brands {
		out.Brands = append(out.Brands, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.brandValues(ctx, lang, row), row.Name),
		})
	}

	tags, terr := s.m.ListTags(ctx, projectID, "", "", 0, 0)
	if terr != nil {
		return out, terr
	}
	for _, row := range tags {
		out.Tags = append(out.Tags, core.CollectionFilterChoice{
			ID: row.ID, Name: filterOptionName(s.tagValues(ctx, lang, row), row.Name),
		})
	}

	attrs, aerr := s.m.ListAttributesByProject(ctx, projectID)
	if aerr != nil {
		return out, aerr
	}
	for _, row := range attrs {

		if !row.IsVariation {
			continue
		}
		labels := s.attributeValueTranslations(ctx, lang, row)
		attr := core.CollectionFilterAttributeGroup{
			Key: row.Key, Name: filterOptionName(s.attributeValues(ctx, lang, row), row.Name),
		}
		for _, value := range decodeAttributeValueOptions(row.Values) {
			name := strings.TrimSpace(labels[value.Key])
			if name == "" {
				name = value.Label
			}
			attr.Values = append(attr.Values, core.CollectionFilterChoice{
				Key: value.Key, Name: name,
			})
		}
		if len(attr.Values) > 0 {
			out.Attributes = append(out.Attributes, attr)
		}
	}
	return out, nil
}

type attributeValueOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func decodeAttributeValueOptions(raw json.RawMessage) []attributeValueOption {
	if len(raw) == 0 {
		return nil
	}
	var rows []attributeValueOption
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]attributeValueOption, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func filterOptionName(values map[string]string, fallback string) string {
	if name := strings.TrimSpace(values["name"]); name != "" {
		return name
	}
	return fallback
}

func productFieldValues(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) map[string]string {
	out := map[string]string{
		"name":        p.Name,
		"subtitle":    p.Subtitle,
		"description": descriptionHTML(p.Description),

		"seoTitle":       p.Name,
		"seoDescription": p.Subtitle,
		"imageAlts":      imageAltsJSON(p, loc),
		"slug":           p.Slug,
		"unit":           p.Unit,
		"images":         imagesJSON(p),
		"defaultImage":   mediaURL(p.DefaultImage),
		"options":        optionsJSON(p, variants, attrs, loc),
		"variants":       variantsJSON(variants),
		"related":        relatedJSON(p, loc),
		"tags":           tagsJSON(p, loc),
	}
	if len(variants) > 0 {
		out["sku"] = variants[0].SKUCode

		summary := summarizeVariantPrices(variants, true)
		if summary.HasAny {
			out["price"] = formatPrice(summary.MinPrice)
			out["minPrice"] = formatPrice(summary.MinPrice)
			out["maxPrice"] = formatPrice(summary.MaxPrice)
			if summary.MinPrice == summary.MaxPrice {
				out["priceRange"] = formatPrice(summary.MinPrice)
			} else {
				out["priceRange"] = formatPrice(summary.MinPrice) + " ~ " + formatPrice(summary.MaxPrice)
			}
		}

		if compare := summary.ComparePrice(); compare != nil {
			out["comparePrice"] = formatPrice(*compare)
		}
	}

	if avg, count, ok := p.RatingSummaryOf(); ok {
		out["rating"] = strconv.FormatFloat(avg, 'f', 2, 64)
		out["ratingCount"] = strconv.Itoa(count)
	}

	if alts := decodeStrings(json.RawMessage(out["imageAlts"])); len(alts) > 0 {
		out["imageAlt"] = alts[0]
	}
	return out
}

func tagsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	names := make([]string, 0, 4)
	if p == nil {
		return "[]"
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		name := loc.name(productcontract.EntityTypeTag, id, "name", row.Name)
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}

type relatedTexts struct {
	categories map[string]*productmodel.ProductCategoryEntity
	brands     map[string]*productmodel.ProductBrandEntity
	tags       map[string]*productmodel.ProductTagEntity
	attributes map[string]*productmodel.ProductAttributeEntity
	fields     map[string]map[string]string

	attrLabels map[string]map[string]string

	imageAlts map[string]string
}

func relatedFieldKey(entityType, id string) string {
	return entityType + "\x00" + id
}

func emptyRelated() *relatedTexts {
	return &relatedTexts{
		categories: map[string]*productmodel.ProductCategoryEntity{},
		brands:     map[string]*productmodel.ProductBrandEntity{},
		tags:       map[string]*productmodel.ProductTagEntity{},
		attributes: map[string]*productmodel.ProductAttributeEntity{},
		fields:     map[string]map[string]string{},
		attrLabels: map[string]map[string]string{},
		imageAlts:  map[string]string{},
	}
}

func (r *relatedTexts) name(entityType, id, field, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.fields[relatedFieldKey(entityType, id)][field]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func (r *relatedTexts) label(attrID, valueKey, fallback string) string {
	if r == nil {
		return fallback
	}
	if v, ok := r.attrLabels[attrID][valueKey]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func relatedJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	type named struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	out := struct {
		Categories []named `json:"categories"`
		Brand      *named  `json:"brand,omitempty"`
		Tags       []named `json:"tags"`
	}{Categories: []named{}, Tags: []named{}}
	if p == nil {
		return "{}"
	}
	for _, id := range decodeStrings(p.CategoryIDs) {
		row, ok := loc.categories[id]
		if !ok {
			continue
		}
		out.Categories = append(out.Categories, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeCategory, id, "name", row.Name),
		})
	}
	if p.BrandID != nil {
		if row, ok := loc.brands[*p.BrandID]; ok {
			out.Brand = &named{Slug: row.Slug, Name: loc.name(productcontract.EntityTypeBrand, row.ID, "name", row.Name)}
		}
	}
	for _, id := range decodeStrings(p.TagIDs) {
		row, ok := loc.tags[id]
		if !ok {
			continue
		}
		out.Tags = append(out.Tags, named{
			Slug: row.Slug,
			Name: loc.name(productcontract.EntityTypeTag, id, "name", row.Name),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func imageAltsJSON(p *productmodel.ProductEntity, loc *relatedTexts) string {
	alts := decodeStrings(p.ImageAlts)
	out := make([]string, len(alts))
	for i, alt := range alts {
		out[i] = alt
		if loc == nil {
			continue
		}
		if target, ok := loc.imageAlts[alt]; ok && strings.TrimSpace(target) != "" {
			out[i] = target
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func variantOptionKeys(variants []*productmodel.VariantEntity) map[string]bool {
	out := map[string]bool{}
	for _, v := range variants {
		if v == nil || len(v.OptionValues) == 0 {
			continue
		}
		var values map[string]string
		if err := json.Unmarshal(v.OptionValues, &values); err != nil {
			continue
		}
		for key := range values {
			if key != "" {
				out[key] = true
			}
		}
	}
	return out
}

func optionsJSON(p *productmodel.ProductEntity, variants []*productmodel.VariantEntity, attrs []*productmodel.ProductAttributeEntity, loc *relatedTexts) string {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	used := variantOptionKeys(variants)
	groups := []optionGroupJSON{}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}

		if !used[a.Key] {
			continue
		}
		values := []optionValueJSON{}
		for _, v := range normalizeValuesFromRaw(a.Values) {
			if !v.Enabled {
				continue
			}
			values = append(values, optionValueJSON{Key: v.Key, Label: loc.label(id, v.Key, v.Label)})
		}
		if len(values) == 0 {
			continue
		}
		groups = append(groups, optionGroupJSON{
			Key:    a.Key,
			Name:   loc.name(productcontract.EntityTypeAttribute, id, "name", a.Name),
			Values: values,
		})
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func variantsJSON(variants []*productmodel.VariantEntity) string {
	rows := []variantJSON{}
	for _, v := range variants {
		row := variantJSON{
			ID:  v.ID,
			SKU: v.SKUCode, Price: formatPrice(v.Price), Image: mediaURL(v.Image),
			Enabled: v.Enabled, Options: map[string]string{},
		}
		if v.ComparePrice != nil {
			row.ComparePrice = formatPrice(*v.ComparePrice)
		}
		if len(v.OptionValues) > 0 {
			_ = json.Unmarshal(v.OptionValues, &row.Options)
		}
		if row.Options == nil {
			row.Options = map[string]string{}
		}
		rows = append(rows, row)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func valueLabelsJSON(values []productdto.AttributeValueResp) string {
	labels := make([]string, 0, len(values))
	for _, v := range values {
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

func stringListJSON(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func formatPrice(v float64) string {
	return money.FormatYuan(v)
}

func imagesJSON(p *productmodel.ProductEntity) string {
	urls := imageURLs(p)
	b, err := json.Marshal(urls)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func imageURLs(p *productmodel.ProductEntity) []string {
	urls := []string{}
	if len(p.Images) > 0 {
		_ = json.Unmarshal(p.Images, &urls)
	}
	if len(urls) == 0 && p.DefaultImage != "" {
		urls = []string{p.DefaultImage}
	}
	return mediaURLs(urls)
}

func descriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.HTML != "" {
		return obj.HTML
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

var (
	_ core.EntityFieldSource = (*entityFieldSource)(nil)
	_ core.ContentResolver   = (*entityResolver)(nil)
)

type entityFieldSource struct {
	svc        *Service
	entityType string
}

func (e *entityFieldSource) EntityType() string { return e.entityType }

func (e *entityFieldSource) FieldWhitelist() []string {
	return productcontract.FieldWhitelist(e.entityType)
}

func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range productcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !productcontract.IsValidType(entityType) {
		return nil, productenums.Err(productenums.ErrInvalidType)
	}
	lang := core.BuildLang(ctx)

	projectID := strings.TrimSpace(core.BuildProjectID(ctx))
	if projectID == "" {
		return nil, fmt.Errorf("%s: 构建上下文缺少工程 id（core.WithBuildProjectID），无法按工程隔离读取实体字段源",
			productenums.ErrMissingProjectContext)
	}
	switch entityType {
	case productcontract.EntityTypeCategory:
		var row *productmodel.ProductCategoryEntity
		if row, err = s.m.GetCategory(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.categoryValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeBrand:
		var row *productmodel.ProductBrandEntity
		if row, err = s.m.GetBrand(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.brandValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeTag:
		var row *productmodel.ProductTagEntity
		if row, err = s.m.GetTag(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.tagValues(ctx, lang, row)}, nil
	case productcontract.EntityTypeAttribute:
		var row *productmodel.ProductAttributeEntity
		if row, err = s.m.GetAttribute(ctx, entityID, projectID); err != nil {
			return nil, mapNotFound(err)
		}
		return &entityResolver{entityType: entityType, values: s.attributeValues(ctx, lang, row)}, nil
	}

	e, gerr := s.m.Get(ctx, entityID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	variants, verr := s.m.ListVariantDisplayFacts(ctx, []string{e.ID})
	if verr != nil {
		return nil, verr
	}

	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	loc, lerr := s.localizeRelated(ctx, lang, e)
	if lerr != nil {
		return nil, lerr
	}
	values := productFieldValues(e, variants, attrs, loc)
	return &entityResolver{entityType: entityType, values: s.translateFields(ctx, lang, productcontract.EntityTypeProduct, values)}, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return productenums.Err(productenums.ErrNotFound)
	}
	return err
}

func (r *entityResolver) ResolveString(field string) (string, error) {
	entityType, name, ok := splitEntityField(field)
	if !ok {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", productenums.ErrInvalidField, field)
	}
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !productcontract.IsValidField(entityType, name) {
		return "", fmt.Errorf("%s: %q", productenums.ErrInvalidField, name)
	}
	return r.values[name], nil
}

func splitEntityField(field string) (entityType, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(field), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[1], ".") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

var seoAliasFields = map[string]map[string]string{
	productcontract.EntityTypeProduct:  {"seoTitle": "name", "seoDescription": "subtitle"},
	productcontract.EntityTypeCategory: {"seoTitle": "name", "seoDescription": "description"},
	productcontract.EntityTypeBrand:    {"seoTitle": "name", "seoDescription": "description"},
}

func (s *Service) categoryValues(ctx context.Context, lang string, e *productmodel.ProductCategoryEntity) map[string]string {
	values := map[string]string{
		"name":        e.Name,
		"slug":        e.Slug,
		"description": e.Description,
		"image":       e.Image,

		"seoTitle":       e.Name,
		"seoDescription": e.Description,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeCategory, values)
}

func (s *Service) brandValues(ctx context.Context, lang string, e *productmodel.ProductBrandEntity) map[string]string {
	values := map[string]string{
		"name":        e.Name,
		"slug":        e.Slug,
		"logo":        e.Logo,
		"description": e.Description,

		"seoTitle":       e.Name,
		"seoDescription": e.Description,
	}
	return s.translateFields(ctx, lang, productcontract.EntityTypeBrand, values)
}

func (s *Service) tagValues(ctx context.Context, lang string, e *productmodel.ProductTagEntity) map[string]string {
	values := map[string]string{"name": e.Name, "slug": e.Slug}
	return s.translateFields(ctx, lang, productcontract.EntityTypeTag, values)
}

func (s *Service) attributeValues(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	values := map[string]string{
		"name":   e.Name,
		"key":    e.Key,
		"values": valueLabelsJSON(normalizeValuesFromRaw(e.Values)),
	}
	values = s.translateFields(ctx, lang, productcontract.EntityTypeAttribute, values)
	values["values"] = s.translatedValueLabelsJSON(ctx, lang, e)
	return values
}

func (s *Service) translatedValueLabelsJSON(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) string {
	if e == nil {
		return "[]"
	}
	items := normalizeValuesFromRaw(e.Values)
	translated := s.attributeValueTranslations(ctx, lang, e)
	labels := make([]string, 0, len(items))
	for _, v := range items {
		if target, ok := translated[v.Key]; ok {
			labels = append(labels, target)
			continue
		}
		labels = append(labels, v.Label)
	}
	return stringListJSON(labels)
}

func (s *Service) translateFields(ctx context.Context, lang string, entityType string, values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	s.translateFieldsBatch(ctx, lang, entityType, []map[string]string{values})
	return values
}

func (s *Service) translateFieldsBatch(ctx context.Context, lang, entityType string, batches []map[string]string) {
	if lang == "" || s.contentStore == nil || len(batches) == 0 {
		return
	}
	type workItem struct {
		batchIdx int
		field    string
		context  string
		source   string
	}
	work := make([]workItem, 0, len(batches)*4)
	hashes := make([]string, 0, len(batches)*4)
	seenHash := map[string]bool{}
	for i, values := range batches {
		if len(values) == 0 {
			continue
		}
		for _, f := range productcontract.TranslatableFields(entityType) {
			if f == "values" {
				continue
			}
			v, ok := values[f]
			if !ok || !i18n.ShouldTranslateContent(v) {
				continue
			}
			h := i18n.ContentHash(v)
			if !seenHash[h] {
				seenHash[h] = true
				hashes = append(hashes, h)
			}
			work = append(work, workItem{
				batchIdx: i,
				field:    f,
				context:  productcontract.FieldContext(entityType, f),
				source:   v,
			})
		}
	}
	if len(hashes) == 0 {

		applySEOAliases(entityType, batches)
		return
	}

	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, item := range work {
		batches[item.batchIdx][item.field] = tr.TranslateContent(item.source, item.context)
	}
	applySEOAliases(entityType, batches)
}

func applySEOAliases(entityType string, batches []map[string]string) {
	aliases := seoAliasFields[entityType]
	if len(aliases) == 0 {
		return
	}
	for _, values := range batches {
		for alias, source := range aliases {
			if v, ok := values[source]; ok {
				values[alias] = v
			}
		}
	}
}

func (s *Service) translateTexts(ctx context.Context, lang, contextName string, sources []string) map[string]string {
	out := map[string]string{}
	if lang == "" || s.contentStore == nil || contextName == "" || len(sources) == 0 {
		return out
	}
	hashes := make([]string, 0, len(sources))
	uniq := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, src := range sources {
		if !i18n.ShouldTranslateContent(src) || seen[src] {
			continue
		}
		seen[src] = true
		uniq = append(uniq, src)
		hashes = append(hashes, i18n.ContentHash(src))
	}
	if len(hashes) == 0 {
		return out
	}

	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	for _, src := range uniq {
		if target := tr.TranslateContent(src, contextName); target != src {
			out[src] = target
		}
	}
	return out
}

func (s *Service) attributeValueTranslations(ctx context.Context, lang string, e *productmodel.ProductAttributeEntity) map[string]string {
	out := map[string]string{}
	if e == nil {
		return out
	}
	values := normalizeValuesFromRaw(e.Values)
	sources := make([]string, 0, len(values))
	for _, v := range values {
		sources = append(sources, v.Label)
	}
	translated := s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeAttribute, "values"), sources)
	if len(translated) == 0 {
		return out
	}
	for _, v := range values {
		if target, ok := translated[v.Label]; ok {
			out[v.Key] = target
		}
	}
	return out
}

func (s *Service) imageAltTranslations(ctx context.Context, lang string, p *productmodel.ProductEntity) map[string]string {
	if p == nil {
		return map[string]string{}
	}
	alts := decodeStrings(p.ImageAlts)
	if len(alts) == 0 {
		return map[string]string{}
	}
	return s.translateTexts(ctx, lang, productcontract.FieldContext(productcontract.EntityTypeProduct, "imageAlts"), alts)
}

type entityResolver struct {
	entityType string
	values     map[string]string
}

func (s *Service) localizeRelated(ctx context.Context, lang string, e *productmodel.ProductEntity) (loc *relatedTexts, err error) {
	loc = emptyRelated()
	if e == nil {
		return loc, nil
	}
	if lang == "" || s.contentStore == nil {
		return loc, nil
	}

	if ids := decodeStrings(e.CategoryIDs); len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids, e.ProjectID)
		if cerr != nil {
			return nil, cerr
		}
		for _, row := range rows {
			loc.categories[row.ID] = row
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID)

		switch {
		case berr == nil:
			loc.brands[row.ID] = row
		case errors.Is(berr, gorm.ErrRecordNotFound):

		default:
			return nil, berr
		}
	}
	if ids := decodeStrings(e.TagIDs); len(ids) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, ids, e.ProjectID)
		if terr != nil {
			return nil, terr
		}
		for _, row := range rows {
			loc.tags[row.ID] = row
		}
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(e.AttributeIDs), e.ProjectID)
	if aerr != nil {
		return nil, aerr
	}
	return s.fillRelatedTexts(ctx, lang, e, loc, attrs), nil
}

func (s *Service) precomputeRelatedTexts(ctx context.Context, lang string,
	categories map[string]*productmodel.ProductCategoryEntity,
	brands map[string]*productmodel.ProductBrandEntity,
	tags map[string]*productmodel.ProductTagEntity,
	attrs []*productmodel.ProductAttributeEntity) *relatedTexts {
	loc := emptyRelated()
	for id, row := range categories {
		loc.categories[id] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeCategory, id)] = s.categoryValues(ctx, lang, row)
	}
	for id, row := range brands {
		loc.brands[id] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeBrand, id)] = s.brandValues(ctx, lang, row)
	}
	for id, row := range tags {
		loc.tags[id] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeTag, id)] = s.tagValues(ctx, lang, row)
	}
	for _, row := range attrs {
		loc.attributes[row.ID] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeAttribute, row.ID)] = s.attributeValues(ctx, lang, row)
		loc.attrLabels[row.ID] = s.attributeValueTranslations(ctx, lang, row)
	}

	return loc
}

func (s *Service) localizeRelatedWithPrecomputed(ctx context.Context, lang string,
	e *productmodel.ProductEntity, pre *relatedTexts,
	categories map[string]*productmodel.ProductCategoryEntity,
	brands map[string]*productmodel.ProductBrandEntity,
	tags map[string]*productmodel.ProductTagEntity,
	attrs []*productmodel.ProductAttributeEntity) *relatedTexts {
	loc := emptyRelated()
	if e == nil {
		return loc
	}
	for id, row := range categories {
		loc.categories[id] = row
	}
	for id, row := range brands {
		loc.brands[id] = row
	}
	for id, row := range tags {
		loc.tags[id] = row
	}
	for _, row := range attrs {
		loc.attributes[row.ID] = row
	}
	if pre != nil {
		for k, v := range pre.fields {
			loc.fields[k] = v
		}
		for k, v := range pre.attrLabels {
			loc.attrLabels[k] = v
		}
	}
	if lang != "" && s.contentStore != nil {
		loc.imageAlts = s.imageAltTranslations(ctx, lang, e)
	}
	return loc
}

func (s *Service) fillRelatedTexts(ctx context.Context, lang string, e *productmodel.ProductEntity, loc *relatedTexts, attrs []*productmodel.ProductAttributeEntity) *relatedTexts {
	if lang == "" || s.contentStore == nil {
		return loc
	}
	for id, row := range loc.categories {
		loc.fields[relatedFieldKey(productcontract.EntityTypeCategory, id)] = s.categoryValues(ctx, lang, row)
	}
	for id, row := range loc.brands {
		loc.fields[relatedFieldKey(productcontract.EntityTypeBrand, id)] = s.brandValues(ctx, lang, row)
	}
	for id, row := range loc.tags {
		loc.fields[relatedFieldKey(productcontract.EntityTypeTag, id)] = s.tagValues(ctx, lang, row)
	}
	for _, row := range attrs {
		loc.attributes[row.ID] = row
		loc.fields[relatedFieldKey(productcontract.EntityTypeAttribute, row.ID)] = s.attributeValues(ctx, lang, row)
		loc.attrLabels[row.ID] = s.attributeValueTranslations(ctx, lang, row)
	}
	if e != nil {
		loc.imageAlts = s.imageAltTranslations(ctx, lang, e)
	}
	return loc
}

type optionGroupJSON struct {
	Key    string            `json:"key"`
	Name   string            `json:"name"`
	Values []optionValueJSON `json:"values"`
}

type optionValueJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice,omitempty"`
	Image        string            `json:"image,omitempty"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}
