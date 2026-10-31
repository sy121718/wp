package contenttemplate

import (
	"context"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

type Options struct {
	svc contenttemplatecontract.ContentTemplateService
}

func New(svc contenttemplatecontract.ContentTemplateService) *Options {
	return &Options{svc: svc}
}

var _ productcontract.EntityTemplatePort = (*Options)(nil)

func (o *Options) EntityTemplates(ctx context.Context, projectID, entityType string) (out []productdto.EntityTemplateOptionResp, err error) {
	out = []productdto.EntityTemplateOptionResp{}
	if o.svc == nil {
		return out, nil
	}
	rows, lerr := o.svc.List(ctx, &contenttemplatecontract.ListReq{EntityType: entityType, ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, productdto.EntityTemplateOptionResp{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func (o *Options) DefaultEntityTemplate(ctx context.Context, projectID, entityType string) (templateID string, err error) {
	if o.svc == nil {
		return "", nil
	}
	tpl, rerr := o.svc.ResolveTemplateScoped(ctx, projectID, entityType)
	if rerr != nil {
		return "", rerr
	}
	if tpl == nil {
		return "", nil
	}
	return tpl.TemplateID, nil
}
