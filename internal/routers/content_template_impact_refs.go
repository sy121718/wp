package routers

import (
	"bytes"
	"cmp"
	"context"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"slices"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"

	"go_wp/pkg/builder"
)

type contentTemplateImpactRefs struct {
	pages    pagecontract.PageService
	presents presentationcontract.PresentationService
}

func ContentTemplateImpactPort(pages pagecontract.PageService,
	presentations presentationcontract.PresentationService) contenttemplatecontract.TemplateImpactPort {
	return &contentTemplateImpactRefs{pages: pages, presents: presentations}
}

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
	a.marks.mark(portContentTemplateImpactPort)

	if optSetter, ok := a.projectService.(interface {
		SetStructureTemplateOptionsPort(projectcontract.StructureTemplateOptionsPort)
	}); ok {
		optSetter.SetStructureTemplateOptionsPort(&structureTemplateOptionsPort{templates: a.contentTemplateSvc})
		a.marks.mark(portProjectStructureTemplates)
	} else {
		logger.Scene("init").Warn("project 模块未提供结构模板候选注入点（SetStructureTemplateOptionsPort）：" +
			"主题设置页的「选结构模板」下拉将只有「不绑定」")
	}
}

func (p *contentTemplateImpactRefs) ListTemplateReferences(ctx context.Context, projectID string,
	templateIDs []string) (refs []contenttemplatedto.TemplateReference, unparsable int, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len(templateIDs) == 0 {

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

	if p.pages != nil {
		for id := range known {
			dep := publish.ContentTemplateKey(id)
			hit, derr := p.pages.FindPagesByDependency(ctx, projectID, dep.Kind, dep.Key)
			if derr != nil {

				return nil, 0, derr
			}
			for i := range hit {
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage,
					PageID: hit[i].ID, PageTitle: hit[i].Title,
				})
			}
		}
	}

	slices.SortFunc(refs, func(a, b contenttemplatedto.TemplateReference) int {
		return cmp.Or(
			cmp.Compare(a.TemplateID, b.TemplateID),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.PageID+a.InstanceID, b.PageID+b.InstanceID),
		)
	})
	return refs, unparsable, nil
}

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
		slices.Sort(slotsByTemplate[id])
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title), slotsByTemplate, nil
}

type structureTemplateOptionsPort struct {
	templates contenttemplatecontract.ContentTemplateService
}

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
		if t == nil || !contenttemplatecontract.IsStructureTemplateType(t.EntityType) {
			continue
		}
		opts = append(opts, projectcontract.StructureTemplateOption{
			ID: t.ID, Name: t.Name, EntityType: t.EntityType, IsDefault: t.IsDefault,
		})
	}
	return opts, nil
}
