package productcontract

import (
	"context"
	projectcontract "go_wp/internal/module/project/contract"
)

// CatalogScope 是有效工程范围及其原始工程清单；不携带页面或显示字段。
type CatalogScope struct {
	Projects  []projectcontract.ProjectResp
	ProjectID string
}

type CatalogScopeReader interface {
	ResolveCatalogScope(context.Context, string) (*CatalogScope, error)
}
