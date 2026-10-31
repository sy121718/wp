package productcontract

import "go_wp/internal/builder/source"

type ProductDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider

	source.CollectionFilterOptionsProvider
}
