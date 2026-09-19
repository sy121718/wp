package routers

// content_template_impact_refs.go — 模板引用反查的装配实现（影响面提示与删除保护）。
//
// 端口定义在 contenttemplate 模块（contenttemplatecontract.TemplateImpactPort），
// 实现放在装配层：它要同时读 page 契约（页面草稿文档 settings.structure 的槽位绑定）
// 与 presentation 契约（实例绑定的模板 + 实例文档里的结构绑定）——两个模块的只读面
// 在这里拼装，contenttemplate 模块因此既不认识页面表、也不认识实例表。
//
// 为什么页面侧走 ListDrafts 全站扫描而不是依赖表：
//   · page_dependencies 里的 content_template:{id} 行只覆盖**构建过**的页面，
//     且当前 page 契约没有「按依赖键只读反查」的入口（那是写路径 MarkStaleByDependency），
//     拿一次读操作去改 stale 列是错的；
//   · 页面文档里的 settings.structure 才是绑定本身（构建期就是按它决定用哪套模板），
//     扫描它同时覆盖「还没构建过的页面」——删模板时恰恰要拦住这一类。
// 依赖表那条来源的缺口写在交付汇报里（需要 page 契约补一个只读反查方法）。

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	"go_wp/pkg/logger"

	"go_wp/internal/builder"
)

// contentTemplateImpactRefs 引用反查端口的装配实现。
type contentTemplateImpactRefs struct {
	pages         pagecontract.PageService
	presents      presentationcontract.PresentationService
}

// ContentTemplateImpactPort 构造模板引用反查端口（装配期交给 contenttemplate service）。
func ContentTemplateImpactPort(pages pagecontract.PageService,
	presentations presentationcontract.PresentationService) contenttemplatecontract.TemplateImpactPort {
	return &contentTemplateImpactRefs{pages: pages, presents: presentations}
}

// wireContentTemplateImpact 把引用反查端口注入 contenttemplate 模块。
//
// 为什么不能省：页面文档里的 settings.structure 绑定写在 JSONB 里，数据库外键管不到。
// 缺这条注入的表现是「删掉一套正被页面当页眉用的模板一路成功」，站点页眉在下一次
// 重建后消失，构建日志里只留一行 Warn —— 所以缺注入必须留下痕迹（Warn），
// 并且页面会把它显示成「引用查不出来」而不是「没有引用」。
func (a *assembly) wireContentTemplateImpact() {
	setter, ok := a.contentTemplateSvc.(interface {
		SetImpactPort(contenttemplatecontract.TemplateImpactPort)
	})
	if !ok {
		logger.Scene("init").Warn("contenttemplate 未提供引用反查注入点（SetImpactPort）：" +
			"模板影响面与删除保护退化为仅查本模块表与实例外键")
		return
	}
	setter.SetImpactPort(ContentTemplateImpactPort(a.pageService, a.presentationSvc))
	// 同一批接线里注入「结构模板候选」（主题设置页的「选结构模板」下拉数据源）：
	// 两者都依赖 contenttemplate 契约，分开两处注入只会让「谁负责接哪个端口」变得难查。
	if optSetter, ok := a.projectService.(interface {
		SetStructureTemplateOptionsPort(projectcontract.StructureTemplateOptionsPort)
	}); ok {
		optSetter.SetStructureTemplateOptionsPort(&structureTemplateOptionsPort{templates: a.contentTemplateSvc})
	} else {
		logger.Scene("init").Warn("project 模块未提供结构模板候选注入点（SetStructureTemplateOptionsPort）：" +
			"主题设置页的「选结构模板」下拉将只有「不绑定」")
	}
}

