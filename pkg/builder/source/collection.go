package source

import (
	"context"
	"strings"
)

type CollectionResolver interface {
	ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error)
}

type CollectionSchema struct {
	Source string

	Label string

	LabelKey string

	Fields []string

	Filters []CollectionFilter

	OrderBy []string
}

type CollectionSchemaProvider interface {
	CollectionSchemas(ctx context.Context) ([]CollectionSchema, error)
}

type CollectionQuery struct {
	Filter map[string]string

	Offset int

	Limit int
}

type CollectionPage struct {
	Items []map[string]any

	Total int
}

type CollectionPager interface {
	ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error)
}

type CollectionFilter struct {
	Key  string   `json:"key"`
	Enum []string `json:"enum,omitempty"`

	Prefix bool `json:"prefix,omitempty"`
}

func ContentContextFor(entityType, field string) string {
	typ := strings.TrimSpace(entityType)
	f := strings.TrimSpace(field)
	if typ == "" || f == "" {
		return ""
	}
	return typ + "." + f
}
