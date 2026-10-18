package producthttp

// product_score.go — 商品 / 分类 / 品牌的 SEO 评分片段（行内面板 + 抽屉）。
//
// 它原先住在 product_detail_template.go 的后半段：两者没有业务关系（模板是详情页的双轨写入，
// 评分是三个实体的标题 / 描述质量），只是历史上先后加进同一个文件。
//
// 取数与算分都在 productservice（product_seo_score.go 的 SeoScoreInputOf / SeoTitleIndex /
// SeoEntityScore）—— 三个 POST 端点与抽屉共用同一份口径，本文件只剩请求绑定、取词、调用与渲染。

import (
	"context"
	contentcontract "go_wp/internal/module/content/contract"
	pagecontract "go_wp/internal/module/page/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ==========================================================================
// SEO 评分：行内面板、抽屉与三个实体的取数入口（商品 / 分类 / 品牌）。
//
// 取数与算分都在 productservice（product_seo_score.go 的 SeoScoreInputOf / SeoTitleIndex /
// SeoEntityScore）—— 三个 POST 端点与抽屉共用**同一份**取数口径，抽屉与行内按钮各写一份的
// 下场是「同一个商品在两处分数不同」。本文件只剩请求绑定、取词、调用与渲染。

// 评分等级文案（颜色 → 当前语言）的**唯一映射**在 seoscore.ScoreGradeText
// （internal/seo/score_grade.go），调用点在下面的 entityScoreView。本文件此前自带一份
// key + 中文兜底表（与本模块 enums 里的 admin.seo.grade.* 常量配套），已按「同一批展示文案
// 只能有一份定义」收编 —— 收编后 product 取到的是 content / project 用的同一批词条
// admin.seo.score.grade.*。
// ==========================================================================

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// SetSeoTitleSources 注入全站标题索引所需的另外两份只读契约（装配期调用，可空）。
//
// 页面与文章是同一个站点的其它页面，做 title 唯一性时必须在同一份索引里 ——
// 只比商品域的话，商品标题与文章标题撞车照样检不出来。两份契约都可空：
// 未注入时索引退化成商品 / 分类 / 品牌三域，评分本身照常（缺依赖不该变成 500）。
func (h *ProductPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

// seoTitleSources 标题索引所需的另外两份只读契约（未注入时是零值 = 索引退化成三域）。
func (h *ProductPageHandle) seoTitleSources() productservice.SeoTitleExtraSources {
	return productservice.SeoTitleExtraSources{Pages: h.seoPages, Contents: h.seoContents}
}

// scoreInputOf 取评分输入 —— 本包唯一的取数入口（三个 POST 端点与抽屉共用）。
//
// 取数与算分都在 productservice（SeoScoreInputOf）：抽屉与行内按钮各写一份取数口径的下场
// 是「同一个商品在两处分数不同」。这里只做 HTTP 该做的事：拿请求语言、把依赖交进去、
// 把空态的词取好。
//
// in == nil 表示**没取到实体**：可能是数据缺失（没传 id / 库里没有），也可能是读取失败
// （res.Err 非空）。两者的现状出口都是那句空态文案（与拆分前逐字一致），但**绝不是 0 分**；
// 判据与将来换出口（例如读取失败回 500）的位置都在 service 的 SeoScoreInputResult 注释里。
func (h *ProductPageHandle) scoreInputOf(c *gin.Context, kind scoring.PageKind,
	id, projectID string) (in *scoring.EntityPageInput, selfID string, emptyView productdto.ScoreResp) {
	res := productservice.SeoScoreInputOf(c.Request.Context(), h.products, h.instances,
		kind, id, projectID, requestScoreLang(c))
	if res.Input == nil {
		return nil, "", scoreEmptyView(c, res.Empty)
	}
	return res.Input, res.SelfID, productdto.ScoreResp{}
}

// scoreEmptyView 空态片段视图：文案是「key + 中文兜底」的事实（service 给出），取值在 HTTP 层。
func scoreEmptyView(c *gin.Context, empty productservice.SeoScoreEmpty) productdto.ScoreResp {
	return productdto.ScoreResp{Empty: shell.TranslateFor(c)(empty.Key, empty.Fallback)}
}

// ProductScorePanel 商品详情页的编辑期评分（POST /admin/products/seo-score）。
func (h *ProductPageHandle) ProductScorePanel(c *gin.Context) (any, error) {
	req, ok := binding.Form[productdto.ScorePanelReq](c)
	if !ok {
		return nil, response.ErrHandled
	}
	return h.renderScorePanel(c, scoring.KindProduct, req.ProductID, req.ProjectID)
}

// ProductCategoryScorePanel 商品分类页的编辑期评分（POST /admin/product-categories/seo-score）。
func (h *ProductPageHandle) ProductCategoryScorePanel(c *gin.Context) (any, error) {
	req, ok := binding.Form[productdto.ScorePanelReq](c)
	if !ok {
		return nil, response.ErrHandled
	}
	return h.renderScorePanel(c, scoring.KindCategory, req.ID, req.ProjectID)
}

// ProductBrandScorePanel 品牌页的编辑期评分（POST /admin/product-brands/seo-score）。
func (h *ProductPageHandle) ProductBrandScorePanel(c *gin.Context) (any, error) {
	req, ok := binding.Form[productdto.ScorePanelReq](c)
	if !ok {
		return nil, response.ErrHandled
	}
	return h.renderScorePanel(c, scoring.KindBrand, req.ID, req.ProjectID)
}

// renderScorePanel 三个评分面板的唯一流程：取输入 → 取不到渲染空态 → 取到出片段。
//
// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK —— 成功路径返回的也是零值
// productdto.ScoreResp（OK=false），用它分流会把每次成功都判成空态。
func (h *ProductPageHandle) renderScorePanel(c *gin.Context, kind scoring.PageKind, id, projectID string) (any, error) {
	in, selfID, emptyView := h.scoreInputOf(c, kind, id, projectID)
	if in == nil {
		return renderScoreView(emptyView)
	}
	return h.renderEntityScore(c, in, projectID, selfID)
}

// entityScoreViewOf 算分 + 查 title 重复 → 片段视图（三个 POST 端点与抽屉共用同一段流程）。
//
// 算分与冲突判定本身都在 productservice（SeoEntityScore / SeoTitleIndex）：
// 这里只把请求侧的取词函数与依赖交进去，再组装片段视图。
func (h *ProductPageHandle) entityScoreViewOf(ctx context.Context, c *gin.Context,
	in *scoring.EntityPageInput, projectID, selfID string) productdto.ScoreResp {
	tr := shell.TranslateFor(c)
	res, dups := productservice.SeoEntityScore(in,
		productservice.SeoTitleIndex(ctx, h.products, h.seoTitleSources(), projectID, tr), selfID)
	return entityScoreView(tr, in, res, dups)
}

// renderEntityScore 算分 + 查 title 重复 + 渲染片段（三个 POST 入口共用）。
func (h *ProductPageHandle) renderEntityScore(c *gin.Context, in *scoring.EntityPageInput, projectID, selfID string) (any, error) {
	// 片段数据根交出去：取词函数由 UI 线注入（adminui.Bare 走 PrepareBare）。
	return gin.H{"Score": h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)}, nil
}

// renderScoreView 渲染评分片段（空态与结果共用同一份模板：片段按 Score.OK 分流）。
func renderScoreView(sv productdto.ScoreResp) (any, error) {
	return gin.H{"Score": sv}, nil
}

// entityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func entityScoreView(tr func(key, fallback string) string, in *scoring.EntityPageInput, res *scoring.Result,
	dups []scoring.TitleEntry) productdto.ScoreResp {
	sv := productdto.ScoreResp{OK: true}
	if res != nil {
		sv.Total = res.Total
		sv.Grade = res.Grade
		// 页型与调权理由回显（docs/02-E1 §5）：编辑者要能看出「这份分数是按商品页的
		// 尺子量的」，否则同一段描述在商品页比文章页高几分会被当成评分器不稳。
		if res.Profile != nil {
			sv.ProfileType = res.Profile.Type
			sv.ProfileReason = res.Profile.Reason
		}
		sv.Sections = scoreSections(tr, res.Sections)
	}
	title := scoring.EntityTitle(in)
	fillSerp(&sv, tr, in, title)
	fillDuplicateTitle(&sv, title, dups)
	return sv
}

