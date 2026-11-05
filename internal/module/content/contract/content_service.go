package contentcontract

import (
	"context"
	"sort"

	"go_wp/internal/module/content/dto"
	"go_wp/pkg/builder/source"
)

var fieldWhitelist = map[string][]string{

	"article": {"title", "body", "excerpt", "featuredImage", "seoTitle", "seoDescription", "focusKeyword"},
}

var translatableFields = map[string]map[string]bool{
	"article": {
		"title":   true,
		"body":    true,
		"excerpt": true,
	},
}

var richTextFields = map[string]bool{"body": true}

func IsRichTextField(_, field string) bool {
	return richTextFields[field]
}

func IsTranslatableField(entityType, field string) bool {
	return translatableFields[entityType][field]
}

func TranslatableFields(entityType string) []string {
	set := translatableFields[entityType]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func EntityTypes() []string {
	out := make([]string, 0, len(fieldWhitelist))
	for t := range fieldWhitelist {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func IsValidType(t string) bool {
	_, ok := fieldWhitelist[t]
	return ok
}

func IsValidField(entityType, field string) bool {
	for _, f := range fieldWhitelist[entityType] {
		if f == field {
			return true
		}
	}
	return false
}

func ContentTypes() []string {
	out := make([]string, 0, len(fieldWhitelist))
	for t := range fieldWhitelist {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func AllFields() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for _, fields := range fieldWhitelist {
		for _, f := range fields {
			if seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func FieldWhitelist(entityType string) []string {
	fields := fieldWhitelist[entityType]
	out := make([]string, len(fields))
	copy(out, fields)
	return out
}

var collectionExcludedFields = map[string]bool{
	"body":         true,
	"focusKeyword": true,
}

func CollectionFieldWhitelist(entityType string) []string {
	fields := fieldWhitelist[entityType]
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if collectionExcludedFields[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func IsCollectionField(entityType, field string) bool {
	if !IsValidField(entityType, field) {
		return false
	}
	return !collectionExcludedFields[field]
}

type ContentService interface {
	ContentDataSource

	Create(ctx context.Context, req *contentdto.CreateReq) (res *contentdto.ContentResp, err error)

	Update(ctx context.Context, req *contentdto.UpdateReq) (res *contentdto.ContentResp, err error)

	Get(ctx context.Context, req *contentdto.GetReq) (res *contentdto.ContentResp, err error)

	List(ctx context.Context, req *contentdto.ListReq) (list []*contentdto.ContentResp, err error)

	Count(ctx context.Context, req *contentdto.ListReq) (n int64, err error)

	Delete(ctx context.Context, req *contentdto.DeleteReq) (err error)

	// BulkDeleteArticles 在一个事务里删除多篇文章：任一失败整批回滚。
	BulkDeleteArticles(ctx context.Context, ids []string) error

	ResolverFor(ctx context.Context, entityType, entityID string) (r source.ContentResolver, err error)

	RegisterEntityTypes(reg source.EntitySourceRegistry) error

	SetDependencyInvalidator(inv DependencyInvalidator)
}

type DependencyInvalidator interface {
	Invalidate(ctx context.Context, kind, key string)
}
