// Package contenttemplate 内容模板数据 → 商品页面选项视图的适配器（形状翻译层）。
//
// 位置判据（internal/module/CLAUDE.md §outbound）：这里**要翻译** —— 内容模板的
// TemplateResp / ResolvedTemplate 与商品页面卡片区要的「id + 名字」不是同一个形状，
// 而且只取其中两个字段（卡片区不展示模板的版本、角色、文档）。
//
// 依赖方向：本包 → 内容模板模块的 **contract**（不认识对方的 service / model），
// 加上本模块的 contract / dto。
package contenttemplate

import (
	"context"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// Options 实现 productcontract.EntityTemplatePort。
type Options struct {
	svc contenttemplatecontract.ContentTemplateService
}

// New 构造适配器。svc 为 nil 时两个方法都返回「没有候选 / 没有默认模板」，
// 调用方据此渲染「模板能力未装配」（与注入前 `h.templates == nil` 的降级同一档）。
func New(svc contenttemplatecontract.ContentTemplateService) *Options {
	return &Options{svc: svc}
}

var _ productcontract.EntityTemplatePort = (*Options)(nil)

// EntityTemplates 该实体类型的模板清单。
//
// ListReq 只带 EntityType（与读模型下沉前逐字一致）：模板清单的作用域口径属于内容模板
// 模块的取数层，商品页不在这里替它决定「要不要按工程过滤」。
func (o *Options) EntityTemplates(ctx context.Context, entityType string) (out []productdto.EntityTemplateOptionView, err error) {
	out = []productdto.EntityTemplateOptionView{}
	if o.svc == nil {
		return out, nil
	}
	rows, lerr := o.svc.List(ctx, &contenttemplatecontract.ListReq{EntityType: entityType})
	if lerr != nil {
		return nil, lerr
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, productdto.EntityTemplateOptionView{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

// DefaultEntityTemplate 该实体类型当前解析到的模板 id（未绑定时不报错、返回空串）。
func (o *Options) DefaultEntityTemplate(ctx context.Context, entityType string) (templateID string, err error) {
	if o.svc == nil {
		return "", nil
	}
	tpl, rerr := o.svc.ResolveTemplate(ctx, entityType)
	if rerr != nil {
		return "", rerr
	}
	if tpl == nil {
		return "", nil
	}
	return tpl.TemplateID, nil
}
