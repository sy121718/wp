package productservice

// product_seo_score.go —— SEO 评分的**输入构造**（审计 SEO-016 / SEO-018）。
//
// 三条分工，改本文件前先读：
//  1. **评分规则本体不在本文件**：算分与 title 冲突判定都在 internal/seo/scoring
//     （ScoreEntityPage / DuplicateTitles / EntityTitle）。本文件只把实体与站点事实翻译成
//     它的输入 —— 拆分前 HTTP 文件头那句「评分本体在 internal/seo/scoring，这里只做输入与
//     视图」只在后半句上做了搬家：**输入**到了这里，**视图**仍留在
//     inbound/http/product_detail_template.go。
//  2. 与本包另外两个「详情页模板」文件的分工：product_detail_template_page.go 是整页读模型，
//     product_detail_template_ops.go 是发布 / 预览的写编排；本文件只服务评分面板
//     （商品编辑页的行内按钮、分类 / 品牌表单的行内展开、商品 SEO 抽屉）。
//  3. **本文件不依赖 HTTP 框架**：拆分前 entityScoreInputOf 的请求上下文形参已去掉，请求侧的事实
//     （当前语言、取词函数）由调用方以显式入参传入。判据：本文件对框架包标识符零命中（含 import 行）。
//
// 依赖也以**窄只读端口**注入（ProductReadPort / SeoInstancePathReader），实参由
// inbound/http 传入 —— 那边拿到的正是本模块的 ProductService 与 presentation 契约，
// 因此这里不需要任何装配期字段，也不会因为「谁先装配」而静默拿到半套依赖。

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/seo/scoring"
	"go_wp/pkg/i18n"
)

// ProductReadPort 商品域的只读面（productcontract.ProductService 满足它）。
//
// 只列评分输入与模板发布真正用到的六个读方法：评分不需要任何写能力 ——
// 拿宽接口（ProductService 全量）会让「评分顺手改了一下商品」这种越界在编译期看不出来。
type ProductReadPort interface {
	Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error)
	List(ctx context.Context, req *productdto.ListReq) (list []*productdto.ProductResp, err error)
	GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (res *productdto.CategoryResp, err error)
	ListCategories(ctx context.Context, req *productdto.ListCategoryReq) (list []*productdto.CategoryResp, err error)
	GetBrand(ctx context.Context, req *productdto.GetBrandReq) (res *productdto.BrandResp, err error)
	ListBrands(ctx context.Context, req *productdto.ListBrandReq) (list []*productdto.BrandResp, err error)
}

// SeoInstancePathReader 按实体读发布实例（presentation 契约满足它；**可空**）。
//
// 可空是行为契约的一部分，不是防御性写法：未注入实例契约时「线上路径未知」，
// 评分器据此判 canonical / JSON-LD 为假 —— 拆分前 instancePath 的注释与这里逐字同义。
type SeoInstancePathReader interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
}

// SeoTitleExtraSources 标题索引里「同一个站点的其它页面」（页面草稿与文章，两者都可空）。
//
// 缺依赖不该让评分不可用，故两份都按可选处理：只比商品域的话，
// 商品标题与文章标题撞车照样检不出来。
type SeoTitleExtraSources struct {
	Pages    pagecontract.PageService       // 页面草稿（可空）
	Contents contentcontract.ContentService // 文章（可空）
}

// seoTitleIndexSize 标题索引每类实体的取样上限。
//
// 「编辑期轻量版」的重点是点一下就有结果（一次列表查询，不是遍历产物文件）。
// 超过上限时商品一侧由 service 按自己的分页上限收敛 —— 这点在注释里写明，
// 因为「索引不完整」会让冲突清单漏报，而漏报是检查类功能最不能有的失效模式。
const seoTitleIndexSize = 100

// SeoScoreEmpty 空态的**事实**（key + 中文兜底）。
//
// 取词留在 HTTP 层：service 拿不到 shell.TranslateFor(c) 那份取词函数（它要 HTTP 请求上下文）。
// 与 productMsg 同形但不是同一个类型 —— productMsg 是 HTTP 层的提示形状，
// 这里只要「取词需要什么」，不要让 service 依赖 HTTP 的展示类型。
type SeoScoreEmpty struct {
	Key      string
	Fallback string
}

