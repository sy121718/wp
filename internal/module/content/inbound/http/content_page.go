package contenthttp

import (
	"context"
	"errors"
	"go_wp/internal/middleware/builtin"
	json "go_wp/pkg/jsonx"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockenums "go_wp/internal/module/block/enums"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/siteurl"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/builder/richdoc"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	seoscore "go_wp/pkg/seo"
	"go_wp/pkg/seo/scoreview"
	"go_wp/pkg/seo/scoring"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const (
	articleEntityType = "article"

	articleListPageSize = 20

	articlePageTitle = "MsgArticlesTitle"
	articleEditTitle = "MsgArticlesEditTitle"
	articleNewTitle  = "新建文章"

	articleEmptyField = "—"

	articleBlogPathPrefix = "/blog/"
)

const (
	articleCreatedText = contentenums.OkCreated
	articleSavedText   = contentenums.OkSaved
	articleDeletedText = contentenums.OkDeleted
)

type articleMsg = facing.Msg

var articleFallbacks = map[string]string{

	articleCreatedText:      "文章已创建。写完正文后记得保存。",
	articleSavedText:        "已保存。引用这篇文章的页面与已发布的详情页已标记待重建。",
	articleDeletedText:      "文章已删除。",
	articleMissingIDText:    "缺少文章标识，请回到列表页重试。",
	articleMissingTitleText: "请先填写文章标题。",
	articleMissingSlugText:  "请先填写文章路径（slug）。",

	articleImportEmptyBodyText: "这篇文章的正文是空的，没有可导入的内容。先写点东西再导入。",
	articleImportNoProjectText: "请先选择目标站点工程。",
	articleImportNoPathText:    "请填写新页面的访问路径。",
	articleImportNoPageText:    "导入需要页面能力（未装配），请联系管理员。",
	articleImportDepsText:      "页面能力未装配（装配缺陷），本页只显示文章内容。",
}

const (
	articleMissingIDText    = contentenums.ErrMissingID
	articleMissingTitleText = contentenums.ErrMissingTitle
	articleMissingSlugText  = contentenums.ErrMissingSlug
)

func articleFacingMsgOf(key string) articleMsg {
	return articleMsg{Key: key, Fallback: articleFallbacks[key]}
}

var (
	articleCreatedMsg = articleFacingMsgOf(articleCreatedText)
	articleSavedMsg   = articleFacingMsgOf(articleSavedText)
	articleDeletedMsg = articleFacingMsgOf(articleDeletedText)

	articleMissingIDMsg = articleFacingMsgOf(articleMissingIDText)

	articleMissingTitleMsg = articleFacingMsgOf(articleMissingTitleText)
	articleMissingSlugMsg  = articleFacingMsgOf(articleMissingSlugText)
)

type articlePublishPort interface {
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)

	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)

	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

type articlePageHandle struct {
	contents  contentcontract.ContentService
	projects  projectcontract.ProjectService
	templates contenttemplatecontract.ContentTemplateService

	instances articlePublishPort

	pages pagecontract.PageService

	linkLocator presentationcontract.PublishedEntityLocator
}

func NewArticlePageHandle(contents contentcontract.ContentService, projects projectcontract.ProjectService,
	templates contenttemplatecontract.ContentTemplateService, instances articlePublishPort,
	pages pagecontract.PageService) *articlePageHandle {
	return &articlePageHandle{
		contents: contents, projects: projects, templates: templates,
		instances: instances, pages: pages,
	}
}

func (h *articlePageHandle) ArticlesPage(c *gin.Context) {
	ctx := c.Request.Context()

	pageErr := articleMsg{}

	page, limit := reqparams.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	filter := &contentdto.ListReq{EntityType: articleEntityType, Keyword: keyword}

	total, cerr := h.contents.Count(ctx, filter)
	if cerr != nil {
		pageErr = firstNonEmptyMsg(pageErr, articleFacingErrMsg(c, cerr))
	} else {
		page = articleClampPage(page, limit, total)
	}
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {
		pageErr = firstNonEmptyMsg(pageErr, articleFacingErrMsg(c, err))
	}
	data := articleListPageData(list, articlesPublished(ctx, h.instances, list), pageErr)
	data.Keyword = keyword
	data.Limit = limit
	data.ClearFilterURL = reqparams.FilterBaseURL("/admin/articles", map[string]string{"limit": strconv.Itoa(limit)})

	data.FilterQuery = "?" + url.Values{"keyword": {keyword}, "limit": {strconv.Itoa(limit)}}.Encode()

	if cerr == nil {
		data.Total = int(total)
	}

	data.Pagination = pagination.Page{Total: total, Page: page, Limit: limit,
		Base: reqparams.FilterBaseURL("/admin/articles", map[string]string{"keyword": keyword})}

	data.StaleImpact = articleStaleImpact(ctx, h)
	pageview.RenderTemplate(c, ArticlesPageTemplate, data)
}

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

func (h *articlePageHandle) ArticleEditPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.Query("id"))

	pageErr := articleMsg{}

	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			pageErr = articleFacingErrMsg(c, err)
		} else {
			item = got
		}
	}
	pageview.RenderTemplate(c, ArticleEditPageTemplate, articleEditPageData(ctx, h, item, id, pageErr, requestScoreLang(c), text.RequestTranslateFunc(c)))
}

func (h *articlePageHandle) ArticleCreate(c *gin.Context) {
	form := articleFormOf(c)
	if form.Title == "" {
		response.ErrorMsg(c, http.StatusBadRequest, articleMissingTitleMsg)
		return
	}
	if form.Slug == "" {
		response.ErrorMsg(c, http.StatusBadRequest, articleMissingSlugMsg)
		return
	}
	res, err := h.contents.Create(c.Request.Context(), &contentdto.CreateReq{
		EntityType: articleEntityType, Slug: form.Slug, Data: form.data(),
	})
	if err != nil {
		response.ErrorMsg(c, response.ErrorStatus(err), articleFacingErrMsg(c, err))
		return
	}
	response.SuccessMsg(c, articleCreatedMsg, gin.H{"id": res.ID})
}

func (h *articlePageHandle) ArticleUpdate(c *gin.Context) {
	form := articleFormOf(c)
	if form.ID == "" {
		response.ErrorMsg(c, http.StatusBadRequest, articleMissingIDMsg)
		return
	}
	if form.Title == "" {
		response.ErrorMsg(c, http.StatusBadRequest, articleMissingTitleMsg)
		return
	}
	if _, err := h.contents.Update(c.Request.Context(), &contentdto.UpdateReq{ID: form.ID, Data: form.data()}); err != nil {
		response.ErrorMsg(c, response.ErrorStatus(err), articleFacingErrMsg(c, err))
		return
	}
	response.SuccessMsg(c, articleSavedMsg, gin.H{"id": form.ID})
}

