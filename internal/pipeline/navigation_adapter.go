package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"strings"

	"go_wp/pkg/builder/core"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

type NavigationAdapter struct {
	Svc       navigationcontract.NavigationService
	Project   projectcontract.ProjectService
	Ctx       context.Context
	ProjectID string
	Lang      string

	Rule      *publish.LangURLRule
	SiteLangs []string

	ContentStore i18n.ContentStore
	cache        map[string][]core.NavigationItem
}

func (a *NavigationAdapter) ResolveMenu(projectID, kind string) (items []core.NavigationItem, err error) {
	key := projectID + "|" + kind
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.Svc.Tree(a.Ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	items = a.itemsOf(nodes, projectID)
	a.translateLabels(items, projectID)
	if a.cache == nil {
		a.cache = map[string][]core.NavigationItem{}
	}
	a.cache[key] = items
	return items, nil
}

func (a *NavigationAdapter) ResolveNavigation(projectID, navigationID string) (items []core.NavigationItem, err error) {
	key := "nav|" + projectID + "|" + navigationID
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.Svc.TreeByID(a.Ctx, projectID, navigationID)
	if err != nil {
		return nil, err
	}
	items = a.itemsOf(nodes, projectID)
	a.translateLabels(items, projectID)
	if a.cache == nil {
		a.cache = map[string][]core.NavigationItem{}
	}
	a.cache[key] = items
	return items, nil
}

func (a *NavigationAdapter) translateLabels(items []core.NavigationItem, projectID string) {
	if a.Lang == "" || len(items) == 0 {
		return
	}
	var sources []string
	var collect func(list []core.NavigationItem)
	collect = func(list []core.NavigationItem) {
		for i := range list {
			if i18n.ShouldTranslateContent(list[i].Label) {
				sources = append(sources, list[i].Label)
			}
			collect(list[i].Children)
		}
	}
	collect(items)
	if len(sources) == 0 {
		return
	}
	hashes := make([]string, 0, len(sources))
	for _, s := range sources {
		hashes = append(hashes, i18n.ContentHash(s))
	}

	tr := i18n.NewContentTranslatorScoped(a.Ctx, projectID, a.ContentStore, a.Lang, hashes)
	contextName := i18n.ContentContext("navigation", "label")
	var apply func(list []core.NavigationItem)
	apply = func(list []core.NavigationItem) {
		for i := range list {
			list[i].Label = tr.TranslateContent(list[i].Label, contextName)
			apply(list[i].Children)
		}
	}
	apply(items)
}

func (a *NavigationAdapter) localizeURL(projectID, raw string) string {
	if a.Rule != nil {
		return LocalizeMenuURLWith(*a.Rule, a.SiteLangs, a.Lang, raw)
	}
	return LocalizeMenuURL(a.Ctx, a.Project, projectID, a.Lang, raw)
}

func (a *NavigationAdapter) itemsOf(nodes []*navigationdto.NavigationNode, projectID string) []core.NavigationItem {
	out := make([]core.NavigationItem, 0, len(nodes))
	for _, n := range nodes {
		panelBlockID := ""
		if n.PanelBlockID != nil {
			panelBlockID = strings.TrimSpace(*n.PanelBlockID)
		}
		out = append(out, core.NavigationItem{
			Label:        n.Title,
			URL:          a.localizeURL(projectID, n.Path),
			Target:       n.Target,
			Children:     a.itemsOf(n.Children, projectID),
			PanelBlockID: panelBlockID,
			PanelWidth:   n.PanelWidth,
		})
	}
	return out
}