// SeoScoreInputResult 评分输入的构造结果。
//
// Input / Err 两个字段合起来就是「数据缺失」与「数据读取失败」的判据：
//   - Input == nil 且 Err == nil —— **数据缺失**（没传实体 id，或库里没有这个实体）→ 空态；
//   - Input == nil 且 Err != nil —— **读取失败**（DB / 契约报错）→ 现状出口同样是空态
//     （与拆分前逐字一致：那句「读不到…无法评分」），但**绝不是 0 分**：
//     调用方现在就能把两者分开，将来要换出口（例如读取失败回 500）不必再区分一次。
type SeoScoreInputResult struct {
	Input  *scoring.EntityPageInput
	SelfID string
	Empty  SeoScoreEmpty
	Err    error
}

// SeoScoreInputOf 按实体类型取数并构造评分输入。
//
// 三个 POST 端点与 SEO 抽屉共用这一份：同一个实体不该有两种取数口径
// （抽屉与行内按钮各写一份，下场是「同一个商品在两处分数不同」）。
//
// kind 就是 scoring.PageKind（「用哪套尺子」）：取值不在这里另立一份常量 ——
// 控制器此前自带 scoreKindProduct / Category / Brand 三个字符串，与 scoring.Kind* 逐字同值，
// 改一处另一处静默失配。缺实体 id 时的空态文案走 productenums.ScoreMissingEntityLabel。
//
// lang 是请求语言（后台 Cookie / Accept-Language）：service 没有 HTTP 请求上下文，
// 由调用方取值后传入 —— 评分是按语言取词与判定的（SEO-001）。
func SeoScoreInputOf(ctx context.Context, r ProductReadPort, insts SeoInstancePathReader,
	kind scoring.PageKind, id, projectID, lang string) SeoScoreInputResult {
	emptyOf := func(key, fallback string) SeoScoreInputResult {
		return SeoScoreInputResult{Empty: SeoScoreEmpty{Key: key, Fallback: fallback}}
	}

	if id == "" {
		// 缺实体 id 的空态：页型 → 文案事实的映射在 enums（控制器不挑文案）。
		key, fallback := productenums.ScoreMissingEntityLabel(string(kind))
		return emptyOf(key, fallback)
	}

	switch kind {
	case scoring.KindCategory:
		node, err := r.GetCategory(ctx, &productdto.GetCategoryReq{ID: id})
		if err != nil || node == nil {
			res := emptyOf(productenums.SEOEmptyCategoryUnreadable, "读不到这个分类，无法评分")
			res.Err = err
			return res
		}
		// SEO 标题 / 描述与「分类名 / 分类描述」合并（2026-09-30，与商品同口径）：
		// 分类名即 <title>、分类描述即 meta description。描述是富文本，先去掉标签
		// 再喂给评分器 —— 否则 <p> 会被算进 meta 描述长度。
		instPath := instancePathOf(ctx, insts, productcontract.EntityTypeCategory, node.ID)
		return SeoScoreInputResult{
			Input: &scoring.EntityPageInput{
				Kind:           scoring.KindCategory,
				Name:           node.Name,
				Description:    StripEntityTags(node.Description),
				SEOTitle:       node.Name,
				SEODescription: StripEntityTags(node.Description),
				Slug:           node.Slug,
				URL:            firstNonEmptyText(instPath, slashSlug(node.Slug)),
				Images:         singleImage(node.Image, "hero"),
				ChildNames:     CategoryChildNames(ctx, r, projectID, node.ID),
				Locale:         lang,
				HasCanonical:   instPath != "",
				HasSchema:      instPath != "",
			},
			SelfID: node.ID,
		}

	case scoring.KindBrand:
		brand, err := r.GetBrand(ctx, &productdto.GetBrandReq{ID: id})
		if err != nil || brand == nil {
			res := emptyOf(productenums.SEOEmptyBrandUnreadable, "读不到这个品牌，无法评分")
			res.Err = err
			return res
		}
		// 同分类：品牌名即 <title>、品牌描述即 meta description（2026-09-30 合并）。
		instPath := instancePathOf(ctx, insts, productcontract.EntityTypeBrand, brand.ID)
		return SeoScoreInputResult{
			Input: &scoring.EntityPageInput{
				Kind:           scoring.KindBrand,
				Name:           brand.Name,
				Description:    StripEntityTags(brand.Description),
				SEOTitle:       brand.Name,
				SEODescription: StripEntityTags(brand.Description),
				Slug:           brand.Slug,
				URL:            firstNonEmptyText(instPath, slashSlug(brand.Slug)),
				Images:         singleImage(brand.Logo, "hero"),
				Locale:         lang,
				HasCanonical:   instPath != "",
				HasSchema:      instPath != "",
			},
			SelfID: brand.ID,
		}

	default:
		detail, err := r.Get(ctx, &productdto.GetReq{ID: id})
		if err != nil || detail == nil {
			res := emptyOf(productenums.SEOEmptyProductUnreadable, "读不到这个商品，无法评分")
			res.Err = err
			return res
		}
		// 线上路径与「产物是否已带 canonical / JSON-LD」是同一个问题的两面：
		// 实例有线上路径 = 详情页真的发布过 = 构建期已注入这两样（presentation 的 applyEntitySEO）。
		instPath := instancePathOf(ctx, insts, productcontract.EntityTypeProduct, detail.ID)
		return SeoScoreInputResult{
			Input: &scoring.EntityPageInput{
				Kind:        scoring.KindProduct,
				Name:        detail.Name,
				Subtitle:    detail.Subtitle,
				Description: EntityPlainText(detail.Description),
				// 评分与前台对齐（2026-09-30 合并）：商品名即 <title>、副标题即 meta description。
				// 若这里仍读 seo_title/seo_description，评分看到的标题会和实际发布出去的**不是同一个**——
				// 编辑改商品名、分数不动，是最难解释的一种不一致。
				SEOTitle:       detail.Name,
				SEODescription: detail.Subtitle,
				Slug:           detail.Slug,
				URL:            firstNonEmptyText(instPath, slashSlug(detail.Slug)),
				Images:         ProductPageImages(detail),
				SpecNames:      VariationSpecNames(detail),
				ChildNames:     ProductCategoryNames(detail),
				Locale:         lang,
				// 未发布时判假：评分器只该显示编辑者能改的东西，而这两项要发布一次才会出现。
				HasCanonical: instPath != "",
				HasSchema:    instPath != "",
			},
			SelfID: detail.ID,
		}
	}
}

