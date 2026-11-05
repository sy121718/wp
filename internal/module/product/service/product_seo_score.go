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
	"go_wp/pkg/seo/scoring"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
)

type ProductReadPort interface {
	Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error)
	List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error)
	GetCategory(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error)
	ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error)
	GetBrand(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error)
	ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error)
}

type SeoInstancePathReader interface {
	GetByEntity(context.Context, *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error)
}

type SeoTitleExtraSources struct {
	Pages    pagecontract.PageService
	Contents contentcontract.ContentService
}

const seoTitlePageSize = 100

type SeoScoreInputResult struct {
	Input  *scoring.EntityPageInput
	SelfID string
	Err    error
}

// SeoScore 只读汇总实体评分及同工程重复标题事实。
func SeoScore(ctx context.Context, r ProductReadPort, insts SeoInstancePathReader, extra SeoTitleExtraSources,
	kind scoring.PageKind, req *productdto.ScorePanelReq, lang string) (*SeoScoreFacts, error) {
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
	out := &SeoScoreFacts{
		EntityType: string(kind), EntityID: input.SelfID, ProjectID: projectID,
		Total: score.Total, Title: scoring.EntityTitle(input.Input),
		Description: firstNonEmpty(input.Input.SEODescription, input.Input.Description),
		URL:         input.Input.URL, Coverage: coverage,
		Sections:   make([]SeoScoreSection, 0, len(score.Sections)),
		Duplicates: []SeoTitleDuplicate{},
	}
	if score.Profile != nil {
		out.ProfileType = score.Profile.Type
	}
	for _, section := range score.Sections {
		fact := SeoScoreSection{Key: section.Key, Score: section.Score, Max: section.Max,
			Weight: section.Weight, Percent: section.Percent, Checks: make([]SeoScoreCheck, 0, len(section.Checks))}
		for _, check := range section.Checks {
			fact.Checks = append(fact.Checks, SeoScoreCheck{Key: check.Key, Score: check.Score, Max: check.Max, Skipped: check.Skipped})
		}
		out.Sections = append(out.Sections, fact)
	}
	for _, duplicate := range scoring.DuplicateTitles(out.Title, index, input.SelfID) {
		out.Duplicates = append(out.Duplicates, SeoTitleDuplicate{ID: duplicate.ID, Kind: duplicate.Kind, Title: duplicate.Title, Path: duplicate.Page})
	}
	return out, nil
}

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

// SeoTitleIndex 收集同工程标题并明确记录未覆盖的来源。
func SeoTitleIndex(ctx context.Context, r ProductReadPort, extra SeoTitleExtraSources,
	projectID string) ([]scoring.TitleEntry, SeoTitleCoverage, error) {
	coverage := SeoTitleCoverage{Included: []string{"product", "category", "brand"}, Omitted: []SeoTitleOmission{}}
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
		coverage.Omitted = append(coverage.Omitted, SeoTitleOmission{Source: "page", Reason: "not_configured"})
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
	coverage.Omitted = append(coverage.Omitted, SeoTitleOmission{Source: "article", Reason: reason})
	return out, coverage, nil
}

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

func StripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

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

func singleImage(src, kind string) []scoring.Image {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	return []scoring.Image{{Src: strings.TrimSpace(src), Kind: kind}}
}

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

func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}