// scoreSections 评分分项 → 片段行（含未达标项的整句说明）。
//
// 未达标行的整句是**模板**：标签 / 实测 / 基准 / 建议四段都是数据，句子的语序与标点
// 由词条决定（英文的冒号与括号与中文不同），命名占位符 + FillTranslate 负责填充
// （词条被写坏时自动回落下面那句中文兜底）。
func scoreSections(tr func(key, fallback string) string, sections []scoring.SectionResult) []productdto.ScoreSectionResp {
	out := make([]productdto.ScoreSectionResp, 0, len(sections))
	for _, sec := range sections {
		item := productdto.ScoreSectionResp{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, productdto.ScoreIssueResp{
				Text: i18n.FillTranslate(tr, productenums.SEOIssueLine,
					"{label}：{actual}（基准 {benchmark}）→ {hint}",
					map[string]string{
						"label": ck.Label, "actual": ck.Actual,
						"benchmark": ck.Benchmark, "hint": ck.Hint,
					}),
				Target: ck.Target,
			})
		}
		out = append(out, item)
	}
	return out
}

// fillSerp 搜索结果预览（标题 / 路径 / 描述）：空值给「未填写」那句文案事实。
func fillSerp(sv *productdto.ScoreResp, tr func(key, fallback string) string, in *scoring.EntityPageInput, title string) {
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
}