// instancePathOf 取实体详情页实例的线上路径（未注入实例契约 / 未发布 → 空串）。
func instancePathOf(ctx context.Context, insts SeoInstancePathReader, entityType, entityID string) string {
	if insts == nil || strings.TrimSpace(entityID) == "" {
		return ""
	}
	inst, err := insts.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: entityType, EntityID: entityID,
	})
	if err != nil || inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.URLPath)
}

// SeoEntityScore 算分 + title 冲突清单（三个端点与抽屉共用同一段流程）。
//
// 两件事都只有一行，仍单独成一个用例：调用方拿到的必须是**同一份输入**算出来的
// 分数与冲突清单 —— 分开写两处，改一处（例如换成先算冲突再算分）就会和另一处不一致。
func SeoEntityScore(in *scoring.EntityPageInput, index []scoring.TitleEntry, selfID string) (*scoring.Result, []scoring.TitleEntry) {
	return scoring.ScoreEntityPage(in), scoring.DuplicateTitles(scoring.EntityTitle(in), index, selfID)
}

// SeoTitleIndex 同工程已发布 / 已有内容的标题索引（审计 SEO-018 编辑期轻量版）。
//
// 每类实体各一次列表查询（商品 / 分类 / 品牌 / 页面 / 文章），不遍历产物文件 ——
// 那是发布侧体检（SEO-019）的活，编辑期要的是点一下就能出结果。
//
// 与发布侧对齐的是**结论形状**（列出全部冲突页面），不是数据来源：发布侧读的是真正
// 服务出去的 <title>，这里读的是实体字段（写之前就得能提示）。两者的差异写在
// scoring.DuplicateTitles 的注释里，改任一处都要一起看。
//
// tr 是取词函数（HTTP 层用 shell.TranslateFor(c) 取来）：只有文章那一项要用它拼
// 「文章 {slug}」——取词函数在 HTTP 层，故由调用方传入。
//
// 「已发布」的判据按实体能力各自取：
//   - 商品：status=published（草稿商品还没有线上页面，拿它判重复会让运营被一堆
//     没上架的东西挡住）；
//   - 分类 / 品牌：它们是分类法，没有发布状态，随商品上线 —— 全量参与比对；
//   - 页面：草稿文档里看不出发布与否，按全部页面参与（宁可多提示一次，
//     也不要漏掉一个即将上线的重复标题）。
func SeoTitleIndex(ctx context.Context, r ProductReadPort, extra SeoTitleExtraSources,
	projectID string, tr func(key, fallback string) string) []scoring.TitleEntry {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var out []scoring.TitleEntry
	if list, err := r.List(ctx, &productdto.ListReq{
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
	if cats, err := flatCategoriesOf(ctx, r, projectID); err == nil {
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
	if brands, err := r.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID}); err == nil {
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
	if extra.Pages != nil {
		if drafts, err := extra.Pages.ListDrafts(ctx); err == nil {
			for _, d := range drafts {
				if d.ProjectID != projectID {
					continue
				}
				if title := PageDraftTitle(d.DraftDocument); title != "" {
					out = append(out, scoring.TitleEntry{Title: title, Page: slashSlug(d.DraftPath), ID: d.ID})
				}
			}
		}
	}
	if extra.Contents != nil {
		if rows, err := extra.Contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: seoTitleIndexSize}); err == nil {
			for _, row := range rows {
				if row == nil || row.Data == nil {
					continue
				}
				// 文章的 SEO 标题即标题（2026-09-30 合并）：不再优先读 seoTitle。
				title := contentText(row.Data, "title")
				if title == "" {
					continue
				}
				// contents 列表接口没有工程过滤参数，标题索引据此标注来源：
				// 多工程部署时别的工程的文章也会进这份索引，宁可报一次重复让人核对，
				// 也好过因为查不到而假报「没有重复」。
				out = append(out, scoring.TitleEntry{
					Title: title,
					Page: i18n.FillTranslate(tr, productenums.SEOIndexArticle, "文章 {slug}",
						map[string]string{"slug": row.Slug}),
					ID: row.ID,
				})
			}
		}
	}
	return out
}

