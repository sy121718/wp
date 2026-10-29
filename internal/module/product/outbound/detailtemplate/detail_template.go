package detailtemplate

import (
	"context"
	"go_wp/pkg/facing"
	"net/http"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
)

// instanceReader 读取实体当前发布实例。
type instanceReader interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
}

// modeReader 提供文档快照与模板引用统计；未注入时只读取绑定事实。
type modeReader interface {
	ListSnapshots(ctx context.Context, req *presentationcontract.ListSnapshotsReq) ([]*presentationdto.SnapshotSummary, error)
	CountByTemplate(ctx context.Context, req *presentationcontract.CountByTemplateReq) (*presentationdto.CountByTemplateResp, error)
}

// Port 实现 productcontract.DetailTemplatePort。
type Port struct {
	templates contenttemplatecontract.ContentTemplateService
	instances instanceReader
	mode      modeReader
}

// New 构造适配器。
//
// 任一上游为 nil 时对应能力降级（模板清单空 / 默认模板空串 / 没有绑定实例）——
// 与「装配未完成」那一档一致，页面据此显示「还没配模板」而不是报 500。
func New(templates contenttemplatecontract.ContentTemplateService, instances instanceReader, mode ...modeReader) *Port {
	p := &Port{templates: templates, instances: instances}
	if len(mode) > 0 {
		p.mode = mode[0]
	}
	return p
}

var _ productcontract.DetailTemplatePort = (*Port)(nil)

// DetailTemplateCandidates 本工程内该实体类型的模板清单。
//
// 工程显式透给下游：contenttemplate.List 在 req.ProjectID 为空时只认「唯一工程」，
// 多工程部署下回 ErrProjectRequired（这一屏的主数据，读不到即整页失败）。
func (p *Port) DetailTemplateCandidates(ctx context.Context, projectID, entityType string) (out []productcontract.DetailTemplateCandidateView, err error) {
	out = []productcontract.DetailTemplateCandidateView{}
	if p.templates == nil {
		return out, nil
	}
	rows, lerr := p.templates.List(ctx, &contenttemplatecontract.ListReq{EntityType: entityType, ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, productcontract.DetailTemplateCandidateView{
			ID:           row.ID,
			Name:         row.Name,
			DraftVersion: row.DraftVersion,
			UpdatedAt:    row.UpdatedAt.TimeOrZero(),
		})
	}
	return out, nil
}

// DefaultDetailTemplate 本工程内该实体类型当前解析到的默认模板 id。
//
// 走 ResolveTemplateScoped（显式工程）而不是无工程的 ResolveTemplate：后者在多工程下回
// ErrProjectRequired。这里已经有工程 id，不该退到「唯一工程」的兜底。
func (p *Port) DefaultDetailTemplate(ctx context.Context, projectID, entityType string) (templateID string, err error) {
	if p.templates == nil {
		return "", nil
	}
	tpl, rerr := p.templates.ResolveTemplateScoped(ctx, projectID, entityType)
	if status, ok := facing.StatusOf(rerr); ok && status == http.StatusNotFound {
		return "", nil
	}
	if rerr != nil {
		return "", rerr
	}
	if tpl == nil {
		return "", nil
	}
	return tpl.TemplateID, nil
}

// BoundDetailTemplate 区分“尚无实例”和“实例查询失败”。
func (p *Port) BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) (productcontract.BoundTemplateView, error) {
	if p.instances == nil || entityID == "" {
		return productcontract.BoundTemplateView{}, nil
	}
	inst, err := p.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: entityType, EntityID: entityID, ProjectID: projectID,
	})
	if status, ok := facing.StatusOf(err); ok && status == http.StatusNotFound {
		return productcontract.BoundTemplateView{}, nil
	}
	if err != nil {
		return productcontract.BoundTemplateView{}, err
	}
	if inst == nil {
		return productcontract.BoundTemplateView{}, nil
	}
	return productcontract.BoundTemplateView{
		Exists: true, InstanceID: inst.ID, TemplateID: inst.TemplateID,
		Status: inst.Status, URLPath: inst.URLPath, RenderMode: inst.RenderMode,
		SourceTemplateVersionID: inst.SourceTemplateVersionID,
	}, nil
}

// DetailTemplatePanel 读取发布关联事实；任何已装配查询失败都向上返回。
func (p *Port) DetailTemplatePanel(ctx context.Context, projectID, entityType, entityID string) (productcontract.DetailTemplatePanelFacts, error) {
	out := productcontract.DetailTemplatePanelFacts{}
	if p.templates == nil || p.instances == nil || entityID == "" {
		return out, nil
	}
	out.Available = true
	var err error
	out.Bound, err = p.BoundDetailTemplate(ctx, projectID, entityType, entityID)
	if err != nil {
		return out, err
	}
	if !out.Bound.Exists {
		return out, nil
	}
	if out.Bound.RenderMode == presentationcontract.RenderModeDocument && out.Bound.SourceTemplateVersionID != "" {
		tpl, err := p.templates.ResolveTemplateScoped(ctx, projectID, entityType)
		if status, ok := facing.StatusOf(err); ok && status == http.StatusNotFound {
			err = nil
		}
		if err != nil {
			return out, err
		}
		if tpl != nil {
			out.CurrentTemplateVersionID = tpl.VersionID
		}
	}
	if p.mode == nil {
		return out, nil
	}
	if out.Bound.RenderMode == presentationcontract.RenderModeDocument {
		snaps, err := p.mode.ListSnapshots(ctx, &presentationcontract.ListSnapshotsReq{InstanceID: out.Bound.InstanceID, ProjectID: projectID, Limit: 8})
		if err != nil {
			return out, err
		}
		out.Snapshots = make([]productcontract.DetailTemplateSnapshotFacts, 0, len(snaps))
		for _, snap := range snaps {
			if snap != nil {
				out.Snapshots = append(out.Snapshots, productcontract.DetailTemplateSnapshotFacts{ID: snap.ID, CreatedAt: snap.CreatedAt})
			}
		}
	}
	count, err := p.mode.CountByTemplate(ctx, &presentationcontract.CountByTemplateReq{TemplateID: out.Bound.TemplateID, ProjectID: projectID})
	if err != nil {
		return out, err
	}
	if count != nil {
		out.HasAffectedTemplateCount = true
		out.AffectedTemplateCount = count.TemplateMode
	}
	return out, nil
}
