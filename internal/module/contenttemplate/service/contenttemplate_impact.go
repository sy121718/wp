package contenttemplateservice

// contenttemplate_impact.go — 模板引用反查（影响面提示与删除保护）。
//
// 与「结构模板被其它模板绑定」那条（contenttemplate_service.go 的 structureTemplateRefs，
// 读本模块自己的 content_templates 表）互补：这里回答的是**页面与自动发布实例**层面的引用 ——
// 页面草稿文档 settings.structure 的绑定、实例的 template_id 与覆盖文档绑定。
// 三者的共同点是都写在 JSONB 文档或跨模块的表里，数据库外键要么管不到（页面）、
// 要么只说得清「被引用」却说不清「是谁」（实例外键）。
//
// 数据来源由装配层经 TemplateImpactPort 注入（最窄只读接口）：本模块不认识页面表与实例表，
// 也不 import 对方的 service/model —— 依赖方向是 contenttemplate ← 装配。

import (
	"context"
	"fmt"
	"sort"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	"go_wp/pkg/logger"
)

// SetImpactPort 注入引用反查端口（装配期调用；未注入时 Impact 回 Available=false，
// 删除路径退化为「只查本模块的表 + 外键兜底」，并各记一条日志让能力缺失可见）。
func (s *Service) SetImpactPort(p contenttemplatecontract.TemplateImpactPort) { s.impact = p }

// Impact 列出工程内引用了各模板的页面与实例（列表页整页渲染与删除保护共用一次扫描）。
func (s *Service) Impact(ctx context.Context, req *contenttemplatedto.ImpactReq) (res *contenttemplatedto.ImpactResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ImpactReq{}
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if s.impact == nil {
		// Available=false 不是「没有引用」：调用方必须把它显示成「查不出来」而不是 0。
		return &contenttemplatedto.ImpactResp{Available: false, References: []contenttemplatedto.TemplateReference{}}, nil
	}
	ids, lerr := s.templateIDsOf(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	refs, unparsable, ierr := s.impact.ListTemplateReferences(ctx, projectID, ids)
	if ierr != nil {
		return nil, ierr
	}
	if refs == nil {
		refs = []contenttemplatedto.TemplateReference{}
	}
	return &contenttemplatedto.ImpactResp{Available: true, References: refs, Unparsable: unparsable}, nil
}

// referencesOfTemplate 汇总「引用某模板」的可定位描述（页面 + 实例），删除保护用。
//
// 端口未装配时返回 nil 且记日志：装配层必然注入（缺失是装配缺陷），但把「未装配」
// 当成「有引用」会让整个模块一条都删不掉 —— 那是在用故障换故障。
func (s *Service) referencesOfTemplate(ctx context.Context, projectID, templateID string) (refs []string, err error) {
	if s.impact == nil {
		logger.Scene("contenttemplate").With("templateId", templateID).
			Warn("模板引用反查未装配：本次删除未检查页面 / 实例绑定")
		return nil, nil
	}
	ids, lerr := s.templateIDsOf(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	all, _, ierr := s.impact.ListTemplateReferences(ctx, projectID, ids)
	if ierr != nil {
		return nil, ierr
	}
	for _, r := range all {
		if r.TemplateID != templateID {
			continue
		}
		refs = append(refs, templateReferenceText(r))
	}
	sort.Strings(refs)
	return refs, nil
}

// templateIDsOf 取工程内全部模板 id：引用扫描要认出文档里的绑定 id，
// 而「文档解析不了」时要退回字符串粗判，粗判同样需要这份集合。
func (s *Service) templateIDsOf(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.m.List(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if id := strings.TrimSpace(r.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// templateReferenceText 一条引用记录 → 面向运营的一句话（删除拦截的错误明细 / 日志）。
//
// 文案里给出**可定位数据**（页面标题或路径 / 实例的实体与线上路径）：只写「被引用了」
// 会让人无从下手，而这一页的处置方式是「先到那个引用方解绑」。
func templateReferenceText(r contenttemplatedto.TemplateReference) string {
	switch r.Kind {
	case contenttemplatedto.ReferenceKindPage:
		name := strings.TrimSpace(r.PageTitle)
		if name == "" {
			name = strings.TrimSpace(r.PagePath)
		}
		if name == "" {
			name = r.PageID
		}
		if len(r.Slots) > 0 {
			return fmt.Sprintf("页面《%s》的%s", name, slotsLabelText(r.Slots))
		}
		return fmt.Sprintf("页面《%s》", name)
	case contenttemplatedto.ReferenceKindInstance:
		name := strings.TrimSpace(r.URLPath)
		if name == "" {
			name = r.EntityType + ":" + r.EntityID
		}
		if strings.TrimSpace(r.EntityType) != "" {
			return fmt.Sprintf("自动发布实例（%s %s，%s）", r.EntityType, r.EntityID, name)
		}
		return fmt.Sprintf("自动发布实例（%s）", name)
	default:
		return "未知来源的引用"
	}
}

// slotsLabelText 槽位列表 → 「页眉 / 页脚」这类面向运营的说法（排序后拼接）。
func slotsLabelText(slots []string) string {
	labels := make([]string, 0, len(slots))
	for _, slot := range slots {
		labels = append(labels, structureSlotLabel(slot))
	}
	sort.Strings(labels)
	return strings.Join(labels, " / ")
}