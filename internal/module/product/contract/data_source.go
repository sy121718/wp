package productcontract

import "go_wp/pkg/builder/source"

type ProductDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider

	source.CollectionFilterOptionsProvider
}