func (h *articlePageHandle) ArticleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		articleListJump(c, false, articleMissingIDMsg)
		return
	}
	if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
		articleListJump(c, false, articleFacingErrMsg(c, err))
		return
	}
	articleListJump(c, true, articleDeletedMsg)
}

func (h *articlePageHandle) ArticlesBulkDelete(c *gin.Context) {

	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {

		articleListJump(c, false, articleBulkIDsJumpMsg(c, berr))
		return
	}
	if err := h.contents.BulkDeleteArticles(c.Request.Context(), ids); err != nil {
		pageview.FailScene(c, articleErrScene, err)
		return
	}
	pageview.Success(c)
}

var articleBulkResultTemplates = []articleMsg{
	{Key: contentenums.BulkArticleNoneSelected, Fallback: "没有选中任何文章，列表未改动。"},
	{Key: contentenums.BulkArticleAllDeleted, Fallback: "已删除 %s 篇文章。"},
	{Key: contentenums.BulkArticleAllSkipped, Fallback: "%s 篇文章都未能删除，列表未改动。"},
	{Key: contentenums.BulkArticlePartial, Fallback: "已删除 %s 篇，%s 篇未能删除（可能已被删除）。"},
}

func articleBulkDeleteResult(deleted, skipped int) articleMsg {
	switch {
	case deleted == 0 && skipped == 0:
		return articleBulkResultTemplates[0]
	case skipped == 0:
		return articleBulkResultTemplates[1].WithArgs(strconv.Itoa(deleted))
	case deleted == 0:
		return articleBulkResultTemplates[2].WithArgs(strconv.Itoa(skipped))
	default:
		return articleBulkResultTemplates[3].WithArgs(strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

func (h *articlePageHandle) ArticleScorePanel(c *gin.Context) {
	data := articleFormOf(c).data()

	pageview.RenderTemplate(c, SeoScoreFragmentTemplate, &SeoScoreFragmentView{
		Score: articleScoreViewOf(data, strings.TrimSpace(c.PostForm("url")), requestScoreLang(c), text.RequestTranslateFunc(c)),
	})
}

func (h *articlePageHandle) ArticleSeoDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("id"))
	if id == "" || h == nil || h.contents == nil {
		c.Status(http.StatusNotFound)
		return
	}
	var item *contentdto.ContentResp
	got, err := h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
	if err != nil {
		logger.Scene("content").With("article_id", id).Error(err, "读取文章失败（SEO 评测抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	item = got
	if item == nil || item.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	data := item.Data
	pageview.RenderTemplate(c, ArticleSeoDrawerTemplate, &ArticleSeoDrawerResp{
		Score: articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item)), requestScoreLang(c), text.RequestTranslateFunc(c)),
		IsNew: false,
	})
}

func (h *articlePageHandle) ArticlePreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	tr := text.RequestTranslateFunc(c)
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		pageview.RenderTemplate(c, ContentPreviewNoticeTemplate, previewPlaceholderResp(c, tr(contentenums.PreviewNeedSave, "保存这篇文章后，这里会显示它的详情页真实形态。")))
	}
	if h == nil || h.instances == nil {
		pageview.RenderTemplate(c, ContentPreviewNoticeTemplate, previewPlaceholderResp(c, tr(contentenums.PreviewUnavailable, "预览不可用：发布能力未装配。")))
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		EntityType: articleEntityType, EntityID: id,
	})
	if err != nil {

		logger.Scene("content").With("article_id", id).Error(err, "文章详情页预览渲染失败")
		pageview.RenderTemplate(c, ContentPreviewNoticeTemplate, previewPlaceholderResp(c, tr(contentenums.PreviewFailed, "预览渲染失败（多半是这部文章还没有可用的详情页模板）。")))
	}
	if res == nil || strings.TrimSpace(res.HTML) == "" {
		pageview.RenderTemplate(c, ContentPreviewNoticeTemplate, previewPlaceholderResp(c, tr(contentenums.PreviewEmpty, "还没有可渲染的详情页：先给 article 类型建一套内容模板。")))
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
	return
}

func previewPlaceholderResp(c *gin.Context, text string) PreviewNoticeResp {
	return PreviewNoticeResp{Text: text, Lang: response.RequestLanguage(c)}
}

const (
	articleImportKind       = "home"
	articleImportTargetType = "none"

	articleImportPathPrefix = "/article-"
)

const (
	articleImportEmptyBodyText = contentenums.ImportErrEmptyBody
	articleImportNoProjectText = contentenums.ImportErrNoProject
	articleImportNoPathText    = contentenums.ImportErrNoPath
	articleImportNoPageText    = contentenums.ImportErrNoPageCapability
	articleImportDepsText      = contentenums.ImportErrDepsMissing
)

func articleImportTextMsg(key string) articleMsg {
	return articleMsg{Key: key, Fallback: articleFallbacks[key]}
}

type articleImportText struct{ Key, Fallback string }

type articleImportPreviewView struct {
	OK        bool
	NodeCount int

	CountText string
	Types     []articleImportTypeView
	Warnings  []articleImportWarningView
	Lossless  bool
}

type articleImportTypeView struct {
	Type string

	LabelKey      string
	LabelFallback string
	Count         int
}

type articleImportWarningView struct {
	Tag string

	ActionKey      string
	ActionFallback string

	Detail string
}

var articleComponentLabels = map[string]articleImportText{
	"core.heading":   {contentenums.ImportNodeHeading, "标题"},
	"core.text":      {contentenums.ImportNodeText, "正文"},
	"core.list":      {contentenums.ImportNodeList, "列表"},
	"core.quote":     {contentenums.ImportNodeQuote, "引用"},
	"core.image":     {contentenums.ImportNodeImage, "图片"},
	"core.divider":   {contentenums.ImportNodeDivider, "分隔线"},
	"core.table":     {contentenums.ImportNodeTable, "表格"},
	"core.container": {contentenums.ImportNodeContainer, "容器"},
}