// ListTemplateReferences 实现 contenttemplatecontract.TemplateImpactPort。
//
// 一次扫描服务整页：页面草稿（全站一次）与实例（本工程一次）各取一遍，
// 在内存里按模板 id 归集，调用方（列表页）不必为每套模板各扫一次。
func (p *contentTemplateImpactRefs) ListTemplateReferences(ctx context.Context, projectID string,
	templateIDs []string) (refs []contenttemplatedto.TemplateReference, unparsable int, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len(templateIDs) == 0 {
		// 工程作用域是读取前提：空工程 id 一律返回空，**不**退化成「不限工程」。
		return nil, 0, nil
	}
	known := make(map[string]struct{}, len(templateIDs))
	for _, id := range templateIDs {
		if id = strings.TrimSpace(id); id != "" {
			known[id] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	add := func(ref contenttemplatedto.TemplateReference) {
		key := ref.Kind + "|" + ref.TemplateID + "|" + ref.PageID + ref.InstanceID
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}

	// 1) 页面草稿文档：settings.structure 的槽位绑定。
	if p.pages != nil {
		drafts, derr := p.pages.ListDrafts(ctx)
		if derr != nil {
			return nil, 0, derr
		}
		for i := range drafts {
			d := &drafts[i]
			if strings.TrimSpace(d.ProjectID) != projectID {
				continue
			}
			title, slotsByTemplate, perr := structureBindingsInDocument(d.DraftDocument)
			if perr != nil {
				unparsable++
				// 解析不了仍然粗判一次（模板 id 是 uuid，误命中概率极低）：
				// 命中即按「引用了、槽位未知」记一条 —— 跳过等于放行一次可能丢绑定的删除。
				for id := range known {
					if bytes.Contains(d.DraftDocument, []byte(id)) {
						add(contenttemplatedto.TemplateReference{
							TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage,
							PageID: d.ID, PagePath: d.DraftPath,
						})
					}
				}
				continue
			}
			for id, slots := range slotsByTemplate {
				if _, ok := known[id]; !ok {
					continue
				}
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage, Slots: slots,
					PageID: d.ID, PagePath: d.DraftPath, PageTitle: title,
				})
			}
		}
	}

	// 2) 自动发布实例：绑定的模板 + 实例文档里的结构绑定。
	if p.presents != nil {
		list, lerr := p.presents.List(ctx, &presentationdto.ListReq{ProjectID: projectID})
		if lerr != nil {
			return nil, 0, lerr
		}
		for _, inst := range list {
			if inst == nil {
				continue
			}
			if tid := strings.TrimSpace(inst.TemplateID); tid != "" {
				if _, ok := known[tid]; ok {
					add(contenttemplatedto.TemplateReference{
						TemplateID: tid, Kind: contenttemplatedto.ReferenceKindInstance,
						InstanceID: inst.ID, EntityType: inst.EntityType, EntityID: inst.EntityID,
						URLPath: inst.URLPath, RenderMode: inst.RenderMode,
					})
				}
			}
			if len(inst.Document) == 0 {
				continue
			}
			_, slotsByTemplate, perr := structureBindingsInDocument(inst.Document)
			if perr != nil {
				unparsable++
				continue
			}
			for id, slots := range slotsByTemplate {
				if _, ok := known[id]; !ok {
					continue
				}
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindInstance, Slots: slots,
					InstanceID: inst.ID, EntityType: inst.EntityType, EntityID: inst.EntityID,
					URLPath: inst.URLPath, RenderMode: inst.RenderMode,
				})
			}
		}
	}

	// 确定性输出：同一份数据渲染出来的行序不随 map 迭代变化（列表页会按它渲染）。
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].TemplateID != refs[j].TemplateID {
			return refs[i].TemplateID < refs[j].TemplateID
		}
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].PageID+refs[i].InstanceID < refs[j].PageID+refs[j].InstanceID
	})
	return refs, unparsable, nil
}

// structureBindingsInDocument 从一份 Page Document 里取出「模板 id → 命中的槽位」与 SEO 标题。
//
// 用 builder.StructureBindings 解析而不是手写 map 取值：头部 / 页脚两条旧通道
//（headerTemplateId / footerTemplateId）与 slotTemplates 的合并规则只有那一处实现，
// 手抄一份必然与构建期漂移（漏掉旧通道 = 漏掉一半绑定，而且只在删除保护上表现为放行）。
func structureBindingsInDocument(doc json.RawMessage) (title string,
	slotsByTemplate map[string][]string, err error) {
	if len(doc) == 0 {
		return "", nil, nil
	}
	var parsed struct {
		Settings struct {
			Structure builder.StructureBindings `json:"structure"`
			SEO       struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if jerr := json.Unmarshal(doc, &parsed); jerr != nil {
		return "", nil, jerr
	}
	for slot, id := range parsed.Settings.Structure.TemplateBindings() {
		if slotsByTemplate == nil {
			slotsByTemplate = map[string][]string{}
		}
		slotsByTemplate[id] = append(slotsByTemplate[id], slot)
	}
	for id := range slotsByTemplate {
		sort.Strings(slotsByTemplate[id])
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title), slotsByTemplate, nil
}
// structureTemplateOptionsPort 主题设置页「选结构模板」下拉的候选来源。
//
// 走 contenttemplate 契约的只读 List（按显式工程），只保留结构模板类型：
// 下拉里出现内容实体模板（product / article）是错的 —— 那类模板的绑定入口在
// 商品 / 文章侧，混进来只会让人以为可以把它当页眉用。
type structureTemplateOptionsPort struct {
	templates contenttemplatecontract.ContentTemplateService
}

// ListStructureTemplateOptions 实现 projectcontract.StructureTemplateOptionsPort。
func (p *structureTemplateOptionsPort) ListStructureTemplateOptions(ctx context.Context, projectID string) (
	opts []projectcontract.StructureTemplateOption, err error) {
	if p == nil || p.templates == nil || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	list, lerr := p.templates.List(ctx, &contenttemplatedto.ListReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, t := range list {
		if t == nil || !isStructureTemplateEntityType(t.EntityType) {
			continue
		}
		opts = append(opts, projectcontract.StructureTemplateOption{
			ID: t.ID, Name: t.Name, EntityType: t.EntityType, IsDefault: t.IsDefault,
		})
	}
	return opts, nil
}

// isStructureTemplateEntityType 是否结构模板类型（页眉 / 页脚）。
//
// 判据与 contenttemplate 模块的白名单同源（entity_type = header / footer）：
// 字符串字面量只出现在这一处，改动时与 contenttemplatemodel.IsStructureTemplateType 同批改。
func isStructureTemplateEntityType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case "header", "footer":
		return true
	default:
		return false
	}
}
