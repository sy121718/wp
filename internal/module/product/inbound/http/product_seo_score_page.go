package producthttp

import (
	"context"
	"net/http"
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
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
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
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindProduct,
		strings.TrimSpace(c.PostForm("productId")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductCategoryScorePanel 商品分类页的编辑期评分（POST /admin/product-categories/seo-score）。
func (h *productPageHandle) ProductCategoryScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindCategory,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductBrandScorePanel 品牌页的编辑期评分（POST /admin/product-brands/seo-score）。
func (h *productPageHandle) ProductBrandScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindBrand,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// entityScoreViewOf 算分 + 查 title 重复 → 片段视图（三个 POST 端点与抽屉共用同一段流程）。
func (h *productPageHandle) entityScoreViewOf(ctx context.Context, c *gin.Context,
	in *scoring.EntityPageInput, projectID, selfID string) scoreView {
	res := scoring.ScoreEntityPage(in)
	tr := shell.TranslateFor(c)
	dups := scoring.DuplicateTitles(scoring.EntityTitle(in), h.seoTitleIndex(ctx, projectID, tr), selfID)
	return entityScoreView(tr, in, res, dups)
}

// renderEntityScore 算分 + 查 title 重复 + 渲染片段（三个 POST 入口共用）。
func (h *productPageHandle) renderEntityScore(c *gin.Context, in *scoring.EntityPageInput, projectID, selfID string) {
	tr := shell.TranslateFor(c)
	// t 是片段模板的取词函数：seo_score 片段不经 shell.Prepare，缺 t 时 Jet 把取词调用
	// 求值成空串（不报错、不 500、不记日志）—— 整片提示会变成空白。
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID), "t": tr})
}

// renderScoreView 渲染评分片段（空态与结果共用同一份模板：片段按 Score.OK 分流）。
func renderScoreView(c *gin.Context, sv scoreView) {
	tr := shell.TranslateFor(c)
	c.HTML(http.StatusOK, "fragments/seo_score", gin.H{"Score": sv, "t": tr})
}

// entityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func entityScoreView(tr func(key, fallback string) string, in *scoring.EntityPageInput, res *scoring.Result,
	dups []scoring.TitleEntry) scoreView {
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
		// 未达标行的整句模板：标签 / 实测 / 基准 / 建议四段都是数据，句子的语序与标点
		// 由词条决定（英文的冒号与括号与中文不同），命名占位符 + FillTranslate 负责填充
		//（词条被写坏时自动回落下面那句中文兜底）。
		for _, sec := range res.Sections {
			item := scoreSectionView{
				Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
				ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
			}
			for _, ck := range sec.Checks {
				if ck.Score >= ck.Max {
					continue
				}
				item.Issues = append(item.Issues, scoreIssueView{
					Text: i18n.FillTranslate(tr, productenums.SEOIssueLine,
						"{label}：{actual}（基准 {benchmark}）→ {hint}",
						map[string]string{
							"label": ck.Label, "actual": ck.Actual,
							"benchmark": ck.Benchmark, "hint": ck.Hint,
						}),
					Target: ck.Target,
				})
			}
			sv.Sections = append(sv.Sections, item)
		}
	}
	title := scoring.EntityTitle(in)
	sv.SerpTitle = title
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(productenums.SEOSerpTitleEmpty, "（未填写标题）")
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpURL = in.URL
	if sv.SerpURL == "" {
		sv.SerpURL = tr(productenums.SEOSerpURLEmpty, "（线上路径未知）")
	}
	sv.SerpDesc = strings.TrimSpace(in.SEODescription)
	if sv.SerpDesc == "" {
		sv.SerpDesc = strings.TrimSpace(in.Description)
	}
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(productenums.SEOSerpDescEmpty, "（未填写描述）")
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
func (h *productPageHandle) seoTitleIndex(ctx context.Context, projectID string,
	tr func(key, fallback string) string) []scoring.TitleEntry {
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
				// 标题就是商品名（2026-09-30 合并后口径）：唯一性检查必须拿**会发布出去
				// 的那个标题**比对，再读 seo_title 只会拿库里的残留旧值去比。
				Title: p.Name,
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
				Title: cat.Name,
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
				Title: b.Name,
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
				// 文章的 SEO 标题即标题（2026-09-30 合并）：不再优先读 seoTitle。
				title := contentText(r.Data, "title")
				if title == "" {
					continue
				}
				// contents 列表接口没有工程过滤参数，标题索引据此标注来源：
				// 多工程部署时别的工程的文章也会进这份索引，宁可报一次重复让人核对，
				// 也好过因为查不到而假报「没有重复」。
				out = append(out, scoring.TitleEntry{
					Title: title,
					Page: i18n.FillTranslate(tr, productenums.SEOIndexArticle, "文章 {slug}",
						map[string]string{"slug": r.Slug}),
					ID: r.ID,
				})
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

// 评分实体类型（取数分派共用同一组取值）。
const (
	scoreKindProduct  = "product"
	scoreKindCategory = "category"
	scoreKindBrand    = "brand"
)

// entityScoreInputOf 按实体类型取数并构造评分输入。
//
// 三个 POST 端点共用这一份：同一个实体不该有两种取数口径（抽屉与行内按钮各写一份，
// 下场是「同一个商品在两处分数不同」）。
//
// 第三个返回值是**空态视图**（OK=false，Empty 已取好词）：非 OK 时调用方直接渲染它 ——
// 三个端点因此共用同一条失败路径，不必各自拼一句文案。
func (h *productPageHandle) entityScoreInputOf(ctx context.Context, c *gin.Context,
	kind, id, projectID string) (in *scoring.EntityPageInput, selfID string, emptyView scoreView) {
	tr := shell.TranslateFor(c)
	emptyOf := func(key, fallback string) scoreView { return scoreView{Empty: tr(key, fallback)} }

	if id == "" {
		switch kind {
		case scoreKindCategory:
			return nil, "", emptyOf(productenums.SEOEmptyMissingCategory, "缺少分类，无法评分")
		case scoreKindBrand:
			return nil, "", emptyOf(productenums.SEOEmptyMissingBrand, "缺少品牌，无法评分")
		default:
			return nil, "", emptyOf(productenums.SEOEmptyMissingProduct, "缺少商品，无法评分")
		}
	}

	switch kind {
	case scoreKindCategory:
		node, err := h.products.GetCategory(ctx, &productdto.GetCategoryReq{ID: id})
		if err != nil || node == nil {
			return nil, "", emptyOf(productenums.SEOEmptyCategoryUnreadable, "读不到这个分类，无法评分")
		}
		// SEO 标题 / 描述与「分类名 / 分类描述」合并（2026-09-30，与商品同口径）：
		// 分类名即 <title>、分类描述即 meta description。描述是富文本，先去掉标签
		// 再喂给评分器 —— 否则 <p> 会被算进 meta 描述长度。
		instPath := h.instancePath(ctx, "product_category", node.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindCategory,
			Name:           node.Name,
			Description:    stripEntityTags(node.Description),
			SEOTitle:       node.Name,
			SEODescription: stripEntityTags(node.Description),
			Slug:           node.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(node.Slug)),
			Images:         singleImage(node.Image, "hero"),
			ChildNames:     categoryChildNames(ctx, h, projectID, node.ID),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, node.ID, scoreView{}

	case scoreKindBrand:
		brand, err := h.products.GetBrand(ctx, &productdto.GetBrandReq{ID: id})
		if err != nil || brand == nil {
			return nil, "", emptyOf(productenums.SEOEmptyBrandUnreadable, "读不到这个品牌，无法评分")
		}
		// 同分类：品牌名即 <title>、品牌描述即 meta description（2026-09-30 合并）。
		instPath := h.instancePath(ctx, "product_brand", brand.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindBrand,
			Name:           brand.Name,
			Description:    stripEntityTags(brand.Description),
			SEOTitle:       brand.Name,
			SEODescription: stripEntityTags(brand.Description),
			Slug:           brand.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(brand.Slug)),
			Images:         singleImage(brand.Logo, "hero"),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, brand.ID, scoreView{}

	default:
		detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
		if err != nil || detail == nil {
			return nil, "", emptyOf(productenums.SEOEmptyProductUnreadable, "读不到这个商品，无法评分")
		}
		// 线上路径与「产物是否已带 canonical / JSON-LD」是同一个问题的两面：
		// 实例有线上路径 = 详情页真的发布过 = 构建期已注入这两样（presentation 的 applyEntitySEO）。
		instPath := h.instancePath(ctx, entityTypeProduct, detail.ID)
		return &scoring.EntityPageInput{
			Kind:        scoring.KindProduct,
			Name:        detail.Name,
			Subtitle:    detail.Subtitle,
			Description: entityPlainText(detail.Description),
			// 评分与前台对齐（2026-09-30 合并）：商品名即 <title>、副标题即 meta description。
			// 若这里仍读 seo_title/seo_description，评分看到的标题会和实际发布出去的**不是同一个**——
			// 编辑改商品名、分数不动，是最难解释的一种不一致。
			SEOTitle:       detail.Name,
			SEODescription: detail.Subtitle,
			Slug:           detail.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(detail.Slug)),
			Images:         productPageImages(detail),
			SpecNames:      variationSpecNames(detail),
			ChildNames:     productCategoryNames(detail),
			Locale:         requestScoreLang(c),
			// 未发布时判假：评分器只该显示编辑者能改的东西，而这两项要发布一次才会出现。
			HasCanonical: instPath != "",
			HasSchema:    instPath != "",
		}, detail.ID, scoreView{}
	}
}