var articleImportActionLabels = map[richdoc.WarningAction]articleImportText{
	richdoc.ActionUnwrap:      {contentenums.ImportActionUnwrap, "去壳保留内容"},
	richdoc.ActionDrop:        {contentenums.ImportActionDrop, "已丢弃"},
	richdoc.ActionTrim:        {contentenums.ImportActionTrim, "已裁剪"},
	richdoc.ActionPlaceholder: {contentenums.ImportActionPlaceholder, "占位（不可还原）"},
}

func (h *articlePageHandle) ArticleImportPreview(c *gin.Context) {
	item, err := h.articleByID(c)
	if err != nil {
		pageview.RenderTemplate(c, ArticleImportFragmentTemplate, &ArticleImportFragmentView{
			Preview: articleImportPreviewView{}, Err: articleImportErrMsg(c, err),
		})
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		pageview.RenderTemplate(c, ArticleImportFragmentTemplate, &ArticleImportFragmentView{
			Preview: articleImportPreviewView{}, Err: articleImportErrMsg(c, err),
		})
		return
	}

	pageview.RenderTemplate(c, ArticleImportFragmentTemplate, &ArticleImportFragmentView{
		Preview: articleImportPreviewViewOf(res, text.RequestTranslateFunc(c)), Err: articleMsg{},
	})
}

func (h *articlePageHandle) ArticleImportCreate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	draftPath := strings.TrimSpace(c.PostForm("draftPath"))

	if id == "" {
		articleListJump(c, false, articleMissingIDMsg)
		return
	}
	if h.pages == nil {
		articleEditJump(c, false, id, articleImportTextMsg(articleImportDepsText))
		return
	}
	if projectID == "" {
		articleEditJump(c, false, id, articleImportTextMsg(articleImportNoProjectText))
		return
	}
	if draftPath == "" {
		articleEditJump(c, false, id, articleImportTextMsg(articleImportNoPathText))
		return
	}
	item, err := h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
	if err != nil {
		articleEditJump(c, false, id, articleImportErrMsg(c, err))
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		articleEditJump(c, false, id, articleImportErrMsg(c, err))
		return
	}
	if len(res.Nodes) == 0 {
		articleEditJump(c, false, id, articleImportTextMsg(articleImportEmptyBodyText))
		return
	}
	doc, err := articleImportDocument(item.Data, res.Nodes)
	if err != nil {
		articleEditJump(c, false, id, articleImportErrMsg(c, err))
		return
	}
	page, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              articleImportKind,
		ContentTargetType: articleImportTargetType,
		DraftPath:         draftPath,
		DraftDocument:     doc,
	})
	if err != nil {
		articleEditJump(c, false, id, articleImportErrMsg(c, err))
		return
	}

	c.Redirect(http.StatusFound, "/workbench?id="+page.ID)
}

func (h *articlePageHandle) articleByID(c *gin.Context) (*contentdto.ContentResp, error) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		id = strings.TrimSpace(c.Query("id"))
	}
	if id == "" {
		return nil, errArticleIDMissing
	}
	return h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
}

var errArticleIDMissing = &articleImportError{text: articleMissingIDText}

type articleImportError struct{ text string }

func (e *articleImportError) Error() string { return e.text }

func articleImportDocument(data map[string]any, nodes []*core.Node) (json.RawMessage, error) {
	seo := map[string]any{"schemaType": "article"}
	if v := articleStr(data, "title"); v != "" {
		seo["title"] = v
	}
	if v := articleStr(data, "excerpt"); v != "" {
		seo["description"] = v
	}
	if v := articleStr(data, "focusKeyword"); v != "" {
		seo["focusKeyword"] = v
	}
	doc := map[string]any{
		"settings": map[string]any{

			"layout": map[string]any{"mode": "full"},
			"seo":    seo,
		},
		"root": nodes,
	}
	return json.Marshal(doc)
}

func articleImportPreviewViewOf(res *richdoc.Result, trs ...func(key, fallback string) string) articleImportPreviewView {
	tr := articlePublishTr(trs)
	if res == nil {
		return articleImportPreviewView{}
	}
	counts := map[string]int{}
	order := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		if n == nil {
			continue
		}
		if _, seen := counts[n.Type]; !seen {
			order = append(order, n.Type)
		}
		counts[n.Type]++
	}
	view := articleImportPreviewView{
		OK:        true,
		NodeCount: len(res.Nodes),
		CountText: i18n.FillTranslate(tr, contentenums.ImportResultCount,
			"转换结果：{count} 个组件", map[string]string{"count": strconv.Itoa(len(res.Nodes))}),
		Lossless: len(res.Warnings) == 0,
		Types:    make([]articleImportTypeView, 0, len(order)),
		Warnings: make([]articleImportWarningView, 0, len(res.Warnings)),
	}
	for _, t := range order {

		labelKey, labelFallback := "", t
		if item, ok := articleComponentLabels[t]; ok {
			labelKey, labelFallback = item.Key, item.Fallback
		}
		view.Types = append(view.Types, articleImportTypeView{
			Type: t, LabelKey: labelKey, LabelFallback: labelFallback, Count: counts[t],
		})
	}
	for _, w := range res.Warnings {
		actionKey, actionFallback := "", string(w.Action)
		if item, ok := articleImportActionLabels[w.Action]; ok {
			actionKey, actionFallback = item.Key, item.Fallback
		}
		view.Warnings = append(view.Warnings, articleImportWarningView{
			Tag: w.Tag, ActionKey: actionKey, ActionFallback: actionFallback, Detail: w.Detail,
		})
	}
	return view
}

func articleImportErrMsg(c *gin.Context, err error) articleMsg {
	return articleErrMsgOf(c, articleImportFallbacks, err)
}

var articleImportFallbacks = map[string]string{
	pageenums.ErrInvalidParam:    "提交的信息不完整，请检查工程与页面路径后重试。",
	pageenums.ErrProjectNotFound: "选择的站点工程不存在，请刷新页面后重试。",
	pageenums.ErrInvalidKind:     "页面类型与内容目标不匹配（导入用的是普通页面类型），这是程序错误，请联系管理员。",
	pageenums.ErrInvalidDocument: "生成的页面草稿不合法，请联系管理员（这是导入器的缺陷）。",
	pageenums.ErrInvalidPath:     "页面路径不合法：只能包含字母、数字、连字符与斜杠。",
	pageenums.ErrPathOccupied:    "这个页面路径已经被占用了，换一个（例如在末尾加 -2）。",
}

