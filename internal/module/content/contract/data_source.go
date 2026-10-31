package contentcontract

import "go_wp/internal/builder/source"

type ContentDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider
}
