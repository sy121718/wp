package contenthttp

// article_handle.go — 文章管理页处理器（原 dashboard/inbound/http/article_handle.go，INF-1）。
//
// 搬迁只做三件事：包名换成 contenthttp；壳层调用换成 shell.*；
// 页面标题常量由 dashboard enums 换成同值的 i18n key 字面量（词条表键不变）。
// 取数逻辑、权限点、模板名一律未动。

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
)

const (
	// articleEntityType contents 表当前唯一合法类型（迁移 080 的 CHECK 约束）。
	articleEntityType = "article"
	// articleListPageSize 列表每页条数的兜底值。
	//
	// 正常路径上每页条数由 shell.PageParams 决定（默认 20、上限 100，可被 ?limit= 覆盖），
	// 这个常量只在 limit 异常（0 / 负数）时兜底。它同时是一页的**跨模块查询预算**：
	// 列表里每篇文章都要逐条查一次 presentation 实例（articlesPublished），
	// 条数直接决定查询次数 —— 所以它不是「随便取大点」的数字。
	//
	// 分页之前这里写的是 articleListLimit = 50 且一次性取满：超过 50 篇的文章
	// 在页面上根本不存在（审计 02-L P1-14）。现在按页取，总量不再有上限。
	articleListPageSize = 20
	// 页面标题：字面量就是 i18n key（迁移 163 的词条表键），字面量走 withI18n 的 fallback
	// 链路 t(标题, 标题) 回落原文，与系统页面槽位页、商品详情模板页同口径。
	// 值与 dashboard enums 的 MsgArticlesTitle / MsgArticlesEditTitle 完全相同 ——
	// i18n key 是字符串协议，页面的词条归属跟着页面走，不再跨模块引 enums。
	articlePageTitle = "MsgArticlesTitle"
	articleEditTitle = "MsgArticlesEditTitle"
	articleNewTitle  = "新建文章"
	// articleEmptyField 空字段展示占位（表格空白单元格读不出「没有值」）。
	articleEmptyField = "—"
	// articleBlogPathPrefix 文章详情页的默认线上路径前缀（可在发布区块里改）。
	articleBlogPathPrefix = "/blog/"
)

// 成功回执（进 ?ok=，因此同样登记在白名单里）。
const (
	articleCreatedText = "文章已创建。写完正文后记得保存。"
	articleSavedText   = "已保存。引用这篇文章的页面与已发布的详情页已标记待重建。"
	articleDeletedText = "文章已删除。"
)

// articleFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// 键有两类：
//   - content 模块的错误常量值（常量值就是常量名本身，见 contentenums）；
//   - 本页自造的成功 / 参数级文案，值等于自身（它们会进 ?ok= / ?err=）。
//
// 为什么必须有这份白名单：content 契约把错误当字符串返回，未命中的多半是数据库原文
// （可能带表名甚至 SQL 片段），那是给运维看的，不能直接进页面。
var articleFacingMessages = map[string]string{
	contentenums.ErrInvalidParam: "提交的信息不完整，请检查标题与路径后重试。",
	contentenums.ErrNotFound:     "这篇文章不存在，可能已被删除。",
	contentenums.ErrInvalidType:  "内容类型不受支持（本页只处理文章）。",
	contentenums.ErrInvalidField: "提交了不支持的字段，请刷新页面后重试。",
	contentenums.ErrSlugTaken:    "这个路径（slug）已被另一篇文章占用，换一个。",
	contentenums.ErrDataInvalid:  "文章数据格式异常，请联系管理员。",
	articleCreatedText:           articleCreatedText,
	articleSavedText:             articleSavedText,
	articleDeletedText:           articleDeletedText,
	articleMissingIDText:         "缺少文章标识，请回到列表页重试。",
	articleMissingTitleText:      "请先填写文章标题。",
	articleMissingSlugText:       "请先填写文章路径（slug）。",
}

// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记会在回显时被自己吞掉）。
const (
	articleMissingIDText    = "缺少文章标识，请回到列表页重试。"
	articleMissingTitleText = "请先填写文章标题。"
	articleMissingSlugText  = "请先填写文章路径（slug）。"
)

// articlePublishPort 发布区块所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本页真正用到的方法：读绑定、首次发布、重建、改 URL。不直接依赖
// presentationcontract 全量契约（它还带着删除、切换模板等本页不碰的写能力）。
type articlePublishPort interface {
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)
}