func (h *articlePageHandle) ArticleNewPage(c *gin.Context) {
	pageview.RenderTemplate(c, ArticleNewPageTemplate, &ArticleNewPageResp{
		Title: articleNewTitle,
		Menu:  "articles",

		Form: &ArticleFormResp{},
	})
}

type articleForm struct {
	ID            string
	Slug          string
	Title         string
	Body          string
	Excerpt       string
	FeaturedImage string
	FocusKeyword  string
}

func articleFormOf(c *gin.Context) articleForm {
	return articleForm{
		ID:            strings.TrimSpace(c.PostForm("id")),
		Slug:          strings.TrimSpace(c.PostForm("slug")),
		Title:         strings.TrimSpace(c.PostForm("title")),
		Body:          core.SanitizeRichHTML(c.PostForm("body")),
		Excerpt:       strings.TrimSpace(c.PostForm("excerpt")),
		FeaturedImage: strings.TrimSpace(c.PostForm("featuredImage")),
		FocusKeyword:  strings.TrimSpace(c.PostForm("focusKeyword")),
	}
}

func (f articleForm) data() map[string]any {
	return map[string]any{
		"title":          f.Title,
		"body":           f.Body,
		"excerpt":        f.Excerpt,
		"featuredImage":  f.FeaturedImage,
		"seoTitle":       f.Title,
		"seoDescription": f.Excerpt,
		"focusKeyword":   f.FocusKeyword,
	}
}

func articleListPageData(list []*contentdto.ContentResp, published map[string]string, pageErr articleMsg) *ArticleListPageResp {
	rows := make([]ArticleListRowResp, 0, len(list))
	for _, it := range list {
		rows = append(rows, articleListRow(it, published[it.ID]))
	}
	return &ArticleListPageResp{
		Title:          articlePageTitle,
		Menu:           "articles",
		Rows:           rows,
		Total:          len(rows),
		Empty:          len(rows) == 0,
		Err:            pageErr,
		BlogBase:       articleBlogPathPrefix,
		Keyword:        "",
		ClearFilterURL: "/admin/articles",
		Limit:          articleListPageSize,

		FilterQuery: "",
	}
}

func articleListRow(it *contentdto.ContentResp, urlPath string) ArticleListRowResp {
	published := strings.TrimSpace(urlPath) != ""
	stateKey, stateFallback := articleStateLabel(published)
	return ArticleListRowResp{
		ID:        it.ID,
		Title:     articleTextOrEmpty(articleStr(it.Data, "title")),
		Slug:      articleTextOrEmpty(it.Slug),
		Excerpt:   articleTextOrEmpty(articleStr(it.Data, "excerpt")),
		Revision:  it.Revision,
		UpdatedAt: it.UpdatedAt,
		Published: published,
		URLPath:   urlPath,
		PublicURL: articlePublicURL(urlPath),
		EditURL:   articleEditURL(it.ID),

		StateKey:      stateKey,
		StateFallback: stateFallback,
	}
}

func articleStateLabel(published bool) (key, fallback string) {
	if published {
		return contentenums.StatePublished, "已发布"
	}
	return contentenums.StateUnpublished, "未发布"
}

func articleEditPageData(ctx context.Context, h *articlePageHandle, item *contentdto.ContentResp,
	id string, pageErr articleMsg, lang string, trs ...func(key, fallback string) string) *ArticleEditPageResp {
	tr := articlePublishTr(trs)
	data := gin.H{}
	if item != nil {
		data = item.Data
	}
	form := ArticleEditFormResp{
		ID:            id,
		Slug:          articleSlugOf(item),
		Title:         articleStr(data, "title"),
		Body:          articleStr(data, "body"),
		Excerpt:       articleStr(data, "excerpt"),
		FeaturedImage: articleStr(data, "featuredImage"),
		FocusKeyword:  articleStr(data, "focusKeyword"),
		Revision:      articleRevisionOf(item),
		UpdatedAt:     articleUpdatedAtOf(item),
	}
	out := &ArticleEditPageResp{
		Title:           articleEditTitle,
		Menu:            "articles",
		IsNew:           item == nil && id == "",
		Form:            form,
		Err:             pageErr,
		ListURL:         "/admin/articles",
		TemplateEditURL: "",

		Score: articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item)), lang, tr),
	}

	projectOptions := articleProjectOptions(ctx, h)
	out.ArticlePublishBlockResp = articlePublishView(ctx, h, id, articleSlugOf(item), projectOptions)
	out.ArticleImportBlockResp = articleImportBlockView(h, item, id, projectOptions)
	if h != nil && h.templates != nil && id != "" {
		if resolved, err := h.templates.ResolveTemplate(ctx, articleEntityType); err == nil {
			projectID := ""
			if len(projectOptions) > 0 {
				if pid := projectOptions[0].ID; pid != "" {
					projectID = pid
				}
			}
			out.TemplateEditURL = articleTemplateEditURL(resolved.TemplateID, articleEntityType, id, projectID)
		}
	}
	return out
}

func articleTemplateEditURL(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return "/workbench?" + q.Encode()
}

func articleImportBlockView(h *articlePageHandle, item *contentdto.ContentResp, id string,
	projectOptions []OptionResp) ArticleImportBlockResp {
	if id == "" {
		return articleImportUnavailable(articleMsg{Key: contentenums.ImportHintSaveFirst, Fallback: "先保存这篇文章，再回来把它导入画布。"})
	}
	if h == nil || h.pages == nil {
		return articleImportUnavailable(articleImportTextMsg(articleImportDepsText))
	}
	if len(projectOptions) == 0 {
		return articleImportUnavailable(articleMsg{Key: contentenums.ImportHintNoProject, Fallback: "还没有站点工程：先在「页面」里建一个工程，导入需要知道页面挂到哪个站。"})
	}

	slug := articleSlugOf(item)
	defaultPath := articleImportPathPrefix + "new"
	if slug != "" {
		defaultPath = articleImportPathPrefix + slug
	}
	return ArticleImportBlockResp{
		ImportAvailable:     true,
		ImportProjects:      projectOptions,
		ImportDefaultPath:   defaultPath,
		ImportHasPublished:  articleStr(itemData(item), "body") != "",
		ImportPreviewTarget: "#article-import-result",
	}
}

func articleImportUnavailable(hint articleMsg) ArticleImportBlockResp {

	return ArticleImportBlockResp{ImportHint: hint}
}

func itemData(item *contentdto.ContentResp) map[string]any {
	if item == nil || item.Data == nil {
		return map[string]any{}
	}
	return item.Data
}

