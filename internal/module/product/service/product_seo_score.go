package productservice

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	contentcontract "go_wp/internal/module/content/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/seo/scoring"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
)

// ProductReadPort 商品域的最小只读面，评分用例没有写入能力。
type ProductReadPort interface {
	Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error)
	List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error)
	GetCategory(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error)
	ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error)
	GetBrand(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error)
	ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error)
}

// SeoInstancePathReader 未注入表示尚未接入发布实例；已注入时读取错误必须传播。
type SeoInstancePathReader interface {
	GetByEntity(context.Context, *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error)
}

// SeoTitleExtraSources 可选跨模块读契约；未覆盖来源随结果明示。
type SeoTitleExtraSources struct {
	Pages    pagecontract.PageService
	Contents contentcontract.ContentService
}

// seoTitlePageSize 是逐页读取的批量大小，不是索引条数上限。
const seoTitlePageSize = 100

// SeoScoreInputResult 构造共享算法输入，所有失败都通过 Err 向上返回。
type SeoScoreInputResult struct {
	Input  *scoring.EntityPageInput
	SelfID string
	Err    error
}

// SeoScore 将读取、评分、同工程标题冲突检查组成一个用例，HTTP 不派生评分形状。
func SeoScore(ctx context.Context, r ProductReadPort, insts SeoInstancePathReader, extra SeoTitleExtraSources,
	kind scoring.PageKind, req *productdto.ScorePanelReq, lang string) (*productdto.SeoScoreResp, error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	input := SeoScoreInputOf(ctx, r, insts, kind, req.ID, projectID, lang)
	if input.Err != nil {
		return nil, input.Err
	}
	index, coverage, err := SeoTitleIndex(ctx, r, extra, projectID)
	if err != nil {
		return nil, err
	}
	score := scoring.ScoreEntityPage(input.Input)
	out := &productdto.SeoScoreResp{
		EntityType: string(kind), EntityID: input.SelfID, ProjectID: projectID,
		Total: score.Total, Title: scoring.EntityTitle(input.Input),
		Description: firstNonEmpty(input.Input.SEODescription, input.Input.Description),
		URL:         input.Input.URL, Coverage: coverage,
		Sections:   make([]productdto.SeoScoreSectionResp, 0, len(score.Sections)),
		Duplicates: []productdto.SeoTitleDuplicateResp{},
	}
	if score.Profile != nil {
		out.ProfileType = score.Profile.Type
	}
	for _, section := range score.Sections {
		fact := productdto.SeoScoreSectionResp{Key: section.Key, Score: section.Score, Max: section.Max,
			Weight: section.Weight, Percent: section.Percent, Checks: make([]productdto.SeoScoreCheckResp, 0, len(section.Checks))}
		for _, check := range section.Checks {
			fact.Checks = append(fact.Checks, productdto.SeoScoreCheckResp{Key: check.Key, Score: check.Score, Max: check.Max, Skipped: check.Skipped})
		}
		out.Sections = append(out.Sections, fact)
	}
	for _, duplicate := range scoring.DuplicateTitles(out.Title, index, input.SelfID) {
		out.Duplicates = append(out.Duplicates, productdto.SeoTitleDuplicateResp{ID: duplicate.ID, Kind: duplicate.Kind, Title: duplicate.Title, Path: duplicate.Page})
	}
	return out, nil
}

