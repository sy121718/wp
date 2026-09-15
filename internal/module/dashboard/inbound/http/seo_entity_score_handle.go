package dashboardhttp

// seo_entity_score_handle.go — 商品 / 分类 / 品牌页的 SEO 评分接线（审计 SEO-016 / SEO-018）。
//
// 定位与文章编辑页的评分侧栏完全一致：**只读计算** —— 不写库、不写产物、不激活 URL，
// 因此不叠加 Casbin 权限点（页面组已有 Session + CSRF）。评分是提示性的，不拦保存：
// 草稿先存下来是常态。
//
// 端点是 HTMX 局部刷新，返回 fragments/seo_score 片段（与文章页、页面设置面板同一份模板）：
//
//	POST /admin/products/seo-score            → 商品详情页评分（ProductProfile）
//	POST /admin/product-categories/seo-score  → 分类页评分（LandingProfile）
//	POST /admin/product-brands/seo-score      → 品牌页评分（LandingProfile）
//
// 页型 → 档案的选择只发生在 scoring.ProfileFor 一处：本文件不自己挑权重，
// 否则「商品页用哪套权重」会有两个答案，而 SEO-016 的根因正是这种接线缺口。

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	pagecontract "go_wp/internal/module/page/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
)

// entityTypeProduct 发布实例的实体类型标识（与 presentation 的实例登记一致）。
const entityTypeProduct = "product"

// seoTitleIndexSize 标题索引每类实体的取样上限。
//
// 「编辑期轻量版」的重点是点一下就有结果（一次列表查询，不是遍历产物文件）。
// 超过上限时商品一侧由 service 按自己的分页上限收敛 —— 这点在注释里写明，
// 因为「索引不完整」会让冲突清单漏报，而漏报是检查类功能最不能有的失效模式。
const seoTitleIndexSize = 100

// SetSeoTitleSources 注入全站标题索引所需的另外两份只读契约（装配期调用，可空）。
//
// 页面与文章是同一个站点的其它页面，做 title 唯一性时必须在同一份索引里 ——
// 只比商品域的话，商品标题与文章标题撞车照样检不出来。两份契约都可空：
// 未注入时索引退化成商品 / 分类 / 品牌三域，评分本身照常（缺依赖不该变成 500）。
func (h *productPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

// ProductScorePanel 商品详情页的编辑期评分（POST /admin/products/seo-score）。
func (h *productPageHandle) ProductScorePanel(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := strings.TrimSpace(c.PostForm("productId"))
	if id == "" {
		renderScoreEmpty(c, "缺少商品，无法评分")
		return
	}
	detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
	if err != nil || detail == nil {
		renderScoreEmpty(c, "读不到这个商品，无法评分")
		return
	}
	// 线上路径与「产物是否已带 canonical / JSON-LD」是同一个问题的两面：
	// 实例有线上路径 = 详情页真的发布过 = 构建期已注入这两样（presentation 的 applyEntitySEO）。
	instPath := h.instancePath(ctx, entityTypeProduct, detail.ID)
	in := &scoring.EntityPageInput{
		Kind:           scoring.KindProduct,
		Name:           detail.Name,
		Subtitle:       detail.Subtitle,
		Description:    entityPlainText(detail.Description),
		SEOTitle:       detail.SEOTitle,
		SEODescription: detail.SEODescription,
		Slug:           detail.Slug,
		URL:            firstNonEmptyString(instPath, slashSlug(detail.Slug)),
		Images:         productPageImages(detail),
		SpecNames:      variationSpecNames(detail),
		ChildNames:     productCategoryNames(detail),
		Locale:         requestScoreLang(c),
		// 未发布时判假：评分器只该显示编辑者能改的东西，而这两项要发布一次才会出现。
		HasCanonical: instPath != "",
		HasSchema:    instPath != "",
	}
	h.renderEntityScore(c, in, projectID, detail.ID)
}

// ProductCategoryScorePanel 商品分类页的编辑期评分（POST /admin/product-categories/seo-score）。
func (h *productPageHandle) ProductCategoryScorePanel(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		renderScoreEmpty(c, "缺少分类，无法评分")
		return
	}
	node, err := h.products.GetCategory(ctx, &productdto.GetCategoryReq{ID: id})
	if err != nil || node == nil {
		renderScoreEmpty(c, "读不到这个分类，无法评分")
		return
	}
	instPath := h.instancePath(ctx, "product_category", node.ID)
	in := &scoring.EntityPageInput{
		Kind:           scoring.KindCategory,
		Name:           node.Name,
		Description:    node.Description,
		SEOTitle:       node.SEOTitle,
		SEODescription: node.SEODescription,
		Slug:           node.Slug,
		URL:            firstNonEmptyString(instPath, slashSlug(node.Slug)),
		Images:         singleImage(node.Image, "hero"),
		ChildNames:     categoryChildNames(ctx, h, projectID, node.ID),
		Locale:         requestScoreLang(c),
		HasCanonical:   instPath != "",
		HasSchema:      instPath != "",
	}
	h.renderEntityScore(c, in, projectID, node.ID)
}