func firstNonEmptyMsg(values ...articleMsg) articleMsg {
	for _, v := range values {
		if !v.Empty() {
			return v
		}
	}
	return articleMsg{}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

func hasMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

type scoreView = scoreview.View

const staleImpactPageLimit = 8

func staleImpactView(available bool, pages []StalePageRowResp, total int, truncated bool, hint articleMsg) *StaleImpactResp {
	if pages == nil {
		pages = []StalePageRowResp{}
	}
	return &StaleImpactResp{
		Available: available,
		Pages:     pages,
		Total:     total,
		Truncated: truncated,
		Limit:     staleImpactPageLimit,
		Hint:      hint,
	}
}

func articleStaleImpact(ctx context.Context, h *articlePageHandle) *StaleImpactResp {
	if h == nil || h.pages == nil {

		return staleImpactView(false, nil, 0, false,
			articleMsg{Key: blockenums.ImpactUnavailableNoPageCapability,
				Fallback: "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"})
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {

		logger.Scene("content").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return staleImpactView(false, nil, 0, false,
			articleMsg{Key: blockenums.ImpactUnavailableProjectReadFailed,
				Fallback: "读取站点工程失败，本次无法统计待重建影响面。"})
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常）")
		return staleImpactView(false, nil, 0, false, articleMsg{})
	}
	pages := make([]StalePageRowResp, 0, len(res.Pages))
	for i := range res.Pages {

		pages = append(pages, StalePageRowResp{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectID:   res.Pages[i].ProjectID,
			ProjectName: res.Pages[i].ProjectName,
		})
	}
	return staleImpactView(true, pages, res.Total, res.Truncated, articleMsg{})
}

func (h *articlePageHandle) ArticleStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("content").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]StalePageRowResp, 0, len(res.Pages))

	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, StalePageRowResp{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectName: res.Pages[i].ProjectName,

			Published: res.Pages[i].Published,

			RebuildFailure: rebuildFailureViewOf(res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	pageview.RenderTemplate(c, ArticleStaleDrawerTemplate, &StalePagesDrawerResp{
		Rows: rows, Total: res.Total, Truncated: res.Truncated,
		Limit: staleImpactPageLimit, MultiProject: len(projects) > 1,
	})
}

type rebuildFailureView struct {
	StageKey      string
	StageFallback string

	At string

	Has bool
}

func rebuildFailureViewOf(stage string, at *utils.JSONTime) rebuildFailureView {
	if strings.TrimSpace(stage) == "" && at == nil {
		return rebuildFailureView{}
	}
	view := rebuildFailureView{Has: true, StageFallback: stage}
	view.StageKey, view.StageFallback = pageenums.RebuildStageLabel(stage)
	if at != nil {
		view.At = time.Time(*at).Local().Format("2006-01-02 15:04")
	}
	return view
}

type articleTranslationRow struct {
	Context string
	Field   string

	FieldKey      string
	FieldFallback string
	Source        string
	SourceHash    string
	Target        string
	Engine        string
	Translated    bool

	Rich bool
}

type articleTranslationGroup struct {
	Key        string
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []articleTranslationRow
}

type articleTranslationHandle struct {
	contents contentcontract.ContentService
	writer   *i18n.ContentWriter
}

func NewArticleTranslationHandle(contents contentcontract.ContentService) *articleTranslationHandle {
	return &articleTranslationHandle{contents: contents}
}

func (h *articleTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

var articleFieldLabel = map[string]articleTranslationText{
	"title":   {contentenums.TranslationsFieldTitle, "标题"},
	"body":    {contentenums.TranslationsFieldBody, "正文"},
	"excerpt": {contentenums.TranslationsFieldExcerpt, "摘要"},
}

type articleTranslationText struct{ Key, Fallback string }

func articleTranslationTextField(field string) (key, fallback string) {
	if item, ok := articleFieldLabel[field]; ok {
		return item.Key, item.Fallback
	}
	return "", field
}

func (h *articleTranslationHandle) ArticleTranslations(c *gin.Context) {
	data := h.build(c, strings.TrimSpace(c.Query("lang")), strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("keyword")))
	pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
}

func (h *articleTranslationHandle) SaveArticleTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	lang := strings.TrimSpace(c.PostForm("lang"))
	projectID := strings.TrimSpace(c.PostForm("project"))
	keyword := strings.TrimSpace(c.PostForm("keyword"))
	page, _ := strconv.Atoi(c.PostForm("page"))
	data := h.build(c, lang, projectID, keyword)

	if page != data.Page || c.PostForm("limit") != strconv.Itoa(data.Limit) {
		data.Errors = []articleMsg{articleTranslationMsg(contentenums.TranslationsErrPageChanged, "列表页码已变化，请刷新后重试")}
		pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = []articleMsg{articleTranslationMsg(contentenums.TranslationsErrRowCountMismatch, "提交的行数不一致，请刷新后重试")}
		pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
		return
	}
	if h.writer == nil {
		data.Errors = []articleMsg{articleTranslationMsg(contentenums.TranslationsErrStorageUnavailable, "译文存储不可用")}
		pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
		return
	}

	var rowErrors []articleMsg
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, articleTranslationFilled(c,
				contentenums.TranslationsErrSourceChanged, "{context}：原文已变化，请刷新后重试",
				map[string]string{"context": contextName}))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateArticleTarget(contextName, source, target); verr != "" {

			rowErrors = append(rowErrors, articleTranslationFilled(c, verr, articleTranslationFallbacks[verr],
				map[string]string{"context": contextName}))
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.Source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		keys = append(keys, key)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
		return
	}
	if len(items) == 0 {
		articleTranslationDone(c, projectID, lang, keyword, data.Page, data.Limit, 0)
		return
	}

	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("content").With("lang", lang).Error(berr, "读取现有文章译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changed := make([]string, 0, len(items))
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changed = append(changed, keys[i])
	}
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("content").
				With("lang", lang).
				With("user_id", builtin.CurrentUserID(c)).
				Error(uerr, "写入文章译文失败")

			data.Errors = []articleMsg{articleTranslationFilled(c,
				contentenums.TranslationsErrSaveFailed, "保存失败：{detail}",
				map[string]string{"detail": response.MsgText(c, articleErrMsgQuiet(c, uerr))})}
			pageview.RenderTemplate(c, ArticleTranslationsTemplate, data)
			return
		}
	}
	articleTranslationDone(c, projectID, lang, keyword, data.Page, data.Limit, written)
}