// SeoScoreInputOf 只从指定工程读取实体与素材，再交给共享评分算法。
func SeoScoreInputOf(ctx context.Context, r ProductReadPort, insts SeoInstancePathReader,
	kind scoring.PageKind, id, projectID, lang string) SeoScoreInputResult {
	id, projectID = strings.TrimSpace(id), strings.TrimSpace(projectID)
	if id == "" || projectID == "" {
		return SeoScoreInputResult{Err: productenums.Err(productenums.ErrInvalidParam)}
	}
	if r == nil {
		return SeoScoreInputResult{Err: errors.New("product SEO reader is unavailable")}
	}
	var in *scoring.EntityPageInput
	var entityType string
	switch kind {
	case scoring.KindCategory:
		node, err := r.GetCategory(ctx, &productdto.GetCategoryReq{ID: id, ProjectID: projectID})
		if err != nil {
			return SeoScoreInputResult{Err: err}
		}
		if node == nil || node.ProjectID != projectID {
			return SeoScoreInputResult{Err: productenums.Err(productenums.ErrCategoryNotFound)}
		}
		children, err := CategoryChildNames(ctx, r, projectID, node.ID)
		if err != nil {
			return SeoScoreInputResult{Err: err}
		}
		entityType = productcontract.EntityTypeCategory
		in = &scoring.EntityPageInput{Kind: kind, Name: node.Name, Description: StripEntityTags(node.Description),
			SEOTitle: node.Name, SEODescription: StripEntityTags(node.Description), Slug: node.Slug,
			Images: singleImage(node.Image, "hero"), ChildNames: children, Locale: lang}
	case scoring.KindBrand:
		brand, err := r.GetBrand(ctx, &productdto.GetBrandReq{ID: id, ProjectID: projectID})
		if err != nil {
			return SeoScoreInputResult{Err: err}
		}
		if brand == nil || brand.ProjectID != projectID {
			return SeoScoreInputResult{Err: productenums.Err(productenums.ErrBrandNotFound)}
		}
		entityType = productcontract.EntityTypeBrand
		in = &scoring.EntityPageInput{Kind: kind, Name: brand.Name, Description: StripEntityTags(brand.Description),
			SEOTitle: brand.Name, SEODescription: StripEntityTags(brand.Description), Slug: brand.Slug,
			Images: singleImage(brand.Logo, "hero"), Locale: lang}
	case scoring.KindProduct:
		detail, err := r.Get(ctx, &productdto.GetReq{ID: id, ProjectID: projectID})
		if err != nil {
			return SeoScoreInputResult{Err: err}
		}
		if detail == nil || detail.ProjectID != projectID {
			return SeoScoreInputResult{Err: productenums.Err(productenums.ErrNotFound)}
		}
		entityType = productcontract.EntityTypeProduct
		in = &scoring.EntityPageInput{Kind: kind, Name: detail.Name, Subtitle: detail.Subtitle,
			Description: EntityPlainText(detail.Description), SEOTitle: detail.Name, SEODescription: detail.Subtitle,
			Slug: detail.Slug, Images: ProductPageImages(detail), SpecNames: VariationSpecNames(detail),
			ChildNames: ProductCategoryNames(detail), Locale: lang}
	default:
		return SeoScoreInputResult{Err: productenums.Err(productenums.ErrInvalidParam)}
	}
	path, published, err := seoInstancePublicationOf(ctx, insts, projectID, entityType, id)
	if err != nil {
		return SeoScoreInputResult{Err: err}
	}
	in.URL = firstNonEmpty(path, slashSlug(in.Slug))
	in.HasCanonical, in.HasSchema = published, published
	return SeoScoreInputResult{Input: in, SelfID: id}
}

// seoInstancePublicationOf 将预定路径与真实激活事实分开：建实例后发布失败仍会留下 URLPath。
// 未绑定实例是正常事实，其它读取失败必须传播；激活依据契约的逐语言 Publications。
func seoInstancePublicationOf(ctx context.Context, insts SeoInstancePathReader, projectID, entityType, entityID string) (string, bool, error) {
	if insts == nil {
		return "", false, nil
	}
	inst, err := insts.GetByEntity(ctx, &presentationcontract.GetByEntityReq{ProjectID: projectID, EntityType: entityType, EntityID: entityID})
	if err != nil {
		if status, ok := facing.StatusOf(err); ok && status == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	if inst == nil {
		return "", false, nil
	}
	if inst.ProjectID != "" && inst.ProjectID != projectID {
		return "", false, productenums.Err(productenums.ErrNotFound)
	}
	published := false
	for _, publication := range inst.Publications {
		if strings.TrimSpace(publication.ActivePath) != "" && strings.TrimSpace(publication.ArtifactHash) != "" {
			published = true
			break
		}
	}
	return strings.TrimSpace(inst.URLPath), published, nil
}

// SeoTitleIndex 完整翻页读取商品域，并按工程过滤可选页面源。
// 文章契约未提供工程过滤，所以明确标注未覆盖，不把其它工程的标题混进结果。
func SeoTitleIndex(ctx context.Context, r ProductReadPort, extra SeoTitleExtraSources,
	projectID string) ([]scoring.TitleEntry, productdto.SeoTitleCoverageResp, error) {
	coverage := productdto.SeoTitleCoverageResp{Included: []string{"product", "category", "brand"}, Omitted: []productdto.SeoTitleOmissionResp{}}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, coverage, productenums.Err(productenums.ErrInvalidParam)
	}
	out := []scoring.TitleEntry{}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, coverage, err
		}
		rows, err := r.List(ctx, &productdto.ListReq{ProjectID: projectID, Status: productenums.StatusPublished, Page: page, Size: seoTitlePageSize})
		if err != nil {
			return nil, coverage, err
		}
		for _, row := range rows {
			if row != nil && row.ProjectID == projectID {
				out = append(out, scoring.TitleEntry{ID: row.ID, Title: row.Name, Kind: "product", Page: slashSlug(row.Slug)})
			}
		}
		if len(rows) < seoTitlePageSize {
			break
		}
	}
	cats, err := flatCategoriesOf(ctx, r, projectID)
	if err != nil {
		return nil, coverage, err
	}
	for _, cat := range cats {
		if cat != nil && cat.ProjectID == projectID {
			out = append(out, scoring.TitleEntry{ID: cat.ID, Title: cat.Name, Kind: "category", Page: slashSlug(cat.Slug)})
		}
	}
	brands, err := r.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID})
	if err != nil {
		return nil, coverage, err
	}
	for _, brand := range brands {
		if brand != nil && brand.ProjectID == projectID {
			out = append(out, scoring.TitleEntry{ID: brand.ID, Title: brand.Name, Kind: "brand", Page: slashSlug(brand.Slug)})
		}
	}
	if extra.Pages == nil {
		coverage.Omitted = append(coverage.Omitted, productdto.SeoTitleOmissionResp{Source: "page", Reason: "not_configured"})
	} else {
		drafts, err := extra.Pages.ListDrafts(ctx)
		if err != nil {
			return nil, coverage, err
		}
		for _, draft := range drafts {
			if draft.ProjectID != projectID {
				continue
			}
			if title := PageDraftTitle(draft.DraftDocument); title != "" {
				out = append(out, scoring.TitleEntry{ID: draft.ID, Title: title, Kind: "page", Page: slashSlug(draft.DraftPath)})
			}
		}
		coverage.Included = append(coverage.Included, "page")
	}
	reason := "not_configured"
	if extra.Contents != nil {
		reason = "project_filter_unavailable"
	}
	coverage.Omitted = append(coverage.Omitted, productdto.SeoTitleOmissionResp{Source: "article", Reason: reason})
	return out, coverage, nil
}