// EntitySeoDrawer 商品 SEO 评分抽屉片段（GET /admin/products/seo/drawer?productId=&project=）。
//
// 为什么只有商品走抽屉：抽屉基座（ui/drawer.js）是**单例** —— 打开新抽屉会替换当前那个。
// 而分类 / 品牌的评分按钮活在**行内编辑抽屉**里（列表页的编辑模板 include 了表单片段），
// 把那里改成跳转式抽屉，用户在分类编辑到一半点「SEO 评分」，正在填的表单会被顶掉且无路返回。
// 那两处保持原有的「htmx 就地展开分数」形态 —— 它们本来就不占常驻版面，没有要修的问题。
//
// 商品编辑页是**独立整页**，不存在这个冲突，且它原来有一张常驻折叠卡占着版面底部。
//
// 打开即算：抽屉本来就是「我要看分」这个动作的落点，不该再要用户先点一次「开始评分」
// （行内按钮需要那一步，是因为它没有「打开」这个动作）。
func (h *productPageHandle) EntitySeoDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if h == nil || h.products == nil {
		c.Status(http.StatusNotFound)
		return
	}
	data := gin.H{
		"ScoreURL":     "/admin/products/seo-score",
		"TargetID":     "product-seo-score-" + id,
		"HiddenFields": productScoreHiddenFields(id, projectID),
	}
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindProduct, id, projectID)
	if in == nil {
		// 读不到实体：仍给 200 + 片段（里面就是那句空态文案）—— 这不是「加载失败」，
		// 抽屉的失败重试界面会把「这个商品读不到」伪装成网络问题。
		data["Score"] = emptyView
		c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", shell.Prepare(c, data))
		return
	}
	data["Score"] = h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)
	c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", shell.Prepare(c, data))
}

// productScoreHiddenFields 商品评分端点的隐藏字段（与页面上那个 POST 端点同一口径）。
func productScoreHiddenFields(id, projectID string) []seoHiddenField {
	return []seoHiddenField{{"projectId", projectID}, {"productId", id}}
}

// seoHiddenField 抽屉里「评分」表单的一个隐藏字段。
type seoHiddenField struct{ Name, Value string }