func articleTranslationMsg(key, fallback string) articleMsg {
	return articleMsg{Key: key, Fallback: fallback}
}

func articleTranslationFilled(c *gin.Context, key, fallback string, params map[string]string) articleMsg {
	return articleMsg{Fallback: i18n.FillTranslate(text.RequestTranslateFunc(c), key, fallback, params)}
}

func articleErrFilled(c *gin.Context, key, fallback string, err error) articleMsg {
	return articleTranslationFilled(c, key, fallback,
		map[string]string{"detail": response.MsgText(c, articleErrMsg(c, err))})
}

func articleTranslationDone(c *gin.Context, projectID, lang, keyword string, page, limit, written int) {
	var msg articleMsg
	if written > 0 {
		msg = articleTranslationFilled(c,
			contentenums.TranslationsSaved, "已保存 {count} 条译文（下次构建生效）。",
			map[string]string{"count": strconv.Itoa(written)})
	} else {
		msg = articleTranslationMsg(contentenums.TranslationsSavedNone, "没有需要写入的变化。")
	}
	articlePageJump(c, true, msg,
		articleTranslationLocation(projectID, lang, keyword, page, limit),
		articleMsg{Key: contentenums.TranslationsHeading, Fallback: "文章翻译"})
}

type articleTranslationsData struct {
	Title      string
	Menu       string
	Lang       string
	Keyword    string
	Page       int
	Limit      int
	ProjectID  string
	HasData    bool
	Pagination pagination.Page
	Langs      []translationLangOption
	Groups     []articleTranslationGroup
	RowCount   int
	Done       int
	Total      int
	Saved      bool
	SavedNote  string
	Errors     []articleMsg
}

func (d *articleTranslationsData) sourceOf(contextName, hash string) (articleTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return r, true
			}
		}
	}
	return articleTranslationRow{}, false
}

var articleTranslationFallbacks = map[string]string{
	contentenums.TranslationsErrContextInvalid:  "语境非法：不是文章的可翻译字段",
	contentenums.TranslationsErrNotTranslatable: "原文不参与翻译（空串、纯数字或纯符号）",
	contentenums.TranslationsErrRichMismatch:    "正文译文的形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}

func validateArticleTarget(contextName string, source articleTranslationRow, target string) string {
	entityType, field, ok := i18n.ParseContentContext(contextName)
	if !ok || !contentcontract.IsTranslatableField(entityType, field) {
		return contentenums.TranslationsErrContextInvalid
	}
	if !i18n.ShouldTranslateContent(source.Source) {
		return contentenums.TranslationsErrNotTranslatable
	}
	if source.Rich && hasMarkup(source.Source) != hasMarkup(target) {
		return contentenums.TranslationsErrRichMismatch
	}
	return ""
}

func articleTranslationLocation(projectID, lang, keyword string, page, limit int) string {
	q := url.Values{"lang": {lang}}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit != 20 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return "/admin/articles/translations?" + q.Encode()
}

func (h *articleTranslationHandle) build(c *gin.Context, lang, projectID, keyword string) *articleTranslationsData {
	ctx := c.Request.Context()
	page, limit := reqparams.PageParams(c)
	if c.Request.Method == http.MethodPost {
		page, _ = strconv.Atoi(c.PostForm("page"))
		limit, _ = strconv.Atoi(c.PostForm("limit"))
		if limit < 1 || limit > 100 {
			limit = 20
		}
	}
	if page < 1 {
		page = 1
	}
	data := &articleTranslationsData{
		Title: contentenums.TranslationsHeading, Menu: "article-translations", Lang: lang,
		Keyword: keyword, Page: page, Limit: limit, ProjectID: projectID,
		Groups: []articleTranslationGroup{},
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if lang == "" {
		return data
	}
	if h.contents == nil {
		data.Errors = []articleMsg{articleTranslationMsg(contentenums.TranslationsErrDepsMissing, "内容模块未装配")}
		return data
	}
	filter := &contentdto.ListReq{EntityType: "article", Keyword: keyword}
	total, err := h.contents.Count(ctx, filter)
	if err != nil {
		data.Errors = []articleMsg{articleErrFilled(c,
			contentenums.TranslationsErrCountFailed, "读取文章总数失败：{detail}", err)}
		return data
	}
	data.HasData = total > 0
	if keyword != "" && total == 0 {

		unfiltered, countErr := h.contents.Count(ctx, &contentdto.ListReq{EntityType: "article"})
		if countErr != nil {
			data.Errors = []articleMsg{articleErrFilled(c,
				contentenums.TranslationsErrCountFailed, "读取文章总数失败：{detail}", countErr)}
			return data
		}
		data.HasData = unfiltered > 0
	}
	page = articleClampPage(page, limit, total)
	data.Page = page
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {

		data.Errors = []articleMsg{articleErrFilled(c,
			contentenums.TranslationsErrListFailed, "读取文章列表失败：{detail}", err)}
		return data
	}
	pg := pagination.Page{Total: total, Page: page, Limit: limit}

	pg.Base = reqparams.FilterBaseURL("/admin/articles/translations", map[string]string{
		"lang": lang, "project": projectID, "keyword": keyword, "limit": strconv.Itoa(limit),
	})
	data.Pagination = pg
	fields := contentcontract.TranslatableFields("article")

	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
		context  string
	}
	var refs []rowRef
	groups := make([]articleTranslationGroup, 0, len(list))
	for _, item := range list {
		group := articleTranslationGroup{
			Key: item.ID, EntityType: "article", EntityID: item.ID,
			Subtitle: item.Slug, Rows: []articleTranslationRow{},
		}
		if t, ok := item.Data["title"].(string); ok {
			group.Title = t
		}
		for _, f := range fields {
			src, ok := item.Data[f].(string)
			if !ok || !i18n.ShouldTranslateContent(src) {
				continue
			}
			contextName := i18n.ContentContext("article", f)
			h := i18n.ContentHash(src)
			refs = append(refs, rowRef{
				groupIdx: len(groups), rowIdx: len(group.Rows), hash: h, context: contextName,
			})
			fieldKey, fieldFallback := articleTranslationTextField(f)
			group.Rows = append(group.Rows, articleTranslationRow{
				Context: contextName, Field: f, FieldKey: fieldKey, FieldFallback: fieldFallback,
				Source: src, SourceHash: h, Rich: f == "body",
			})
		}
		data.RowCount += len(group.Rows)
		data.Total++
		groups = append(groups, group)
	}
	data.Groups = groups

	if h.writer != nil && len(refs) > 0 {
		hashes := make([]string, 0, len(refs))
		for _, r := range refs {
			hashes = append(hashes, r.hash)
		}
		targets, lerr := h.writer.LoadTargets(ctx, lang, hashes)
		if lerr != nil {
			logger.Scene("content").With("lang", lang).Error(lerr, "读取现有文章译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, r.context)]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}
	return data
}

const articleErrScene = "content"

func articleStr(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

func articleTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return articleEmptyField
	}
	return value
}

func articleSlugOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.Slug
}

func articleRevisionOf(item *contentdto.ContentResp) int64 {
	if item == nil {
		return 0
	}
	return item.Revision
}

func articleUpdatedAtOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.UpdatedAt
}

func articleEditURL(id string) string {
	return "/admin/articles/edit?" + url.Values{"id": {id}}.Encode()
}

func articlePreviewURL(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return articleBlogPathPrefix + slug
}

func articleDefaultURLPath(slug string) string {
	return articlePreviewURL(slug)
}

func articlePublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return "/site/" + strings.TrimPrefix(p, "/")
}

