package source

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
)

type Resolver struct {
	pages     pagecontract.PageService
	contents  contentcontract.ContentService
	instances presentationcontract.PresentationService
	blocks    blockcontract.BlockService
}

func New(pages pagecontract.PageService, contents contentcontract.ContentService,
	instances presentationcontract.PresentationService, blocks blockcontract.BlockService) *Resolver {
	return &Resolver{pages: pages, contents: contents, instances: instances, blocks: blocks}
}

var _ navigationcontract.SourceResolver = (*Resolver)(nil)

func (r *Resolver) ResolveSource(ctx context.Context, projectID, sourceType, sourceID string) (title, url string, err error) {
	switch strings.TrimSpace(sourceType) {
	case "page":
		return r.resolvePage(ctx, projectID, sourceID)
	case "article", "product", "category":
		return r.resolveContent(ctx, sourceType, sourceID)
	case "block":
		return r.resolveBlock(ctx, projectID, sourceID)
	}
	return "", "", nil
}

func (r *Resolver) Candidates(ctx context.Context, projectID string) (groups []navigationcontract.SourceGroup, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, nil
	}
	out := make([]navigationcontract.SourceGroup, 0, 5)
	if g := r.pageCandidates(ctx, projectID); len(g.Items) > 0 {
		out = append(out, g)
	}
	for _, entityType := range []string{"article", "product", "category"} {
		if g := r.contentCandidates(ctx, entityType); len(g.Items) > 0 {
			out = append(out, g)
		}
	}

	return out, nil
}

func sourceGroupTitleKey(entityType string) string {
	switch strings.TrimSpace(entityType) {
	case "page":
		return navigationenums.SourcePage
	case "article":
		return navigationenums.SourceArticle
	case "product":
		return navigationenums.SourceProduct
	case "category":
		return navigationenums.SourceCategory
	default:

		return strings.TrimSpace(entityType)
	}
}

func (r *Resolver) pageCandidates(ctx context.Context, projectID string) navigationcontract.SourceGroup {
	group := navigationcontract.SourceGroup{Type: "page", Title: sourceGroupTitleKey("page")}
	if r.pages == nil {
		return group
	}
	titles, err := r.pages.ListPageTitles(ctx, projectID)
	if err != nil {
		return group
	}
	for _, p := range titles {
		url := p.DraftPath
		if p.ActivePath != nil && *p.ActivePath != "" {
			url = *p.ActivePath
		}
		title := strings.TrimSpace(p.SEOTitle)
		if title == "" {
			title = p.DraftPath
		}
		group.Items = append(group.Items, navigationcontract.SourceCandidate{
			ID: p.ID, Label: p.DraftPath, Title: title, URL: url,
		})
	}
	return group
}

func (r *Resolver) contentCandidates(ctx context.Context, entityType string) navigationcontract.SourceGroup {
	group := navigationcontract.SourceGroup{Type: entityType, Title: sourceGroupTitleKey(entityType)}
	if r.contents == nil {
		return group
	}
	list, err := r.contents.List(ctx, &contentdto.ListReq{EntityType: entityType, Limit: 200})
	if err != nil {
		return group
	}
	for _, c := range list {
		if c == nil {
			continue
		}
		label := strings.TrimSpace(c.Slug)
		if label == "" {
			label = c.ID
		}
		title := contentTitle(c)
		if title == "" {
			title = label
		}
		group.Items = append(group.Items, navigationcontract.SourceCandidate{
			ID: c.ID, Label: label, Title: title, URL: r.contentURL(ctx, entityType, c.ID),
		})
	}
	return group
}

func (r *Resolver) resolvePage(ctx context.Context, projectID, id string) (title, url string, err error) {
	if r.pages == nil {
		return "", "", nil
	}
	page, err := r.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: id})
	if err != nil || page == nil {
		return "", "", nil
	}
	url = page.DraftPath
	if page.ActivePath != nil && *page.ActivePath != "" {
		url = *page.ActivePath
	}
	return pageDocTitle(page.DraftDocument), url, nil
}

func pageDocTitle(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}
	var v struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v.Settings.SEO.Title)
}

func (r *Resolver) resolveContent(ctx context.Context, entityType, id string) (title, url string, err error) {
	if r.contents == nil {
		return "", "", nil
	}
	entity, err := r.contents.Get(ctx, &contentdto.GetReq{ID: id})
	if err != nil || entity == nil {
		return "", "", nil
	}
	return contentTitle(entity), r.contentURL(ctx, entityType, id), nil
}

func contentTitle(c *contentdto.ContentResp) string {
	for _, key := range []string{"title", "name"} {
		if v, ok := c.Data[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func (r *Resolver) contentURL(ctx context.Context, entityType, id string) string {
	if r.instances == nil {
		return ""
	}
	list, err := r.instances.List(ctx, &presentationdto.ListReq{EntityType: entityType})
	if err != nil {
		return ""
	}
	for _, ins := range list {
		if ins != nil && ins.EntityID == id && ins.URLPath != "" {
			return ins.URLPath
		}
	}
	return ""
}

func (r *Resolver) resolveBlock(ctx context.Context, projectID, id string) (title, url string, err error) {
	if r.blocks == nil {
		return "", "", nil
	}
	block, err := r.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: projectID, ID: id})
	if err != nil || block == nil {
		return "", "", nil
	}
	return strings.TrimSpace(block.Name), "", nil
}
