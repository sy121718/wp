package contentcontract

import "go_wp/pkg/builder/source"

type ContentDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider
}