func articlesPublished(ctx context.Context, port articlePublishPort, list []*contentdto.ContentResp) map[string]string {
	out := map[string]string{}
	if port == nil {
		return out
	}
	for _, it := range list {
		res, err := port.GetByEntity(ctx, &presentationdto.GetByEntityReq{
			EntityType: articleEntityType, EntityID: it.ID,
		})
		if err != nil || res == nil {
			continue
		}
		if strings.TrimSpace(res.URLPath) != "" {
			out[it.ID] = res.URLPath
		}
	}
	return out
}

func scoreViewFromResult(res *scoring.Result, _ ...func(key, fallback string) string) scoreView {
	return scoreview.Of(res)
}

func articleScoreViewOf(data map[string]any, articleURL, lang string,
	_ ...func(key, fallback string) string) scoreView {
	sv := scoreViewFromResult(seoscore.ScoreArticle(data, articleURL, lang))
	if !sv.OK {
		return sv
	}

	sv.SerpTitle = articleStr(data, "title")
	sv.HasSerpTitle = strings.TrimSpace(sv.SerpTitle) != ""
	sv.SerpDesc = articleStr(data, "excerpt")
	sv.HasSerpDesc = strings.TrimSpace(sv.SerpDesc) != ""
	sv.SerpURL = articleURL
	if strings.TrimSpace(sv.SerpURL) == "" {
		sv.SerpURL = articleBlogPathPrefix + "example"
	}
	sv.HasSerpURL = true
	return sv
}

func articleInternalMsg(c *gin.Context, err error) articleMsg {
	if err != nil {
		logger.Scene(articleErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "content 后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return adminui.InternalMsg()
}

func articleErrMsg(c *gin.Context, err error) articleMsg {
	return articleErrMsgOf(c, articleFallbacks, err)
}

func articleErrMsgQuiet(c *gin.Context, err error) articleMsg {
	return articleErrMsgOfFact(articleFallbacks, adminui.PageErrorFactQuiet(c, articleErrScene, err))
}

func articleFacingErrMsg(c *gin.Context, err error) articleMsg {
	if msg, ok := articleBulkIDsMsg(err); ok {
		return msg
	}
	return articleErrMsg(c, err)
}

func articleErrMsgOf(c *gin.Context, scoped map[string]string, err error) articleMsg {
	return articleErrMsgOfFact(scoped, adminui.PageErrorFact(c, articleErrScene, err))
}

func articleErrMsgOfFact(scoped map[string]string, fact adminui.ErrFact) articleMsg {
	if fact.Key == "" {
		return articleMsg{}
	}
	return articleMsg{Key: fact.Key, Fallback: articleErrFallback(scoped, fact.Key, fact.Fallback)}
}

func articleErrFallback(scoped map[string]string, key, factFallback string) string {

	for _, fb := range []string{scoped[key], articleFallbacks[key], factFallback} {
		if fb = strings.TrimSpace(fb); fb != "" {
			return fb
		}
	}
	return key
}

func articleBulkIDsMsg(err error) (articleMsg, bool) {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return articleMsg{}, false
	}
	return articleMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count)), true
}

func articleBulkIDsJumpMsg(c *gin.Context, err error) articleMsg {
	if msg, ok := articleBulkIDsMsg(err); ok {
		return msg
	}

	return articleInternalMsg(c, err)
}

const (
	articlePublishedText      = contentenums.PublishOkPublished
	articleRebuiltText        = contentenums.PublishOkRebuilt
	articleURLUpdatedText     = contentenums.PublishOkURLUpdated
	articleNoProjectText      = contentenums.PublishErrNoProject
	articleNoURLPathText      = contentenums.PublishErrNoURLPath
	articleNoTemplatePickText = contentenums.PublishErrNoTemplatePick

	articleNoTemplateHint = contentenums.PublishHintNoTemplate

	articlePublishUnavailableText = contentenums.PublishErrUnavailable

	articlePublishDepsMissingText = contentenums.PublishErrDepsMissing

	articleNewPathRequiredText = contentenums.PublishErrNewPathRequired

	articleSaveFirstHint = contentenums.PublishHintSaveFirst
)

func articlePublishTextMsg(key string) articleMsg {
	return articleMsg{Key: key, Fallback: articlePublishFallbacks[key]}
}

func articlePublishHintMsg(key string) articleMsg {
	return articlePublishTextMsg(key)
}

func articlePublishTr(trs []func(key, fallback string) string) func(key, fallback string) string {
	if len(trs) > 0 && trs[0] != nil {
		return trs[0]
	}
	return func(_, fallback string) string { return fallback }
}

