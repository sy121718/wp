package source

import "context"

type ContentResolver interface {
	ResolveString(field string) (string, error)
}

type EntityFieldSource interface {
	EntityType() string

	FieldWhitelist() []string

	ResolverFor(ctx context.Context, entityID string) (ContentResolver, error)
}

type FieldRef struct {
	EntityType string

	Field string

	CollectionSource string
}

type EntitySourceRegistry interface {
	Register(src EntityFieldSource) error

	Lookup(entityType string) (EntityFieldSource, bool)

	IsValidType(entityType string) bool

	FieldWhitelist(entityType string) []string

	ResolverFor(ctx context.Context, entityType, entityID string) (ContentResolver, error)
}
