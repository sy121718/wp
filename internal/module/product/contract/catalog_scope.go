package productcontract

import (
	"context"
	projectcontract "go_wp/internal/module/project/contract"
)

type CatalogScope struct {
	Projects  []projectcontract.ProjectResp
	ProjectID string
}

type CatalogScopeReader interface {
	ResolveCatalogScope(context.Context, string) (*CatalogScope, error)
}