// ProductBrandScorePanel 品牌页的编辑期评分（POST /admin/product-brands/seo-score）。
func (h *productPageHandle) ProductBrandScorePanel(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		renderScoreEmpty(c, "缺少品牌，无法评分")
		return
	}
	brand, err := h.products.GetBrand(ctx, &productdto.GetBrandReq{ID: id})
	if err != nil || brand == nil {
		renderScoreEmpty(c, "读不到这个品牌，无法评分")
		return
	}
	instPath := h.instancePath(ctx, "product_brand", brand.ID)
	in := &scoring.EntityPageInput{
		Kind:           scoring.KindBrand,
		Name:           brand.Name,
		Description:    brand.Description,
		SEOTitle:       brand.SEOTitle,
		SEODescription: brand.SEODescription,
		Slug:           brand.Slug,
		URL:            firstNonEmptyString(instPath, slashSlug(brand.Slug)),
		Images:         singleImage(brand.Logo, "hero"),
		Locale:         requestScoreLang(c),
		HasCanonical:   instPath != "",
		HasSchema:      instPath != "",
	}
	h.renderEntityScore(c, in, projectID, brand.ID)
}

// renderEntityScore 算分 + 查 title 重复 + 渲染片段（三个入口共用同一段流程）。
func (h *productPageHandle) renderEntityScore(c *gin.Context, in *scoring.EntityPageInput, projectID, selfID string) {
	res := scoring.ScoreEntityPage(in)
	dups := scoring.DuplicateTitles(scoring.EntityTitle(in), h.seoTitleIndex(c.Request.Context(), projectID), selfID)
	c.HTML(http.StatusOK, "fragments/seo_score", gin.H{"Score": entityScoreView(in, res, dups)})
}

// renderScoreEmpty 空态片段（读不到实体时不返回 500：面板显示一句可读的话即可）。
func renderScoreEmpty(c *gin.Context, msg string) {
	c.HTML(http.StatusOK, "fragments/seo_score", gin.H{"Score": scoreView{Empty: msg}})
}

// entityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func entityScoreView(in *scoring.EntityPageInput, res *scoring.Result, dups []scoring.TitleEntry) scoreView {
	sv := scoreView{OK: true}
	if res != nil {
		sv.Total = res.Total
		sv.Grade = res.Grade
		// 页型与调权理由回显（docs/02-E1 §5）：编辑者要能看出「这份分数是按商品页的
		// 尺子量的」，否则同一段描述在商品页比文章页高几分会被当成评分器不稳。
		if res.Profile != nil {
			sv.ProfileType = res.Profile.Type
			sv.ProfileReason = res.Profile.Reason
		}
		for _, sec := range res.Sections {
			item := scoreSectionView{
				Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
				ColorLabel: seoColorLabels[sec.Color],
			}
			for _, ck := range sec.Checks {
				if ck.Score >= ck.Max {
					continue
				}
				item.Issues = append(item.Issues, scoreIssueView{
					Text:   ck.Label + "：" + ck.Actual + "（基准 " + ck.Benchmark + "）→ " + ck.Hint,
					Target: ck.Target,
				})
			}
			sv.Sections = append(sv.Sections, item)
		}
	}
	title := scoring.EntityTitle(in)
	sv.SerpTitle = title
	if sv.SerpTitle == "" {
		sv.SerpTitle = "（未填写标题）"
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpURL = in.URL
	if sv.SerpURL == "" {
		sv.SerpURL = "（线上路径未知）"
	}
	sv.SerpDesc = strings.TrimSpace(in.SEODescription)
	if sv.SerpDesc == "" {
		sv.SerpDesc = strings.TrimSpace(in.Description)
	}
	if sv.SerpDesc == "" {
		sv.SerpDesc = "（未填写描述）"
	}
	// title 唯一性（SEO-018 编辑期轻量版）：结论形状与发布侧体检一致 —— 列出全部冲突页面。
	if len(dups) > 0 {
		sv.DuplicateNote = scoring.DuplicateTitleMessage(title, dups)
		for _, d := range dups {
			sv.Duplicates = append(sv.Duplicates, d.Page)
		}
	}
	return sv
}