// flatCategoriesOf 取某工程的分类树并摊平成 DFS 前序列表（工程为空时返回空列表）。
//
// 口径：同一个 ListCategories 契约 + 同一份 FlattenCategoryTree。**本函数是唯一实现** ——
// 原先 inbound/http 的 flatCategories 是同一口径的第二份，已随迁移删除（task-163）。
func flatCategoriesOf(ctx context.Context, r ProductReadPort, projectID string) (out []*productdto.CategoryResp, err error) {
	out = []*productdto.CategoryResp{}
	if projectID == "" {
		return out, nil
	}
	tree, err := r.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return FlattenCategoryTree(tree), nil
}

// PageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
func PageDraftTitle(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}
	var parsed struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title)
}

// EntityPlainText 商品描述（jsonb）→ 纯文本。
//
// 两种形态与构建期一致（{"html": "..."} 富文本 / JSON 字符串纯文本），
// 去标签的理由与文章侧相同：把 <p> 当正文算进字数，一篇 300 字的描述会被算成 900。
func EntityPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return StripEntityTags(s)
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return StripEntityTags(asString)
	}
	return ""
}

var entityTagRe = regexp.MustCompile("<[^>]+>")

// StripEntityTags 去 HTML 标签并压掉多余空白（只读展示与统计用，不承担清洗职责）。
func StripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

// ProductPageImages 商品图集 → 评分图片项（首图按 hero、其余按 content，
// alt 与 Images 逐位对应 —— 与构建期 imageAltsJSON 同一份数据）。
func ProductPageImages(p *productdto.ProductResp) []scoring.Image {
	if p == nil {
		return nil
	}
	out := make([]scoring.Image, 0, len(p.Images))
	for i, src := range p.Images {
		if strings.TrimSpace(src) == "" {
			continue
		}
		kind := "content"
		if i == 0 {
			kind = "hero"
		}
		alt := ""
		if i < len(p.ImageAlts) {
			alt = strings.TrimSpace(p.ImageAlts[i])
		}
		out = append(out, scoring.Image{Src: strings.TrimSpace(src), Alt: alt, Kind: kind})
	}
	return out
}

// singleImage 单图实体（分类图 / 品牌 logo）→ 评分图片项。
func singleImage(src, kind string) []scoring.Image {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	return []scoring.Image{{Src: strings.TrimSpace(src), Kind: kind}}
}

// VariationSpecNames 参与变体的属性组名（详情页把它们渲染成分组小标题）。
func VariationSpecNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Attributes))
	for _, a := range p.Attributes {
		if a != nil && a.IsVariation && strings.TrimSpace(a.Name) != "" {
			out = append(out, strings.TrimSpace(a.Name))
		}
	}
	return out
}

// ProductCategoryNames 商品挂载的分类名（详情页的关联分区标题）。
func ProductCategoryNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Categories))
	for _, c := range p.Categories {
		if c != nil && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// CategoryChildNames 返回直接子分类名；读取失败不能变成缺少标题的评分事实。
func CategoryChildNames(ctx context.Context, r ProductReadPort, projectID, id string) ([]string, error) {
	cats, err := flatCategoriesOf(ctx, r, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 2)
	for _, c := range cats {
		if c != nil && c.ParentID == id && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out, nil
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。**唯一实现**（原先 inbound/http 的同名函数已随迁移删除，task-163）。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}