// articlePageHandle 文章管理页处理器。
type articlePageHandle struct {
	contents  contentcontract.ContentService
	projects  projectcontract.ProjectService
	templates contenttemplatecontract.ContentTemplateService
	// instances 发布能力（装配期注入；为空时发布区块整体不渲染）。
	instances articlePublishPort
	// pages 页面能力（导入到画布用）：只调 Create 新建一页草稿，不碰发布/删除。
	// 为空时导入区块明确提示"能力未装配"，而不是给一个点了会 500 的按钮。
	pages pagecontract.PageService
	// linkLocator 已上线路径解析端口（SEO-015 内链建议；为空时建议端点降级为空列表）。
	linkLocator presentationcontract.PublishedEntityLocator
}

// NewArticlePageHandle 构造。
func NewArticlePageHandle(contents contentcontract.ContentService, projects projectcontract.ProjectService,
	templates contenttemplatecontract.ContentTemplateService, instances articlePublishPort,
	pages pagecontract.PageService) *articlePageHandle {
	return &articlePageHandle{
		contents: contents, projects: projects, templates: templates,
		instances: instances, pages: pages,
	}
}

// ArticlesPage 文章列表（GET /admin/articles）。
//
// 分页（审计 02-L P1-14）：页码与每页条数走 shell.PageParams，总数由内容契约的 Count 给出
// （与 List 同一份过滤条件：实体类型和关键词），列表按 offset 取当页 —— 不再「一次取 50 条、
// 超过 50 篇的文章在页面上根本不存在」。
//
// 取数顺序是**先计数 → 收敛页码 → 再取当页**（上一轮的实测教训，货源页与采购页各踩过一次）：
// 反过来（先取第 N 页再数总数）时，越界页码会让 service 返回空页，而分页条按收敛后的页码
// 渲染 ——「表格为空、分页条却显示第 2 页」正是这样产生的。
//
// 不走「全量拉取 + handler 切片」：articlesPublished 对每篇逐条查 presentation 实例，
// 全量取数意味着 N 次跨模块查询，文章量级一涨就成倍放大（这正是上一轮否决该方案的理由）。
func (h *articlePageHandle) ArticlesPage(c *gin.Context) {
	ctx := c.Request.Context()
	pageErr := articleQueryText(c, c.Query("err"), shell.PageInternalText(c))
	pageOk := articleQueryText(c, c.Query("ok"), "")

	page, limit := shell.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	filter := &contentdto.ListReq{EntityType: articleEntityType, Keyword: keyword}
	// 先计数。总数只用于「总页数」与页码收敛；计数失败不阻塞列表取数（列表照常按原页码取），
	// 但会把错误显示出来 —— 静默地「没有分页条」会让人以为文章本来就不多。
	total, cerr := h.contents.Count(ctx, filter)
	if cerr != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, cerr))
	} else {
		page = articleClampPage(page, limit, total)
	}
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
	}
	data := articleListPageData(list, articlesPublished(ctx, h.instances, list), pageErr, pageOk)
	data["Keyword"] = keyword
	data["Limit"] = limit
	data["ClearFilterURL"] = shell.FilterBaseURL("/admin/articles", map[string]string{"limit": strconv.Itoa(limit)})
	// Total 覆盖为**真源总数**：articleListPageData 给的是当页行数，分页之后它不再等于
	// 文章总数 —— 不覆盖的话列表工具栏会写着「全部文章（20）」，而库里有两百篇。
	if cerr == nil {
		data["Total"] = total
	}
	// 分页条（shell 组件，服务端渲染）：单页或空数据时 BuildPagination 返回 nil，
	// TemplateKeys 给空 map，模板的 {{if .["PaginationLinks"]}} 自然跳过。
	//
	// 翻页保留关键词，回执 ?err= / ?ok= / ?done= 不进基地址，避免翻页后重复显示。
	for k, v := range shell.BuildPagination(total, page, limit,
		shell.FilterBaseURL("/admin/articles", map[string]string{"keyword": keyword}), shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	// 依赖失效影响面（只读）：文章 / 块 / 主题 / 导航变更后，哪些页面正在等待重建。
	// 本批只做可见性，不做自动重建（见 article_stale_impact.go）。
	data["StaleImpact"] = articleStaleImpact(ctx, h)
	// 批量动作的结果走独立的 ?done=：本页的 ?err= 要过 articleFacingMessages 白名单
	// （防数据库原文直出），带计数的动态文案进不了那张表（值互不相同）。回显因此走
	// articlePageDone 的受控出口 —— 值由服务端拼装，但**页面不是可信边界**：
	// ?done=任意文案 谁都能手写，原样渲染出来就是一条顶着「成功」样式的伪造消息。
	data["Done"] = articlePageDone(c, c.Query("done"))
	c.HTML(http.StatusOK, "admin/content/articles.html", shell.Prepare(c, data))
}