// seoTitleIndex 同工程已发布 / 已有内容的标题索引（审计 SEO-018 编辑期轻量版）。
//
// 每类实体各一次列表查询（商品 / 分类 / 品牌 / 页面 / 文章），不遍历产物文件 ——
// 那是发布侧体检（SEO-019）的活，编辑期要的是点一下就能出结果。
//
// 与发布侧对齐的是**结论形状**（列出全部冲突页面），不是数据来源：发布侧读的是真正
// 服务出去的 <title>，这里读的是实体字段（写之前就得能提示）。两者的差异写在
// scoring.DuplicateTitles 的注释里，改任一处都要一起看。
//
// 「已发布」的判据按实体能力各自取：
//   - 商品：status=published（草稿商品还没有线上页面，拿它判重复会让运营被一堆
//     没上架的东西挡住）；
//   - 分类 / 品牌：它们是分类法，没有发布状态，随商品上线 —— 全量参与比对；
//   - 页面：草稿文档里看不出发布与否，按全部页面参与（宁可多提示一次，
//     也不要漏掉一个即将上线的重复标题）。
func (h *productPageHandle) seoTitleIndex(ctx context.Context, projectID string) []scoring.TitleEntry {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var out []scoring.TitleEntry
	if list, err := h.products.List(ctx, &productdto.ListReq{
		ProjectID: projectID, Status: productenums.StatusPublished, Size: seoTitleIndexSize,
	}); err == nil {
		for _, p := range list {
			if p == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: scoring.PreferredTitle(p.SEOTitle, p.Name),
				Page:  slashSlug(p.Slug),
				ID:    p.ID,
			})
		}
	}
	if cats, err := h.flatCategories(ctx, projectID); err == nil {
		for _, cat := range cats {
			if cat == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: scoring.PreferredTitle(cat.SEOTitle, cat.Name),
				Page:  slashSlug(cat.Slug),
				ID:    cat.ID,
			})
		}
	}
	if brands, err := h.listBrands(ctx, projectID); err == nil {
		for _, b := range brands {
			if b == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: scoring.PreferredTitle(b.SEOTitle, b.Name),
				Page:  slashSlug(b.Slug),
				ID:    b.ID,
			})
		}
	}
	// 页面与文章（可空注入）：两者都是「同一个站点的其它页面」，缺了它们
	// 跨内容的重复标题检不出来 —— 但缺依赖不该让评分不可用，故按可选处理。
	if h.seoPages != nil {
		if drafts, err := h.seoPages.ListDrafts(ctx); err == nil {
			for _, d := range drafts {
				if d.ProjectID != projectID {
					continue
				}
				if title := pageDraftTitle(d.DraftDocument); title != "" {
					out = append(out, scoring.TitleEntry{Title: title, Page: slashSlug(d.DraftPath), ID: d.ID})
				}
			}
		}
	}
	if h.seoContents != nil {
		if rows, err := h.seoContents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: seoTitleIndexSize}); err == nil {
			for _, r := range rows {
				if r == nil || r.Data == nil {
					continue
				}
				title := scoring.PreferredTitle(contentText(r.Data, "seoTitle"), contentText(r.Data, "title"))
				if title == "" {
					continue
				}
				// contents 列表接口没有工程过滤参数，标题索引据此标注来源：
				// 多工程部署时别的工程的文章也会进这份索引，宁可报一次重复让人核对，
				// 也好过因为查不到而假报「没有重复」。
				out = append(out, scoring.TitleEntry{Title: title, Page: "文章 " + r.Slug, ID: r.ID})
			}
		}
	}
	return out
}

// instancePath 取实体详情页实例的线上路径（未注入实例契约 / 未发布 → 空串）。
func (h *productPageHandle) instancePath(ctx context.Context, entityType, entityID string) string {
	if h.instances == nil || strings.TrimSpace(entityID) == "" {
		return ""
	}
	inst, err := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: entityType, EntityID: entityID,
	})
	if err != nil || inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.URLPath)
}

// pageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
func pageDraftTitle(doc json.RawMessage) string {
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

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// entityPlainText 商品描述（jsonb）→ 纯文本。
//
// 两种形态与构建期一致（{"html": "..."} 富文本 / JSON 字符串纯文本），
// 去标签的理由与文章侧相同：把 <p> 当正文算进字数，一篇 300 字的描述会被算成 900。
func entityPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return stripEntityTags(s)
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return stripEntityTags(asString)
	}
	return ""
}

var entityTagRe = regexp.MustCompile("<[^>]+>")

// stripEntityTags 去 HTML 标签并压掉多余空白（只读展示与统计用，不承担清洗职责）。
func stripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

// productPageImages 商品图集 → 评分图片项（首图按 hero、其余按 content，
// alt 与 Images 逐位对应 —— 与构建期 imageAltsJSON 同一份数据）。
func productPageImages(p *productdto.ProductResp) []scoring.Image {
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

// variationSpecNames 参与变体的属性组名（详情页把它们渲染成分组小标题）。
func variationSpecNames(p *productdto.ProductResp) []string {
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

// productCategoryNames 商品挂载的分类名（详情页的关联分区标题）。
func productCategoryNames(p *productdto.ProductResp) []string {
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

// categoryChildNames 某分类的子分类名（列表页把子分类渲染成小节）。
//
// 取不到（分类不存在 / 列表失败）返回 nil：少一个小标题只会让标题结构项如实报缺，
// 不会让评分整体失败。
func categoryChildNames(ctx context.Context, h *productPageHandle, projectID, id string) []string {
	cats, err := h.flatCategories(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, c := range cats {
		if c != nil && c.ParentID == id && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}

// firstNonEmptyString 取第一个非空值。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