var articlePublishFallbacks = map[string]string{
	presentationenums.ErrInvalidParam:         "发布参数不完整，请检查工程、路径与模板。",
	presentationenums.ErrNotFound:             "这篇文章还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套文章详情模板。",
	presentationenums.ErrEntityMissing:        "文章不存在，可能已被删除。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与文章正文后重试。",
	presentationenums.ErrProjectRequired:      "站点里有多个工程，请显式选择这篇所属的工程。",
	presentationenums.ErrProjectNotFound:      "选择的站点工程不存在，请刷新后重试。",
	presentationenums.ErrRegistryMissing:      "实体类型注册表未装配（装配缺陷），请联系管理员。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是文章类型的，换一套再试。",
	presentationenums.ErrInvalidPath:          "访问路径不合法：必须以 / 开头，且不含空格、引号与 .. 路径段。",
	presentationenums.ErrSamePath:             "新路径与当前路径相同，没有需要修改的地方。",
	presentationenums.ErrPathOccupied:         "这个路径已被其他页面或详情页占用，换一个再试。",
	articlePublishedText:                      "已发布。文章详情页已上线，访问面立即可见。",
	articleRebuiltText:                        "已重新发布。原路径的产物已更新。",
	articleURLUpdatedText:                     "已改 URL。新路径已上线，旧路径按你的选择处理（301 跳转或直接失效）。",
	articleNoProjectText:                      "请先选择这篇文章属于哪个站点工程。",
	articleNoURLPathText:                      "请填写文章详情页的访问路径。",
	articleNoTemplatePickText:                 "请选择一套文章详情模板。",
	articleNoTemplateHint:                     "这个站还没有「文章详情模板」（entityType=article 的内容模板），所以现在没有东西可以渲染这篇文章。模板的建立与内容编辑链路目前没有后台入口，需要先建一套 article 类型的内容模板，再回来发布。",
	articlePublishUnavailableText:             "发布能力未装配，请联系管理员。",
	articlePublishDepsMissingText:             "发布能力未装配（装配缺陷），本页只显示文章内容。",
	articleNewPathRequiredText:                "请填写新的访问路径。",
	articleSaveFirstHint:                      "先保存这篇文章，再回来看发布状态。",
}

func (h *articlePageHandle) ArticlePublish(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	urlPath := strings.TrimSpace(c.PostForm("urlPath"))
	templateID := strings.TrimSpace(c.PostForm("templateId"))

	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextMsg(articlePublishUnavailableText))
		return
	}
	if projectID == "" {
		articleEditJump(c, false, id, articlePublishTextMsg(articleNoProjectText))
		return
	}
	if urlPath == "" {
		articleEditJump(c, false, id, articlePublishTextMsg(articleNoURLPathText))
		return
	}
	if !strings.HasPrefix(urlPath, "/") {

		urlPath = "/" + urlPath
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), &presentationdto.CreateInstanceReq{
		EntityType: articleEntityType, EntityID: id,
		URLPath: urlPath, ProjectID: projectID, TemplateID: templateID,
	}); err != nil {
		articleEditJump(c, false, id, articlePublishErrMsg(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextMsg(articlePublishedText))
}

func (h *articlePageHandle) ArticleRebuild(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextMsg(articlePublishUnavailableText))
		return
	}
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationdto.RebuildReq{EntityID: id}); err != nil {
		articleEditJump(c, false, id, articlePublishErrMsg(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextMsg(articleRebuiltText))
}

func (h *articlePageHandle) ArticleUpdateURL(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextMsg(articlePublishUnavailableText))
		return
	}
	newPath := strings.TrimSpace(c.PostForm("newPath"))
	if newPath == "" {
		articleEditJump(c, false, id, articlePublishTextMsg(articleNewPathRequiredText))
		return
	}
	if _, err := h.instances.UpdateURL(c.Request.Context(), &presentationdto.UpdateURLReq{
		EntityType:   articleEntityType,
		EntityID:     id,
		NewPath:      newPath,
		WithRedirect: c.PostForm("withRedirect") != "",
	}); err != nil {
		articleEditJump(c, false, id, articlePublishErrMsg(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextMsg(articleURLUpdatedText))
}

func articlePublishView(ctx context.Context, h *articlePageHandle, id, slug string, projectOptions []OptionResp) ArticlePublishBlockResp {
	if strings.TrimSpace(id) == "" {
		return articlePublishUnavailable(articlePublishHintMsg(articleSaveFirstHint))
	}
	if h.instances == nil {
		return articlePublishUnavailable(articlePublishHintMsg(articlePublishDepsMissingText))
	}

	out := ArticlePublishBlockResp{PublishConfigured: true}
	inst, err := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: articleEntityType, EntityID: id,
	})
	if err == nil && inst != nil && strings.TrimSpace(inst.URLPath) != "" {
		out.Published = true
		out.URLPath = inst.URLPath
		out.PublicURL = articlePublicURL(inst.URLPath)
		out.Stale = inst.Stale
		out.Status = inst.Status
		return out
	}

	out.DefaultURLPath = articleDetailDefaultPath(ctx, h, projectOptions, slug, id)
	out.Projects = projectOptions
	templates := articleTemplateOptions(ctx, h)
	out.Templates = templates
	out.HasTemplates = len(templates) > 0
	out.NoTemplateHint = articlePublishHintMsg(articleNoTemplateHint)
	return out
}

func articlePublishUnavailable(hint articleMsg) ArticlePublishBlockResp {

	return ArticlePublishBlockResp{
		PublishHint: hint,

		NoTemplateHint: articlePublishHintMsg(""),
	}
}

func articleDetailDefaultPath(ctx context.Context, h *articlePageHandle,
	projectOptions []OptionResp, slug, id string) string {
	if len(projectOptions) > 0 {
		if pid := projectOptions[0].ID; pid != "" {
			if p := siteurl.DetailPathForProject(ctx, h.projects, pid, siteurl.KindArticle, slug, id); p != "" {
				return p
			}
		}
	}
	if slug == "" {
		return articleImportPathPrefix + "new"
	}
	return articleImportPathPrefix + slug
}

func articleProjectOptions(ctx context.Context, h *articlePageHandle) []OptionResp {
	out := []OptionResp{}
	if h.projects == nil {
		return out
	}
	list, err := h.projects.List(ctx)
	if err != nil {
		return out
	}
	for _, p := range list {
		out = append(out, OptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}

func articleTemplateOptions(ctx context.Context, h *articlePageHandle) []TemplateOptionResp {
	out := []TemplateOptionResp{}
	if h.templates == nil {
		return out
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: articleEntityType})
	if err != nil {
		return out
	}
	for _, t := range list {
		out = append(out, TemplateOptionResp{ID: t.ID, Name: t.Name, Version: t.DraftVersion})
	}
	return out
}

func articlePublishErrMsg(c *gin.Context, err error) articleMsg {
	return articleErrMsgOf(c, articlePublishFallbacks, err)
}

const (
	articlePublishPath  = "/admin/articles/publish"
	articleRebuildPath  = "/admin/articles/rebuild"
	articleListPath     = "/admin/articles"
	articleEditPagePath = "/admin/articles/edit"
)