// flatCategoriesOf 取某工程的分类树并摊平成 DFS 前序列表（工程为空时返回空列表）。
//
// 与 inbound/http 的 flatCategories 是同一口径（同一个 ListCategories 契约 + 同一份
// FlattenCategoryTree）；跨包不可见，故在本包重述一遍。迁移后 http 那一份（
// internal/module/product/inbound/http/product_taxonomy.go 的 flatCategories）已无调用方。
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

// ==========================================================================
// 评分输入素材的提取（标题、正文纯文本、图片与分类名）。
//
// 这几个都是纯函数，导出是因为 inbound/http 的同包测试直接引用其中的两个
//（product_seo_score_test.go 的 pageDraftTitle / entityPlainText）——
// 唯一实现在这里，HTTP 侧只留一行委托。
// ==========================================================================

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

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
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

// CategoryChildNames 某分类的子分类名（列表页把子分类渲染成小节）。
//
// 取不到（分类不存在 / 列表失败）返回 nil：少一个小标题只会让标题结构项如实报缺，
// 不会让评分整体失败。
func CategoryChildNames(ctx context.Context, r ProductReadPort, projectID, id string) []string {
	cats, err := flatCategoriesOf(ctx, r, projectID)
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

// firstNonEmptyText 取第一个非空值。
//
// 与 inbound/http 的 firstNonEmptyString 同一逻辑：跨包不可见，而本文件的取数逻辑
// 要用它拼「实例路径 → 否则 slug 近似路径」（迁移后 http 那一份已无调用方）。
func firstNonEmptyText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。同 firstNonEmptyText：跨包不可见，故在本包重述。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}