// fillDuplicateTitle title 唯一性（SEO-018 编辑期轻量版）：结论形状与发布侧体检一致 ——
// 列出全部冲突页面。
func fillDuplicateTitle(sv *productdto.ScoreResp, title string, dups []scoring.TitleEntry) {
	if len(dups) == 0 {
		return
	}
	sv.DuplicateNote = scoring.DuplicateTitleMessage(title, dups)
	for _, d := range dups {
		sv.Duplicates = append(sv.Duplicates, d.Page)
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
func (h *ProductPageHandle) EntitySeoDrawer(c *gin.Context) (any, error) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if h == nil || h.products == nil {
		c.Status(http.StatusNotFound)
		return nil, response.ErrHandled
	}
	data := gin.H{
		"ScoreURL":     "/admin/products/seo-score",
		"TargetID":     "product-seo-score-" + id,
		"HiddenFields": productScoreHiddenFields(id, projectID),
	}
	in, selfID, emptyView := h.scoreInputOf(c, scoring.KindProduct, id, projectID)
	if in == nil {
		// 读不到实体：仍给 200 + 片段（里面就是那句空态文案）—— 这不是「加载失败」，
		// 抽屉的失败重试界面会把「这个商品读不到」伪装成网络问题。
		data["Score"] = emptyView
		return data, nil
	}
	data["Score"] = h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)
	return data, nil
}

// productScoreHiddenFields 商品评分端点的隐藏字段（与页面上那个 POST 端点同一口径）。
func productScoreHiddenFields(id, projectID string) []seoHiddenField {
	return []seoHiddenField{{"projectId", projectID}, {"productId", id}}
}

// seoHiddenField 抽屉里「评分」表单的一个隐藏字段。
type seoHiddenField struct{ Name, Value string }

// pageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
//
// 实现在 productservice（product_seo_score.go 的 PageDraftTitle），这里留一行委托：本包测试
// 直接调用这个标识符（product_seo_score_test.go），同包改名等于改测试契约。
func pageDraftTitle(doc json.RawMessage) string { return productservice.PageDraftTitle(doc) }

// entityPlainText 商品描述（jsonb）→ 纯文本（评分与前台展示口径一致）。
//
// 同 pageDraftTitle：实现在 productservice.EntityPlainText，本处是给本包测试留的委托。
func entityPlainText(raw json.RawMessage) string { return productservice.EntityPlainText(raw) }
