package source

import "context"

type CollectionFilterChoice struct {
	ID string

	Key string

	Name string

	ParentID string
}

type CollectionFilterAttributeGroup struct {
	Key  string
	Name string

	Values []CollectionFilterChoice
}

type CollectionFilterOptions struct {
	Categories []CollectionFilterChoice
	Brands     []CollectionFilterChoice
	Tags       []CollectionFilterChoice
	Attributes []CollectionFilterAttributeGroup
}

type CollectionFilterOptionsProvider interface {
	CollectionFilterOptions(ctx context.Context, source, projectID string) (CollectionFilterOptions, error)
}
