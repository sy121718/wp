package productcontract

import (
	"sort"
	"strings"

	"go_wp/pkg/builder/source"
	productenums "go_wp/internal/module/product/enums"
)

const (
	EntityTypeProduct   = "product"
	EntityTypeCategory  = "product_category"
	EntityTypeBrand     = "product_brand"
	EntityTypeTag       = "product_tag"
	EntityTypeAttribute = "product_attribute"
)

var fieldWhitelist = map[string][]string{
	EntityTypeProduct: {
		"name", "subtitle", "description",

		"seoTitle", "seoDescription",
		"imageAlt", "imageAlts",
		"slug", "sku", "unit",

		"url",
		"images", "defaultImage",
		"price", "comparePrice", "priceRange", "minPrice", "maxPrice",

		"rating", "ratingCount",
		"options", "variants",
		"related",

		"tags",

		"createdAt",
	},
	EntityTypeCategory: {
		"name", "slug", "description", "image",
		"seoTitle", "seoDescription",
	},
	EntityTypeBrand: {
		"name", "slug", "logo", "description",
		"seoTitle", "seoDescription",
	},
	EntityTypeTag: {
		"name", "slug",
	},
	EntityTypeAttribute: {
		"name", "key", "values",
	},
}

var translatableFields = map[string]map[string]bool{
	EntityTypeProduct: {
		"name":        true,
		"subtitle":    true,
		"description": true,
		"imageAlts":   true,
	},
	EntityTypeCategory: {
		"name":        true,
		"description": true,
	},
	EntityTypeBrand: {
		"name":        true,
		"description": true,
	},
	EntityTypeTag: {
		"name": true,
	},
	EntityTypeAttribute: {
		"name":   true,
		"values": true,
	},
}

func EntityTypes() []string {
	return []string{EntityTypeProduct, EntityTypeCategory, EntityTypeBrand, EntityTypeTag, EntityTypeAttribute}
}

func IsValidType(t string) bool {
	_, ok := fieldWhitelist[t]
	return ok
}

func FieldWhitelist(entityType string) []string {
	src, ok := fieldWhitelist[entityType]
	if !ok {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func SortedFieldWhitelist(entityType string) []string {
	out := FieldWhitelist(entityType)
	sort.Strings(out)
	return out
}

func IsValidField(entityType, field string) bool {
	for _, f := range fieldWhitelist[entityType] {
		if f == field {
			return true
		}
	}
	return false
}

func IsTranslatableField(entityType, field string) bool {
	return translatableFields[entityType][field]
}

func TranslatableFields(entityType string) []string {
	src := translatableFields[entityType]
	if len(src) == 0 {
		return nil
	}
	out := make([]string, 0, len(src))
	for _, f := range fieldWhitelist[entityType] {
		if src[f] {
			out = append(out, f)
		}
	}
	return out
}

func FieldContext(entityType, field string) string {
	return source.ContentContextFor(entityType, field)
}

const CollectionSourceProduct = "content:product"

const CollectionLabel = "商品列表"

const CollectionLabelKey = "admin.collection.product"

const (
	CollectionFilterStatus = "status"

	CollectionFilterCategoryID = "categoryId"

	CollectionFilterBrandID = "brandId"

	CollectionFilterTagID = "tagId"

	CollectionFilterOption = "option"

	CollectionFilterOptionPrefix = CollectionFilterOption + "."

	CollectionFilterCategoryIDs = "categoryIds"
	CollectionFilterBrandIDs    = "brandIds"
	CollectionFilterTagIDs      = "tagIds"

	CollectionFilterCategoryMode = "categoryMode"
	CollectionFilterBrandMode    = "brandMode"
	CollectionFilterTagMode      = "tagMode"

	CollectionFilterMinPrice = "minPrice"
	CollectionFilterMaxPrice = "maxPrice"

	CollectionFilterMinRating = "minRating"

	CollectionFilterOnSale = "onSale"

	CollectionTagModeAny = "any"
	CollectionTagModeAll = "all"
)

var collectionFilters = []source.CollectionFilter{
	{Key: CollectionFilterStatus, Enum: []string{productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived}},
	{Key: CollectionFilterCategoryID},
	{Key: CollectionFilterCategoryIDs},
	{Key: CollectionFilterCategoryMode, Enum: []string{CollectionTagModeAny, CollectionTagModeAll}},
	{Key: CollectionFilterBrandID},
	{Key: CollectionFilterBrandIDs},
	{Key: CollectionFilterBrandMode, Enum: []string{CollectionTagModeAny, CollectionTagModeAll}},
	{Key: CollectionFilterTagID},
	{Key: CollectionFilterTagIDs},

	{Key: CollectionFilterTagMode, Enum: []string{CollectionTagModeAny, CollectionTagModeAll}},
	{Key: CollectionFilterOnSale},

	{Key: CollectionFilterMinPrice},
	{Key: CollectionFilterMaxPrice},

	{Key: CollectionFilterMinRating},

	{Key: CollectionFilterOption, Prefix: true},
}

var collectionOrderKeys = []string{"sort", "createdAt", "priceAsc", "priceDesc", "ratingDesc"}

func CollectionFilters() []source.CollectionFilter {
	out := make([]source.CollectionFilter, 0, len(collectionFilters))
	for _, f := range collectionFilters {
		copied := source.CollectionFilter{Key: f.Key, Prefix: f.Prefix}
		if len(f.Enum) > 0 {
			copied.Enum = make([]string, len(f.Enum))
			copy(copied.Enum, f.Enum)
		}
		out = append(out, copied)
	}
	return out
}

func IsCollectionFilterKey(key string) bool {
	for _, f := range collectionFilters {
		if f.Prefix {
			if sub, ok := strings.CutPrefix(key, f.Key+"."); ok && sub != "" {
				return true
			}
			continue
		}
		if f.Key == key {
			return true
		}
	}
	return false
}

func CollectionOrderBy() []string {
	out := make([]string, len(collectionOrderKeys))
	copy(out, collectionOrderKeys)
	return out
}