// articleClampPage 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 与 shell.BuildPagination 内部的收敛同一条规则（总页数由 total 与 limit 算出），
// 差别只在于这里发生在**取数之前**：越界页码（手输 URL、书签失效、上一次翻页留下的页码）
// 直接传给取数层时 service 会老实返回空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样产生的。
// 同形实现在 product 域（clampPageToTotal），两处都是「取数前收敛」这一条判据的落地。
func articleClampPage(page, limit int, total int64) int {
	if limit < 1 {
		limit = articleListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

// ArticleEditPage 文章编辑页（GET /admin/articles/edit?id=）；不带 id 即新建。
func (h *articlePageHandle) ArticleEditPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.Query("id"))
	pageErr := articleQueryText(c, c.Query("err"), shell.PageInternalText(c))
	pageOk := articleQueryText(c, c.Query("ok"), "")

	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
		} else {
			item = got
		}
	}
	c.HTML(http.StatusOK, "admin/content/article_edit.html",
		shell.Prepare(c, articleEditPageData(ctx, h, item, id, pageErr, pageOk, requestScoreLang(c))))
}

// ArticleCreate 新建文章（POST /admin/articles/create，权限点 content:create）。
func (h *articlePageHandle) ArticleCreate(c *gin.Context) {
	form := articleFormOf(c)
	if form.Title == "" {
		articleRedirectList(c, articleMissingTitleText, "")
		return
	}
	if form.Slug == "" {
		articleRedirectList(c, articleMissingSlugText, "")
		return
	}
	res, err := h.contents.Create(c.Request.Context(), &contentdto.CreateReq{
		EntityType: articleEntityType, Slug: form.Slug, Data: form.data(),
	})
	if err != nil {
		articleRedirectList(c, articleFacingError(c, err), "")
		return
	}
	// 新建后直接进编辑页：列表页的表单只有标题与路径，正文要在这里写。
	articleRedirectEdit(c, res.ID, articleCreatedText, "")
}

// ArticleUpdate 保存文章（POST /admin/articles/update，权限点 content:update）。
func (h *articlePageHandle) ArticleUpdate(c *gin.Context) {
	form := articleFormOf(c)
	if form.ID == "" {
		h.articleUpdateFailure(c, articleMissingIDText, "id")
		return
	}
	if form.Title == "" {
		h.articleUpdateFailure(c, articleMissingTitleText, "title")
		return
	}
	if _, err := h.contents.Update(c.Request.Context(), &contentdto.UpdateReq{ID: form.ID, Data: form.data()}); err != nil {
		h.articleUpdateFailure(c, articleFacingError(c, err), "")
		return
	}
	articleRedirectEdit(c, form.ID, articleSavedText, "")
}

// articleUpdateFailure reads metadata for the side panels, then replaces every editable
// field with this POST's raw value (including empty strings).
func (h *articlePageHandle) articleUpdateFailure(c *gin.Context, pageErr, invalidField string) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.PostForm("id"))
	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			// 元数据读取错误只记录日志，页面仍展示原本的保存错误。
			articleInternalText(c, err)
		} else {
			item = got
		}
	}
	data := articleEditPageData(ctx, h, item, id, pageErr, "", requestScoreLang(c))
	form := data["Form"].(gin.H)
	for key, field := range map[string]string{
		"Title": "title", "Body": "body", "Excerpt": "excerpt",
		"FeaturedImage": "featuredImage", "SEOTitle": "seoTitle",
		"SEODescription": "seoDescription", "FocusKeyword": "focusKeyword",
	} {
		form[key] = c.PostForm(field)
	}
	data["IsNew"] = false // 缺 id 时仍展示编辑页，避免误落入新建动作。
	data["EditUnavailable"] = item == nil
	if item == nil {
		// 元数据不可用时禁用依赖旧版本、路径和实体标识的所有后续动作。
		for k, v := range articlePublishUnavailable("") {
			data[k] = v
		}
		for k, v := range articleImportUnavailable("") {
			data[k] = v
		}
		data["TemplateEditURL"] = ""
	}
	data["InvalidField"] = invalidField
	data["Score"] = articleScoreViewOf(map[string]any{
		"title": form["Title"], "body": form["Body"], "excerpt": form["Excerpt"],
		"seoTitle": form["SEOTitle"], "seoDescription": form["SEODescription"],
		"focusKeyword": form["FocusKeyword"],
	}, articlePreviewURL(articleSlugOf(item)), requestScoreLang(c))
	c.HTML(http.StatusOK, "admin/content/article_edit.html", shell.Prepare(c, data))
}

