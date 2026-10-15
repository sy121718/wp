// Package detailtemplate 内容模板 + 自动发布实例 → 商品「详情页模板」整页选项的适配器。
//
// 位置判据（internal/module/CLAUDE.md §outbound）：这里**要翻译** ——
// contenttemplate 的 TemplateResp（id / 名字 / 草稿版本 / 更新时间 / 文档 / 角色 / 生效标记）
// 与 presentation 的 InstanceResp（实例、渲染模式、产物哈希 …）都不是这一屏要的形状，
// 而这一屏只取各自的两三个字段。
//
// 一个适配器跨两个上游模块，是因为端口本身就是两者之并集（见
// product/contract/detail_template_port.go）。依赖方向：本包 → 两个模块的 **contract**
// （不认识对方的 service / model）。
package detailtemplate

import (
	"context"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// instanceReader 本包需要的自动发布能力（消费者侧最窄接口）。
//
// 收窄成一条而不是直接收 presentationcontract.PresentationService：
// 这一屏的**读**只用到 GetByEntity，写动作（预览 / 首次发布 / 换模板）留在控制器
// （它们要 gin.Context 里的提交值与提示页出口）。收窄后这份依赖在测试里可用一个
// 两行的桩替代，且换 presentation 的实现不会波及这里。
type instanceReader interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
}

// Port 实现 productcontract.DetailTemplatePort。
type Port struct {
	templates contenttemplatecontract.ContentTemplateService
	instances instanceReader
}

// New 构造适配器。
//
// 任一上游为 nil 时对应能力降级（模板清单空 / 默认模板空串 / 没有绑定实例）——
// 与「装配未完成」那一档一致，页面据此显示「还没配模板」而不是报 500。
func New(templates contenttemplatecontract.ContentTemplateService, instances instanceReader) *Port {
	return &Port{templates: templates, instances: instances}
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
			UpdatedAt:    row.UpdatedAt,
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
	if rerr != nil {
		return "", rerr
	}
	if tpl == nil {
		return "", nil
	}
	return tpl.TemplateID, nil
}

// BoundDetailTemplate 该实体当前绑定的发布实例。
//
// 读不到 / 实例为空一律按「没有实例」返回，不往上抛错：这一屏对实例的依赖是展示，
// 前置条件是模板清单 —— 实例读不出来时页面仍要能让人点「首次发布」。
func (p *Port) BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) productcontract.BoundTemplateView {
	if p.instances == nil || entityID == "" {
		return productcontract.BoundTemplateView{}
	}
	// 工程同样显式透下去：presentation 侧 GetByEntity 不带工程时多工程下解析不出唯一工程，
	// 这里按「没有实例」吞掉会让页面把「读不到」显示成「还没发布」。
	inst, ierr := p.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: entityType, EntityID: entityID, ProjectID: projectID,
	})
	if ierr != nil || inst == nil {
		return productcontract.BoundTemplateView{}
	}
	return productcontract.BoundTemplateView{
		Exists:     true,
		TemplateID: inst.TemplateID,
		Status:     inst.Status,
		URLPath:    inst.URLPath,
	}
}
