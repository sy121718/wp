package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

type EntityTemplateOptionResp = productdto.EntityTemplateOptionResp

type EntityTemplatePort interface {
	EntityTemplates(ctx context.Context, projectID, entityType string) (out []EntityTemplateOptionResp, err error)

	DefaultEntityTemplate(ctx context.Context, projectID, entityType string) (templateID string, err error)
}