// ArticleDelete 删除文章（POST /admin/articles/delete，权限点 content:delete）。
func (h *articlePageHandle) ArticleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		articleRedirectList(c, articleMissingIDText, "")
		return
	}
	if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
		articleRedirectList(c, articleFacingError(c, err), "")
		return
	}
	articleRedirectList(c, "", articleDeletedText)
}

// ArticlesBulkDelete 批量删除文章（POST /admin/articles/bulk-delete，权限点 content:delete）。
//
// 逐条走**同一条单条删除路径**（h.contents.Delete）：某一条失败不中断整批 ——
// 批量操作因为一条失败就整批回滚时，用户会以为「一条都没删」然后反复重试。
// 结果按「已删除 N 篇 / 跳过 M 篇」回带列表页（回带通道见 ArticlesPage 的 Done）。
func (h *articlePageHandle) ArticlesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经白名单判定来源。
		articleRedirectList(c, articleFacingOrInternal(c, berr), "")
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusFound, "/admin/articles?done="+url.QueryEscape(articleBulkDeleteResult(c, deleted, skipped)))
}

// articleBulkText 批量结论文案的一条模板（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧 articleBulkDeleteResult 拿它 Sprintf 出整句，
// 读侧 articleDoneTexts 拿**同一个值**、经同一处取词（articleBulkTextOf）得到当前语言模板
// 再归一比对。读侧另抄一份中文的后果是静默的 —— 写侧改了措辞候选就失配，
// 页面上变成「没有这条提示」（成功态未命中落空串）。
type articleBulkText struct{ key, fallback string }

// articleBulkTextOf 取一条批量结论文案的当前语言文本（写侧与读侧**共用这一个取法**）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（本文件的模板一律只允许 %s，
// 数字先经 strconv.Itoa）—— 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func articleBulkTextOf(c *gin.Context, t articleBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// articleBulkResultTemplates 批量删除的四个结论分支（**写读共用这一份**）。
//
// 写侧 articleBulkDeleteResult 用它 Sprintf 出文案，读侧 articleDoneTexts 用它（当前语言）
// 经 shell.NoticeTemplate 归一后整体比对。
var articleBulkResultTemplates = []articleBulkText{
	{contentenums.BulkArticleNoneSelected, "没有选中任何文章，列表未改动。"},
	{contentenums.BulkArticleAllDeleted, "已删除 %s 篇文章。"},
	{contentenums.BulkArticleAllSkipped, "%s 篇文章都未能删除，列表未改动。"},
	{contentenums.BulkArticlePartial, "已删除 %s 篇，%s 篇未能删除（可能已被删除）。"},
}

// articleBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚，
// 不能只报「操作完成」（部分成功被静默成全部成功，用户不会再去看剩下那几篇）。
func articleBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return articleBulkTextOf(c, articleBulkResultTemplates[0])
	case skipped == 0:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[1]), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[2]), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[3]), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// articleDoneTexts 列表页 ?done= 可以原样展示的受控文案（**当前语言**，数字归一后可比）。
func articleDoneTexts(c *gin.Context) []string {
	out := make([]string, 0, len(articleBulkResultTemplates))
	for _, tpl := range articleBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(articleBulkTextOf(c, tpl)))
	}
	return out
}

// articlePageDone 列表页 ?done= 的受控出口（成功提示：未命中落空串）。
//
// 判定用 shell.FacingNotice 的**整体**匹配（逐字 / 数字归一 / 「候选 + ：」），不是
// strings.Contains —— 后者只要夹带一段已知文案就能往页面上塞任意前缀 / 后缀。
// 未命中不落归口文案：成功提示没有「必须说点什么」的语义。
func articlePageDone(c *gin.Context, raw string) string {
	return shell.FacingQueryText(raw, "", func(msg string) string {
		return shell.FacingNotice(msg, articleDoneTexts(c))
	})
}

// ArticleScorePanel 渲染 SEO 评分侧栏片段（POST /admin/articles/score）。
//
// 走 HTMX 局部刷新而不是整页重绘：编辑者刚改完标题/关键词就按「重新评分」，
// 表单不重画，光标与未保存的正文都不会丢。无 JS 时退化为「保存后刷新整页看分数」——
// 编辑页的初始评分就是服务端算好后直接渲染进去的。
//
// 复用页面设置面板那份片段（fragments/seo_score）：两边吃的是同一个 scoreView，
// 各写一份模板只会让「哪些检查项算未达标」的呈现方式慢慢分叉。
func (h *articlePageHandle) ArticleScorePanel(c *gin.Context) {
	data := articleFormOf(c).data()
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": articleScoreViewOf(data, strings.TrimSpace(c.PostForm("url")), requestScoreLang(c))})
}

// —— 表单与视图 ——
