// Package source 把 page/content/presentation/block 契约适配为 navigation 的
// SourceResolver：菜单项来源非 custom 时，构建期与管理页按来源实体解析标题与 URL。
//
// 位置说明：跨模块适配属于 outbound（模块规范），navigation/service 只依赖
// contract 定义的 SourceResolver 接口，装配由顶层 routes.go 在依赖模块就绪后注入。
package source

import (
	"context"
	"encoding/json"
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

// Resolver 来源实体解析器。
type Resolver struct {
	pages     pagecontract.PageService
	contents  contentcontract.ContentService
	instances presentationcontract.PresentationService
	blocks    blockcontract.BlockService
}

// New 构造（各契约可为 nil：对应来源类型退化为「解析不到」）。
func New(pages pagecontract.PageService, contents contentcontract.ContentService,
	instances presentationcontract.PresentationService, blocks blockcontract.BlockService) *Resolver {
	return &Resolver{pages: pages, contents: contents, instances: instances, blocks: blocks}
}

// 编译期断言。
var _ navigationcontract.SourceResolver = (*Resolver)(nil)

// ResolveSource 按来源类型 + 实体 ID 解析标题与 URL（解析不到返回空串，由调用方回退）。
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

// Candidates 列出该工程可加入菜单的来源实体（页面/文章/产品/分类/全局块）。
// 单个来源依赖不可用时该分组为空，不影响其余分组。
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
	// 全局块不进候选：块是内容片段、没有公开 URL，加入菜单只会得到一个空链接。
	// 来源类型 block 仍被 ResolveSource 支持（历史数据/API 直建），标题取块名。
	return out, nil
}

// sourceGroupTitleKey 来源分组的 i18n key。
//
// 本包是 outbound 适配器，**不在这一层拼文案**：它没有请求语言，写出中文就等于把
// 一种语言焊进契约数据。contract 的 SourceGroup.Title 因此只承载 key，
// 由展示层（navigation/inbound/http 的 navSourceGroupTitle）取词 + 兜底。
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
		// 未知来源类型：回显类型名（空标题更难查），它是枚举值、不含文案。
		return strings.TrimSpace(entityType)
	}
}

// pageCandidates 页面候选（按工程过滤；label 用页面路径、title 用文档 SEO 标题）。
//
// 取标题走 page 的**标题投影** ListPageTitles 而不是 List：List 为列表页服务，
// 走 model.ListAll 并刻意 omit 掉 draft_document 大字段，这里再解析文档只会拿到空 JSON，
// 标题静默回退成路径 —— 抽屉里 13 个候选全显示 /about、/blog，用户想挑「加哪个页面进菜单」
// 却只能靠猜路径（2026-09 修复）。标题在 SQL 侧取出，整份 JSONB 不进 Go。
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

// contentCandidates 内容候选（文章/产品/分类；label 用 slug，URL 取自动发布实例路径）。
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

// resolvePage 页面来源：URL 取激活路径（未发布则草稿路径），标题取文档 settings.seo.title。
func (r *Resolver) resolvePage(ctx context.Context, projectID, id string) (title, url string, err error) {
	if r.pages == nil {
		return "", "", nil
	}
	page, err := r.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: id})
	if err != nil || page == nil {
		return "", "", nil // 页面已删除/不可读：回退记录自身值
	}
	url = page.DraftPath
	if page.ActivePath != nil && *page.ActivePath != "" {
		url = *page.ActivePath
	}
	return pageDocTitle(page.DraftDocument), url, nil
}

// pageDocTitle 取页面文档 settings.seo.title（空 = 调用方回退）。
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

// resolveContent 内容来源（文章/产品/分类）：标题取实体字段，URL 取自动发布实例路径。
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

// contentTitle 按内容类型取展示标题（article=title，product/category=name）。
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

// contentURL 取内容实体的公开路径（presentation 自动发布实例 URLPath）。
// 未建实例（尚未发布）时返回空串，调用方回退记录自身 path。
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

// resolveBlock 块来源：块是内容片段、没有公开 URL，只解析标题（链接沿用记录自身 path）。
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
